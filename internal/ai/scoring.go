package ai

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// This file asks a typed-decision model (TypeSafe's Jev, via OpenRouter) for the
// numeric half of a debrief: the hire verdict and the five rubric scores, each
// with a confidence. Jev cannot write prose, so the summary and the
// strengths/improvements stay with the interview model. See
// docs/jev-debrief-plan.md; cmd/debrief-eval compares these scores with the
// cached LLM debriefs before anything user-facing depends on them.

// DefaultScoreModel is the Jev alias on OpenRouter; it tracks the newest
// release, and every answer reports the exact version that produced it.
const DefaultScoreModel = "~typesafe/jev-latest"

// stateContext prefixes every question, because question keys never reach the
// model and each question has to explain the state on its own.
const stateContext = `The input is the transcript of a mock technical coding interview — lines start with "Interviewer:" or "Candidate:" — followed, when it was captured, by the candidate's final code as transcribed from their screen.`

// verdictQuestionKey labels the hire-verdict question in requests and answers.
const verdictQuestionKey = "verdict"

// verdictOption is one answer to the verdict question: the key Jev picks, the
// canonical hire-scale label it maps to ("" for "not enough evidence"), and
// the situation it describes.
type verdictOption struct {
	key, label, description string
}

// verdictOptions mirror the debrief's 5-point hire scale, plus an explicit
// "not enough evidence" option: Jev always picks something, so without one it
// would force a verdict on a two-line session.
var verdictOptions = []verdictOption{
	{"strong_hire", "Strong Hire", "Strong Hire: reached a correct, optimal solution largely on their own, explained it clearly, and analysed its complexity correctly."},
	{"hire", "Hire", "Hire: reached a correct, efficient solution with at most small hints, with sound reasoning and only minor gaps."},
	{"lean_hire", "Lean Hire", "Lean Hire: reached a working solution but needed meaningful hints, or left gaps in complexity analysis, edge cases, or code quality."},
	{"no_hire", "No Hire", "No Hire: did not reach a working solution, or only got there after being given the key insight."},
	{"strong_no_hire", "Strong No Hire", "Strong No Hire: made little progress, showed fundamental misunderstandings, or could not engage with the problem."},
	{"unclear", "", "Not enough evidence: the interview was too short, or no coding problem was really worked on, to make a recommendation."},
}

// verdictInstructions is the verdict question as sent to the model.
const verdictInstructions = stateContext + ` As a senior engineer on the hiring panel, what is your overall hiring recommendation for this candidate, based on this interview alone? Weigh their problem solving, their code, their communication, their complexity analysis, and any verdict on correctness the interviewer stated.`

// rubricDimension is one debrief rubric dimension as a pair of questions: a
// five-level score whose levels are concrete situations (Jev places a state on
// situations far better than on degrees like "moderately good"), and a yes/no
// evidence check, so "too little to judge" maps to the rubric's 0 rather than
// dragging the scale down.
type rubricDimension struct {
	key      string   // models.DebriefRubric JSON name, e.g. "problemSolving"
	question string   // what is judged
	levels   []string // situations for scores 1..5, lowest first
	evidence string   // yes/no: there is enough evidence to judge this
}

// rubricDimensions mirror DebriefPrompt's five dimensions, in display order.
var rubricDimensions = []rubricDimension{
	{
		key:      "problemSolving",
		question: "How well did the candidate solve the coding problem — their approach, the correctness of their solution, and their handling of edge cases?",
		levels: []string{
			"Could not form a viable approach, even with hints.",
			"Found an approach only after substantial hints; the solution stayed incomplete or incorrect.",
			"Reached a working solution with some hints, but missed edge cases or a better approach.",
			"Reached a correct, efficient solution with little help and handled most edge cases.",
			"Independently reached an optimal, correct solution and handled edge cases unprompted.",
		},
		evidence: "The candidate actually worked on a coding problem in this interview, so their problem solving can be judged.",
	},
	{
		key:      "coding",
		question: "How good is the candidate's final code — its correctness, structure, and readability? Judge the transcribed final code if present, otherwise what the transcript reveals about the code they wrote.",
		levels: []string{
			"No meaningful code was written, or it is fundamentally broken.",
			"Code was written but is largely incorrect or very hard to follow.",
			"Code mostly works but has bugs, awkward structure, or unclear naming.",
			"Code is correct and readable, with only minor issues.",
			"Code is correct, clean, and well structured — production quality for an interview.",
		},
		evidence: "There is enough of the candidate's code, in the final code or the transcript, to judge its quality.",
	},
	{
		key:      "communication",
		question: "How clearly did the candidate communicate — thinking out loud, explaining their approach, and responding to the interviewer's questions and hints?",
		levels: []string{
			"Barely explained anything, or ignored the interviewer's questions and hints.",
			"Explanations were hard to follow, and hints were often missed or misread.",
			"Explained the main idea, but with gaps, and needed prompting to think out loud.",
			"Explained clearly and used hints well, with minor lapses.",
			"Narrated their thinking clearly throughout and engaged crisply with every question and hint.",
		},
		evidence: "The candidate said enough in this interview for their communication to be judged.",
	},
	{
		key:      "complexity",
		question: "How correct was the candidate's reasoning about the time and space complexity of their solution?",
		levels: []string{
			"Never addressed complexity, or stated it completely wrong.",
			"Stated complexity mostly incorrectly, or only after being led to it.",
			"Got part of it right — for example time but not space — or reasoned shakily.",
			"Stated correct time and space complexity with reasonable justification.",
			"Stated and justified correct time and space complexity precisely, including trade-offs.",
		},
		evidence: "Time or space complexity was discussed in this interview.",
	},
	{
		key:      "pace",
		question: "How well did the candidate manage their time — moving from a first approach toward a working, optimal solution without stalling?",
		levels: []string{
			"Stalled for most of the interview and never got close to a solution.",
			"Progressed slowly, with long stalls or circling on the same idea.",
			"Made steady progress but spent too long on some steps.",
			"Moved efficiently from approach to solution with only brief stalls.",
			"Moved briskly and decisively from approach to an optimal solution.",
		},
		evidence: "The interview ran long enough to judge how the candidate used their time.",
	},
}

// evidenceThreshold is the yes-probability below which a dimension is reported
// as "not enough evidence" (0), matching how the LLM debrief scores 0.
const evidenceThreshold = 0.5

// scoreStateBudget caps the state Jev reads, in bytes (~4 per token). Jev takes
// 32K tokens for the state plus its longest question; this leaves headroom for
// the question and for code-dense transcripts, which tokenize worse.
const scoreStateBudget = 100_000

// RubricKeys lists the rubric dimensions in display order, by their
// models.DebriefRubric JSON names.
func RubricKeys() []string {
	keys := make([]string, len(rubricDimensions))
	for i, d := range rubricDimensions {
		keys[i] = d.key
	}
	return keys
}

// scoreKey and evidenceKey label a dimension's two questions.
func scoreKey(dim string) string    { return "score_" + dim }
func evidenceKey(dim string) string { return "evidence_" + dim }

// ScoreQuestions returns the eleven questions a scoring call asks: the verdict,
// then a score and an evidence check per rubric dimension.
func ScoreQuestions() map[string]DecisionQuestion {
	qs := make(map[string]DecisionQuestion, 1+2*len(rubricDimensions))
	options := make(map[string]string, len(verdictOptions))
	for _, o := range verdictOptions {
		options[o.key] = o.description
	}
	qs[verdictQuestionKey] = DecisionQuestion{Type: "choice", Instructions: verdictInstructions, Options: options}
	for _, d := range rubricDimensions {
		qs[scoreKey(d.key)] = DecisionQuestion{Type: "score", Instructions: stateContext + " " + d.question, Levels: d.levels}
		qs[evidenceKey(d.key)] = DecisionQuestion{Type: "noul", Instructions: stateContext + " " + d.evidence}
	}
	return qs
}

// DimensionScore is Jev's assessment of one rubric dimension.
type DimensionScore struct {
	Key        string  // models.DebriefRubric JSON name
	Score      float64 // 1..5, probability-weighted; 0 when there wasn't enough evidence
	Evidence   float64 // yes-probability that there was enough evidence to judge it
	Confidence float64 // how concentrated the score distribution was, 0..1
}

// SessionScores is Jev's typed assessment of a finished session — the numeric
// half of a debrief.
type SessionScores struct {
	Verdict           string           // hire-scale label; "" when Jev judged there was not enough evidence
	VerdictConfidence float64          // how concentrated the verdict distribution was, 0..1
	Dimensions        []DimensionScore // in RubricKeys order
	Model             string           // the exact model version that answered
	InputTokens       int
	Trimmed           bool // the state exceeded scoreStateBudget and its middle was dropped
}

// ScoreSession asks a decision model for the verdict and rubric scores over the
// same transcript + final-code text GenerateDebrief reads; one call answers all
// eleven questions. Callers should treat any error as "no Jev scores" and fall
// back to the LLM debrief.
func (c *Client) ScoreSession(ctx context.Context, model, transcript, finalCode string) (SessionScores, error) {
	state, trimmed := ScoreState(transcript, finalCode)
	res, err := c.Decide(ctx, model, state, ScoreQuestions())
	if err != nil {
		return SessionScores{}, err
	}
	scores, err := ParseScores(res)
	if err != nil {
		return SessionScores{}, err
	}
	scores.Trimmed = trimmed
	return scores, nil
}

// ParseScores maps a scoring call's answers onto SessionScores: the verdict
// option to its hire-scale label, each dimension's weighted level to the 1-5
// rubric scale (or 0 when the evidence check says there was too little to go
// on). A missing or unrecognised answer is an error that quotes the response,
// since the decisions API's field names are not yet stable.
func ParseScores(res DecisionResult) (SessionScores, error) {
	fail := func(format string, args ...any) (SessionScores, error) {
		return SessionScores{}, fmt.Errorf("ai: score: "+format+" (response: %s)", append(args, clip(string(res.Raw), maxErrorBody))...)
	}

	v, ok := res.Answers[verdictQuestionKey]
	if !ok {
		return fail("no verdict answer")
	}
	label, known := verdictLabel(v.Choice)
	if !known {
		return fail("unknown verdict option %q", v.Choice)
	}
	s := SessionScores{
		Verdict:           label,
		VerdictConfidence: v.Confidence,
		Model:             res.Model,
		InputTokens:       res.Usage.InputTokens,
	}

	for _, d := range rubricDimensions {
		sc, ok := res.Answers[scoreKey(d.key)]
		if !ok {
			return fail("no %s score", d.key)
		}
		ev, ok := res.Answers[evidenceKey(d.key)]
		if !ok {
			return fail("no %s evidence answer", d.key)
		}
		score := 0.0
		if ev.Noul >= evidenceThreshold {
			score = levelToRubric(sc.Score, len(d.levels))
		}
		s.Dimensions = append(s.Dimensions, DimensionScore{
			Key:        d.key,
			Score:      score,
			Evidence:   ev.Noul,
			Confidence: sc.Confidence,
		})
	}
	return s, nil
}

// verdictLabel maps a verdict option key to its hire-scale label; known is false
// for a key that isn't one of verdictOptions.
func verdictLabel(key string) (label string, known bool) {
	for _, o := range verdictOptions {
		if o.key == key {
			return o.label, true
		}
	}
	return "", false
}

// levelToRubric converts Jev's probability-weighted level (0..levels-1) to the
// debrief's 1-5 scale, clamped.
func levelToRubric(level float64, levels int) float64 {
	if levels < 2 {
		return 0
	}
	r := 1 + level*4/float64(levels-1)
	return min(max(r, 1), 5)
}

// ScoreState builds the text Jev scores — the same transcript + final-code text
// GenerateDebrief reads — dropping the middle of an oversized one so that both
// the opening (problem setup) and the ending (final solution, the interviewer's
// verdict, the code) survive. trimmed reports whether anything was dropped.
func ScoreState(transcript, finalCode string) (state string, trimmed bool) {
	return trimMiddle(debriefContent(transcript, finalCode), scoreStateBudget)
}

// trimMiddle shortens text to about budget bytes by keeping whole lines from the
// start (a quarter of the budget) and from the end (the rest), replacing what is
// between with a marker. A text with no line short enough to keep falls back
// to its last budget bytes.
func trimMiddle(text string, budget int) (string, bool) {
	if len(text) <= budget {
		return text, false
	}
	lines := strings.SplitAfter(text, "\n")
	const markerRoom = 64
	headBudget := budget / 4
	tailBudget := budget - headBudget - markerRoom

	head, used := 0, 0
	for head < len(lines) && used+len(lines[head]) <= headBudget {
		used += len(lines[head])
		head++
	}
	tail, used := len(lines), 0
	for tail > head && used+len(lines[tail-1]) <= tailBudget {
		used += len(lines[tail-1])
		tail--
	}
	if head == 0 && tail == len(lines) {
		cut := len(text) - budget
		for cut < len(text) && !utf8.RuneStart(text[cut]) {
			cut++
		}
		return text[cut:], true
	}
	marker := fmt.Sprintf("[… %d lines omitted …]\n", tail-head)
	return strings.Join(lines[:head], "") + marker + strings.Join(lines[tail:], ""), true
}
