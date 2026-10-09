// Command debrief-eval compares Jev's typed scores with the LLM debriefs already
// cached in the local data.db, so whether Jev should score Mogi's debriefs is
// decided on real sessions before anything user-facing depends on it (see
// docs/jev-debrief-plan.md).
//
// It is a manual, repo-owner utility (run `go run ./cmd/debrief-eval`); the app
// binary never invokes it. It opens data.db read-only, sends each finished
// session's transcript + final code — the same text its debrief was generated
// from — to Jev through OpenRouter's decisions endpoint, and prints a row per
// session plus aggregate agreement. Network access lives here and in
// internal/ai, mirroring cmd/downloads.
//
// The OpenRouter key comes from OPENROUTER_API_KEY, else the BYOK key stored in
// data.db; the managed test-account key is never used. Jev's endpoint is an
// alpha route: -endpoint points elsewhere if it moves, and -dry-run prints the
// exact request without sending anything or needing a key.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"mogi/internal/ai"
	"mogi/internal/models"
	"mogi/internal/store"
)

// callTimeout bounds each API call; the HTTP client's own timeout is shorter,
// so this is only a backstop.
const callTimeout = 90 * time.Second

// config holds the parsed command-line flags.
type config struct {
	dbPath   string
	limit    int
	model    string
	endpoint string
	runs     int
	llmRerun bool
	dryRun   bool
}

func main() {
	var cfg config
	flag.StringVar(&cfg.dbPath, "db", "", "path to data.db (default: the app's own database)")
	flag.IntVar(&cfg.limit, "n", 20, "compare at most this many sessions, newest first")
	flag.StringVar(&cfg.model, "model", ai.DefaultScoreModel, "decision model to score with")
	flag.StringVar(&cfg.endpoint, "endpoint", "", "decisions endpoint URL (default: OpenRouter's alpha route)")
	flag.IntVar(&cfg.runs, "runs", 2, "score each session this many times, to measure Jev's run-to-run spread")
	flag.BoolVar(&cfg.llmRerun, "llm-rerun", false, "also regenerate each LLM debrief once, unsaved, to measure the LLM's run-to-run spread (spends the session model's tokens)")
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "print the request for the newest session and exit, calling no API")
	flag.Parse()

	if err := run(cfg, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "debrief-eval:", err)
		os.Exit(1)
	}
}

// run loads the comparable sessions, scores them, and writes the report to out
// (progress goes to progress, so out stays a clean report).
func run(cfg config, out, progress io.Writer) error {
	if cfg.runs < 1 {
		cfg.runs = 1
	}
	path := cfg.dbPath
	if path == "" {
		p, err := store.DefaultPath()
		if err != nil {
			return err
		}
		path = p
	}
	db, err := store.OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer db.Close()

	cands, noDebrief, err := loadCandidates(db, cfg.limit)
	if err != nil {
		return err
	}
	if noDebrief > 0 {
		fmt.Fprintf(out, "Note: %d finished session(s) have no debrief yet and are skipped — open their Debrief tab in the app to include them.\n", noDebrief)
	}
	if len(cands) == 0 {
		fmt.Fprintf(out, "No finished sessions with a cached debrief in %s.\n", path)
		return nil
	}
	if cfg.dryRun {
		return printDryRun(out, cfg.model, cands[0])
	}

	key, source, err := resolveKey(db)
	if err != nil {
		return err
	}
	client := ai.NewClient(key)
	if cfg.endpoint != "" {
		client.SetDecisionsURL(cfg.endpoint)
	}

	fmt.Fprintf(out, "Jev vs cached LLM debriefs: %d session(s) from %s\n", len(cands), path)
	fmt.Fprintf(out, "Model %s, %d run(s) each; key from %s.\n", cfg.model, cfg.runs, source)
	if cfg.llmRerun {
		fmt.Fprintln(out, "-llm-rerun: regenerating each LLM debrief once (unsaved) on the session's own model.")
	}
	fmt.Fprintln(out)

	results := make([]result, 0, len(cands))
	for i, c := range cands {
		fmt.Fprintf(progress, "[%d/%d] %s\n", i+1, len(cands), sessionTitle(c.summary))
		results = append(results, evaluate(client, cfg, c))
	}
	printReport(out, results)
	return nil
}

// candidate is a finished session with a cached LLM debrief to compare against.
type candidate struct {
	summary    models.SessionSummary
	transcript string
	finalCode  string
}

// sessionReader is the slice of the store the tool reads sessions through.
type sessionReader interface {
	ListSessions() ([]models.SessionSummary, error)
	GetMessages(sessionID string) ([]models.Message, error)
	GetSessionFinalCode(id string) (string, error)
}

// loadCandidates returns up to limit of the newest finished sessions that have a
// cached debrief and enough transcript to assess (the debrief's own two-message
// minimum), plus how many otherwise-eligible sessions were skipped only because
// their debrief was never generated.
func loadCandidates(db sessionReader, limit int) (cands []candidate, noDebrief int, err error) {
	sessions, err := db.ListSessions()
	if err != nil {
		return nil, 0, err
	}
	for _, s := range sessions {
		if s.EndedAt == nil || s.MessageCount < 2 {
			continue
		}
		if s.Debrief == nil {
			noDebrief++
			continue
		}
		if len(cands) >= limit {
			continue
		}
		msgs, err := db.GetMessages(s.ID)
		if err != nil {
			return nil, 0, err
		}
		code, err := db.GetSessionFinalCode(s.ID)
		if err != nil {
			return nil, 0, err
		}
		cands = append(cands, candidate{summary: s, transcript: ai.BuildTranscript(msgs), finalCode: code})
	}
	return cands, noDebrief, nil
}

// keySource is the slice of the store resolveKey reads.
type keySource interface {
	GetAPIKey(provider string) (string, error)
}

// resolveKey picks the OpenRouter key: OPENROUTER_API_KEY wins, else the BYOK
// key the app stored in data.db. It reports where the key came from — never the
// key itself — so the report says which account is billed. The managed
// test-account key is deliberately not used: it is developer-funded and capped.
func resolveKey(db keySource) (key, source string, err error) {
	if k := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); k != "" {
		return k, "OPENROUTER_API_KEY", nil
	}
	if k, err := db.GetAPIKey("openrouter"); err == nil && strings.TrimSpace(k) != "" {
		return strings.TrimSpace(k), "the BYOK key stored in data.db", nil
	}
	return "", "", errors.New("no OpenRouter key: set OPENROUTER_API_KEY or save one in the app's Settings")
}

// printDryRun shows exactly what a scoring call would send for one session — the
// request body, indented — without touching the network or needing a key.
func printDryRun(out io.Writer, model string, c candidate) error {
	state, trimmed := ai.ScoreState(c.transcript, c.finalCode)
	body, err := ai.DecisionRequestBody(model, state, ai.ScoreQuestions())
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		return err
	}
	note := ""
	if trimmed {
		note = ", trimmed to fit Jev's context"
	}
	fmt.Fprintf(out, "Dry run: request for %q (%s), state %d bytes%s:\n\n", sessionTitle(c.summary), c.summary.ID, len(state), note)
	pretty.WriteTo(out)
	fmt.Fprintln(out)
	return nil
}

// result is one session's comparison: the cached LLM debrief, Jev's scores per
// run, and (with -llm-rerun) a fresh, unsaved LLM debrief.
type result struct {
	cand     candidate
	jev      []ai.SessionScores
	jevErr   error
	rerun    *models.Debrief
	rerunErr error
}

// evaluate scores one session cfg.runs times with Jev (stopping at the first
// error) and, with -llm-rerun, regenerates its LLM debrief once.
func evaluate(client *ai.Client, cfg config, c candidate) result {
	r := result{cand: c}
	for i := 0; i < cfg.runs; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		s, err := client.ScoreSession(ctx, cfg.model, c.transcript, c.finalCode)
		cancel()
		if err != nil {
			r.jevErr = err
			break
		}
		r.jev = append(r.jev, s)
	}
	if cfg.llmRerun {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		d, err := client.GenerateDebrief(ctx, c.summary.Model, c.transcript, c.finalCode)
		cancel()
		if err != nil {
			r.rerunErr = err
		} else {
			r.rerun = &d
		}
	}
	return r
}

// verdictScale orders the hire-scale labels from strongest to weakest.
var verdictScale = []string{"Strong Hire", "Hire", "Lean Hire", "No Hire", "Strong No Hire"}

// verdictStep returns how many steps apart two verdicts sit on the hire scale,
// or -1 when either is missing ("" — the judge saw too little to decide).
func verdictStep(a, b string) int {
	ia, ib := -1, -1
	for i, v := range verdictScale {
		if v == a {
			ia = i
		}
		if v == b {
			ib = i
		}
	}
	if ia < 0 || ib < 0 {
		return -1
	}
	if ia > ib {
		return ia - ib
	}
	return ib - ia
}

// rubricValue reads one dimension of an LLM rubric by its JSON name.
func rubricValue(r models.DebriefRubric, key string) int {
	switch key {
	case "problemSolving":
		return r.ProblemSolving
	case "coding":
		return r.Coding
	case "communication":
		return r.Communication
	case "complexity":
		return r.Complexity
	case "pace":
		return r.Pace
	}
	return 0
}

// llmHeadline and jevHeadline compute the single headline number the app shows:
// the mean of the five dimensions, zeros included (mirrors scoreOf in
// frontend/src/lib/verdict.ts).
func llmHeadline(r models.DebriefRubric) float64 {
	sum := 0
	for _, k := range ai.RubricKeys() {
		sum += rubricValue(r, k)
	}
	return float64(sum) / float64(len(ai.RubricKeys()))
}

func jevHeadline(s ai.SessionScores) float64 {
	sum := 0.0
	for _, d := range s.Dimensions {
		sum += d.Score
	}
	return sum / float64(len(ai.RubricKeys()))
}

// dimLabels mirror the short radar labels in Debrief.tsx.
var dimLabels = map[string]string{
	"problemSolving": "Logic",
	"coding":         "Quality",
	"communication":  "Comm.",
	"complexity":     "Complexity",
	"pace":           "Pace",
}

// summary aggregates the comparison across sessions. Dimension differences
// only count pairs where both sides found enough evidence (score > 0).
type summary struct {
	sessions, scored int

	verdictPairs, verdictExact, verdictWithinOne int

	dimDiff  map[string]float64 // sum of |LLM − Jev| per dimension
	dimPairs map[string]int
	bigGaps  int // dimension pairs with |LLM − Jev| ≥ 2

	jevSpread, llmSpread           float64 // sums of per-dimension |run1 − run2|
	jevSpreadPairs, llmSpreadPairs int
	jevVerdictRuns, jevFlips       int // sessions with two runs; verdict changed between them
	llmVerdictRuns, llmFlips       int

	tokens int
	models map[string]bool
}

// summarize compares each session's first Jev run with its cached LLM debrief —
// a one-shot score is what the feature would cache — and measures both judges'
// run-to-run spread where a second run exists.
func summarize(results []result) summary {
	s := summary{dimDiff: map[string]float64{}, dimPairs: map[string]int{}, models: map[string]bool{}}
	s.sessions = len(results)
	for _, r := range results {
		llm := *r.cand.summary.Debrief
		if r.rerun != nil {
			s.llmVerdictRuns++
			if r.rerun.Verdict != llm.Verdict {
				s.llmFlips++
			}
			for _, k := range ai.RubricKeys() {
				a, b := rubricValue(llm.Rubric, k), rubricValue(r.rerun.Rubric, k)
				if a > 0 && b > 0 {
					s.llmSpread += math.Abs(float64(a - b))
					s.llmSpreadPairs++
				}
			}
		}
		if len(r.jev) == 0 {
			continue
		}
		s.scored++
		first := r.jev[0]
		for _, run := range r.jev {
			s.tokens += run.InputTokens
			if run.Model != "" {
				s.models[run.Model] = true
			}
		}

		if step := verdictStep(llm.Verdict, first.Verdict); step >= 0 {
			s.verdictPairs++
			if step == 0 {
				s.verdictExact++
			}
			if step <= 1 {
				s.verdictWithinOne++
			}
		}
		for _, d := range first.Dimensions {
			l := rubricValue(llm.Rubric, d.Key)
			if l == 0 || d.Score == 0 {
				continue
			}
			diff := math.Abs(float64(l) - d.Score)
			s.dimDiff[d.Key] += diff
			s.dimPairs[d.Key]++
			if diff >= 2 {
				s.bigGaps++
			}
		}

		if len(r.jev) > 1 {
			second := r.jev[1]
			s.jevVerdictRuns++
			if second.Verdict != first.Verdict {
				s.jevFlips++
			}
			for i, d := range first.Dimensions {
				if i < len(second.Dimensions) && d.Score > 0 && second.Dimensions[i].Score > 0 {
					s.jevSpread += math.Abs(d.Score - second.Dimensions[i].Score)
					s.jevSpreadPairs++
				}
			}
		}
	}
	return s
}

// printReport writes the per-session table, any errors, and the summary.
func printReport(out io.Writer, results []result) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	header := []string{"DATE", "SESSION", "LLM", "JEV (CONF)", "STEP"}
	for _, k := range ai.RubricKeys() {
		header = append(header, dimLabels[k])
	}
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	var failures []string
	for _, r := range results {
		sum := r.cand.summary
		llm := *sum.Debrief
		row := []string{
			sum.StartedAt.Local().Format("2006-01-02"),
			clipTitle(sessionTitle(sum), 32),
			fmt.Sprintf("%s %.1f", verdictText(llm.Verdict), llmHeadline(llm.Rubric)),
		}
		if len(r.jev) == 0 {
			row = append(row, "error", "—")
			for range ai.RubricKeys() {
				row = append(row, "")
			}
			failures = append(failures, fmt.Sprintf("%s: %v", sessionTitle(sum), r.jevErr))
		} else {
			first := r.jev[0]
			step := "—"
			if n := verdictStep(llm.Verdict, first.Verdict); n >= 0 {
				step = fmt.Sprint(n)
			}
			row = append(row,
				fmt.Sprintf("%s (%.2f) %.1f", verdictText(first.Verdict), first.VerdictConfidence, jevHeadline(first)),
				step)
			for _, d := range first.Dimensions {
				row = append(row, fmt.Sprintf("%s→%s", intText(rubricValue(llm.Rubric, d.Key)), floatText(d.Score)))
			}
		}
		if r.rerunErr != nil {
			failures = append(failures, fmt.Sprintf("%s (LLM rerun): %v", sessionTitle(sum), r.rerunErr))
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	tw.Flush()
	fmt.Fprintln(out, "\nLLM and JEV show the verdict and the headline score (the mean of the five dimensions, as")
	fmt.Fprintln(out, "in the app); STEP is how far apart the verdicts sit on the hire scale. Dimension cells read")
	fmt.Fprintln(out, "LLM→Jev; — means that judge saw too little evidence.")

	if len(failures) > 0 {
		fmt.Fprintln(out, "\nErrors:")
		for _, f := range failures {
			fmt.Fprintln(out, "  "+f)
		}
	}

	s := summarize(results)
	fmt.Fprintf(out, "\nSummary (%d of %d sessions scored)\n", s.scored, s.sessions)
	if s.verdictPairs > 0 {
		fmt.Fprintf(out, "  Verdict agreement: exact %d/%d (%s), within one step %d/%d (%s)\n",
			s.verdictExact, s.verdictPairs, pct(s.verdictExact, s.verdictPairs),
			s.verdictWithinOne, s.verdictPairs, pct(s.verdictWithinOne, s.verdictPairs))
	} else {
		fmt.Fprintln(out, "  Verdict agreement: no session where both judges reached a verdict")
	}
	var dims []string
	for _, k := range ai.RubricKeys() {
		if n := s.dimPairs[k]; n > 0 {
			dims = append(dims, fmt.Sprintf("%s %.2f", dimLabels[k], s.dimDiff[k]/float64(n)))
		}
	}
	if len(dims) > 0 {
		fmt.Fprintf(out, "  Rubric mean |LLM−Jev|: %s\n", strings.Join(dims, " · "))
	}
	fmt.Fprintf(out, "  Big disagreements (|Δ| ≥ 2): %d\n", s.bigGaps)
	if s.jevVerdictRuns > 0 {
		fmt.Fprintf(out, "  Jev run-to-run: mean |Δ| %s per dimension; verdict changed in %d of %d sessions\n",
			mean(s.jevSpread, s.jevSpreadPairs), s.jevFlips, s.jevVerdictRuns)
	} else {
		fmt.Fprintln(out, "  Jev run-to-run: pass -runs 2 or more to measure")
	}
	if s.llmVerdictRuns > 0 {
		fmt.Fprintf(out, "  LLM run-to-run: mean |Δ| %s per dimension; verdict changed in %d of %d sessions\n",
			mean(s.llmSpread, s.llmSpreadPairs), s.llmFlips, s.llmVerdictRuns)
	} else {
		fmt.Fprintln(out, "  LLM run-to-run: pass -llm-rerun to measure (spends the session model's tokens)")
	}
	if len(s.models) > 0 {
		names := make([]string, 0, len(s.models))
		for m := range s.models {
			names = append(names, m)
		}
		sort.Strings(names)
		fmt.Fprintf(out, "  Answered by: %s; %d input tokens sent to Jev\n", strings.Join(names, ", "), s.tokens)
	}

	fmt.Fprintln(out, "\nDecision rule (docs/jev-debrief-plan.md): switch the debrief's scores to Jev only if")
	fmt.Fprintln(out, "within-one-step verdict agreement is at least ~85%, Jev's run-to-run spread is well below")
	fmt.Fprintln(out, "the LLM's, and reading the biggest disagreements favours Jev or is a wash.")
}

// sessionTitle is the History list's label for a session, with its difficulty.
func sessionTitle(s models.SessionSummary) string {
	t := s.ProblemTitle
	if t == "" {
		t = "Interview session"
	}
	if s.Difficulty != "" {
		t += " (" + s.Difficulty + ")"
	}
	return t
}

// clipTitle shortens a title for the table, on a rune boundary.
func clipTitle(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// verdictText shows a missing verdict as "—".
func verdictText(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// intText and floatText show a 0 ("too little evidence") score as "—".
func intText(v int) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprint(v)
}

func floatText(v float64) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f", v)
}

// pct formats n/d as a whole percentage.
func pct(n, d int) string {
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

// mean formats sum/n to two decimals, or "—" with nothing to average.
func mean(sum float64, n int) string {
	if n == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", sum/float64(n))
}
