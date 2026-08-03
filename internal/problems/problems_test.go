package problems

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"mogi/internal/models"
)

// TestEmbedData verifies the committed CSV parses into the shape the app
// expects. Counts are floors rather than exact values — mirroring the
// generator's own sanity gates — so the biweekly dataset refresh doesn't break
// the build; the per-row invariants pin what the app actually relies on.
func TestEmbedData(t *testing.T) {
	companies := Companies()
	if len(companies) < 400 {
		t.Errorf("company count = %d, want >= 400", len(companies))
	}

	total := 0
	for _, c := range companies {
		total += c.ProblemCount
	}
	if total < 10000 {
		t.Errorf("total problems = %d, want >= 10000", total)
	}

	// google's pool must contain Two Sum, fully enriched: the id and acceptance
	// come from the LeetCode API join, the URL is derived from the slug.
	google, err := Problems("google")
	if err != nil {
		t.Fatalf("Problems(google): %v", err)
	}
	var twoSum *models.Problem
	positiveFreq := false
	for i := range google {
		if google[i].URL == "https://leetcode.com/problems/two-sum" {
			twoSum = &google[i]
		}
		if google[i].Frequency > 0 {
			positiveFreq = true
		}
	}
	if twoSum == nil {
		t.Fatal("google pool has no two-sum")
	}
	if twoSum.ID != 1 {
		t.Errorf("Two Sum ID = %d, want 1", twoSum.ID)
	}
	if twoSum.Title != "Two Sum" {
		t.Errorf("Two Sum title = %q", twoSum.Title)
	}
	if twoSum.Difficulty != "Easy" {
		t.Errorf("Two Sum difficulty = %q, want Easy", twoSum.Difficulty)
	}
	if twoSum.Acceptance <= 0 || twoSum.Acceptance >= 100 {
		t.Errorf("Two Sum acceptance = %v, want in (0,100)", twoSum.Acceptance)
	}
	if !positiveFreq {
		t.Error("google pool has no problem with positive frequency")
	}

	// Dataset-wide invariants: canonical difficulty, API-enriched id, sane
	// percentage ranges, and a derived LeetCode URL on every row.
	for _, c := range companies {
		pl, err := Problems(c.Slug)
		if err != nil {
			t.Fatalf("Problems(%q): %v", c.Slug, err)
		}
		for _, p := range pl {
			switch p.Difficulty {
			case "Easy", "Medium", "Hard":
			default:
				t.Fatalf("%s / %q: non-canonical difficulty %q", c.Slug, p.Title, p.Difficulty)
			}
			if p.ID <= 0 {
				t.Fatalf("%s / %q: missing problem id", c.Slug, p.Title)
			}
			if p.Frequency < 0 || p.Frequency > 100 {
				t.Fatalf("%s / %q: frequency %v out of range", c.Slug, p.Title, p.Frequency)
			}
			if p.Acceptance < 0 || p.Acceptance > 100 {
				t.Fatalf("%s / %q: acceptance %v out of range", c.Slug, p.Title, p.Acceptance)
			}
			if !strings.HasPrefix(p.URL, "https://leetcode.com/problems/") {
				t.Fatalf("%s / %q: unexpected URL %q", c.Slug, p.Title, p.URL)
			}
		}
	}

	if _, err := Problems("does-not-exist"); err == nil {
		t.Error("Problems(unknown company) should error")
	}
}

// TestDisplayName covers both naming paths: display names carried through from
// the upstream folder (including brand casing the old hand-maintained override
// map used to patch) and the title-case fallback for slugs absent from the
// dataset (e.g. a stale lastCompany preference).
func TestDisplayName(t *testing.T) {
	fromDataset := map[string]string{
		"google":    "Google",
		"bytedance": "ByteDance",
	}
	for slug, want := range fromDataset {
		if got := DisplayName(slug); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", slug, got, want)
		}
	}

	fallback := map[string]string{
		"some-unknown-co":  "Some Unknown Co",
		"vanished-startup": "Vanished Startup",
	}
	for slug, want := range fallback {
		if got := DisplayName(slug); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", slug, got, want)
		}
	}

	// Every company in the dataset carries a non-empty display name.
	for _, c := range Companies() {
		if strings.TrimSpace(c.Name) == "" {
			t.Fatalf("company %q has an empty display name", c.Slug)
		}
	}
}

// TestMockPairInvariants runs many seeded draws over google's real pool: the pair
// is always ordered easier-first, the two are distinct, and — because google has
// a large recent subset — both are always drawn from that recent subset.
func TestMockPairInvariants(t *testing.T) {
	google, err := Problems("google")
	if err != nil {
		t.Fatalf("Problems(google): %v", err)
	}

	recent := 0
	for _, p := range google {
		if p.Recent {
			recent++
		}
	}
	if recent < recentPoolThreshold {
		t.Fatalf("google recent pool = %d, expected >= %d for this test", recent, recentPoolThreshold)
	}

	for seed := int64(0); seed < 500; seed++ {
		pair, err := mockPair(google, rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		checkPairInvariant(t, pair)
		if !pair[0].Recent || !pair[1].Recent {
			t.Fatalf("seed %d: drew a non-recent problem from a recent-rich pool: %+v", seed, pair)
		}
	}
}

// TestMockPairDegeneratePools exercises the fallback paths on hand-built pools
// that the long tail of companies actually has: single-tier, no-Easy, a lone
// mid-tier among a higher tier, and all-Easy. None may panic, and every draw
// must still be ordered easier-first with distinct problems.
func TestMockPairDegeneratePools(t *testing.T) {
	cases := map[string][]models.Problem{
		"all medium": {
			mkProb(1, "Medium", 90, 40, false), mkProb(2, "Medium", 70, 55, false),
			mkProb(3, "Medium", 50, 30, false), mkProb(4, "Medium", 30, 60, false),
			mkProb(5, "Medium", 10, 45, false), mkProb(6, "Medium", 5, 50, false),
		},
		"no easy (medium+hard)": {
			mkProb(1, "Hard", 90, 30, false), mkProb(2, "Medium", 80, 55, false),
			mkProb(3, "Hard", 60, 25, false), mkProb(4, "Medium", 40, 50, false),
			mkProb(5, "Hard", 20, 35, false), mkProb(6, "Medium", 10, 65, false),
		},
		"lone medium among hard": {
			mkProb(1, "Medium", 95, 50, false), mkProb(2, "Hard", 90, 30, false),
			mkProb(3, "Hard", 70, 28, false), mkProb(4, "Hard", 50, 33, false),
			mkProb(5, "Hard", 30, 22, false), mkProb(6, "Hard", 10, 40, false),
		},
		"all easy": {
			mkProb(1, "Easy", 90, 70, false), mkProb(2, "Easy", 70, 60, false),
			mkProb(3, "Easy", 50, 80, false), mkProb(4, "Easy", 30, 55, false),
			mkProb(5, "Easy", 10, 65, false),
		},
		"zero frequency (uniform fallback)": {
			mkProb(1, "Easy", 0, 70, false), mkProb(2, "Medium", 0, 50, false),
			mkProb(3, "Hard", 0, 30, false), mkProb(4, "Medium", 0, 45, false),
			mkProb(5, "Easy", 0, 60, false),
		},
	}
	for name, pool := range cases {
		t.Run(name, func(t *testing.T) {
			for seed := int64(0); seed < 300; seed++ {
				pair, err := mockPair(pool, rand.New(rand.NewSource(seed)))
				if err != nil {
					t.Fatalf("seed %d: %v", seed, err)
				}
				checkPairInvariant(t, pair)
			}
		})
	}
}

// TestMockPairSmallPoolErrors confirms pools below the minimum error instead of
// returning a meaningless pair — both via the internal draw and the real
// snapshot's smallest company.
func TestMockPairSmallPoolErrors(t *testing.T) {
	tooSmall := []models.Problem{
		mkProb(1, "Easy", 90, 60, false), mkProb(2, "Medium", 70, 50, false),
		mkProb(3, "Hard", 50, 30, false), mkProb(4, "Medium", 30, 45, false),
	}
	if _, err := mockPair(tooSmall, rand.New(rand.NewSource(1))); err == nil {
		t.Errorf("mockPair with %d problems should error", len(tooSmall))
	}

	// Find any ineligible company in the snapshot and confirm the public MockPair
	// rejects it (robust to data refreshes changing which company is smallest).
	var small string
	for _, c := range Companies() {
		if !c.MockEligible {
			small = c.Slug
			break
		}
	}
	if small == "" {
		t.Skip("no ineligible company in this snapshot")
	}
	if _, err := MockPair(small); err == nil {
		t.Errorf("MockPair(%q) should error (pool < %d)", small, mockMinPool)
	}
}

// TestMockPairPublic smoke-tests the seeded public entry point on an eligible
// company.
func TestMockPairPublic(t *testing.T) {
	pair, err := MockPair("google")
	if err != nil {
		t.Fatalf("MockPair(google): %v", err)
	}
	checkPairInvariant(t, pair)
}

// TestMockPairFromInvariants runs seeded draws over hand-built curated sets —
// including shapes MockPair would reject (tiny pools) — asserting the pair is
// always distinct and ordered easier-first.
func TestMockPairFromInvariants(t *testing.T) {
	cases := map[string][]models.Problem{
		"mixed tiers": {
			mkProb(1, "Easy", 90, 70, false), mkProb(2, "Medium", 70, 50, false),
			mkProb(3, "Hard", 50, 30, false), mkProb(4, "Medium", 30, 45, false),
			mkProb(5, "Easy", 10, 60, false),
		},
		"exactly two, easy+medium": {
			mkProb(1, "Easy", 40, 70, false), mkProb(2, "Medium", 80, 50, false),
		},
		"exactly two, same tier": {
			mkProb(1, "Medium", 40, 70, false), mkProb(2, "Medium", 80, 50, false),
		},
		"all same tier": {
			mkProb(1, "Medium", 90, 40, false), mkProb(2, "Medium", 70, 55, false),
			mkProb(3, "Medium", 50, 30, false), mkProb(4, "Medium", 30, 60, false),
			mkProb(5, "Medium", 10, 45, false), mkProb(6, "Medium", 5, 50, false),
		},
		"zero frequency (uniform fallback)": {
			mkProb(1, "Easy", 0, 70, false), mkProb(2, "Medium", 0, 50, false),
			mkProb(3, "Hard", 0, 30, false),
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			for seed := int64(0); seed < 300; seed++ {
				pair, err := mockPairFrom(set, rand.New(rand.NewSource(seed)))
				if err != nil {
					t.Fatalf("seed %d: %v", seed, err)
				}
				checkPairInvariant(t, pair)
			}
		})
	}
}

// TestMockPairFromDuplicateInput proves the set draw never returns the same
// problem twice even when the stored set contains repeats, and that the
// minimum-size check counts distinct questions, not raw entries.
func TestMockPairFromDuplicateInput(t *testing.T) {
	a := mkProb(1, "Easy", 50, 70, false)
	b := mkProb(2, "Medium", 50, 50, false)

	if _, err := mockPairFrom([]models.Problem{a, a}, rand.New(rand.NewSource(1))); err == nil {
		t.Error("two copies of one question should error (only one distinct)")
	}

	// [A, A, B] has exactly two distinct questions, so every draw must be {A, B};
	// checkPairInvariant fatals if a duplicate pair ever slips through.
	for seed := int64(0); seed < 300; seed++ {
		pair, err := mockPairFrom([]models.Problem{a, a, b}, rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		checkPairInvariant(t, pair)
	}
}

// TestMockPairFromTooSmall pins the set floor: zero or one question errors, two
// distinct questions succeed (unlike the company draw's mockMinPool of 5).
func TestMockPairFromTooSmall(t *testing.T) {
	if _, err := mockPairFrom(nil, rand.New(rand.NewSource(1))); err == nil {
		t.Error("empty set should error")
	}
	one := []models.Problem{mkProb(1, "Easy", 50, 60, false)}
	if _, err := mockPairFrom(one, rand.New(rand.NewSource(1))); err == nil {
		t.Error("one-question set should error")
	}
	two := []models.Problem{
		mkProb(1, "Easy", 50, 60, false), mkProb(2, "Hard", 50, 30, false),
	}
	pair, err := mockPairFrom(two, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("two-question set: %v", err)
	}
	checkPairInvariant(t, pair)
}

// TestMockPairFromIgnoresRecentFilter builds a set the company draw would
// narrow to its recent subset (>= recentPoolThreshold recent problems) plus one
// non-recent Hard. The set draw must still consider the non-recent entry: Q2 is
// always that Hard, which a recent-narrowed draw could never pick.
func TestMockPairFromIgnoresRecentFilter(t *testing.T) {
	var set []models.Problem
	for i := 1; i <= recentPoolThreshold; i++ {
		set = append(set, mkProb(i, "Easy", 10, 60, true))
	}
	hard := mkProb(999, "Hard", 10, 20, false)
	set = append(set, hard)

	for seed := int64(0); seed < 300; seed++ {
		pair, err := mockPairFrom(set, rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		checkPairInvariant(t, pair)
		if pair[1].ID != hard.ID {
			t.Fatalf("seed %d: Q2 = %+v, want the lone Hard (recent filter must not apply)", seed, pair[1])
		}
	}
}

// TestMockPairFromPublic smoke-tests the seeded public entry point.
func TestMockPairFromPublic(t *testing.T) {
	set := []models.Problem{
		mkProb(1, "Easy", 50, 60, false), mkProb(2, "Medium", 50, 45, false),
		mkProb(3, "Hard", 50, 30, false),
	}
	pair, err := MockPairFrom(set)
	if err != nil {
		t.Fatalf("MockPairFrom: %v", err)
	}
	checkPairInvariant(t, pair)
}

// checkPairInvariant asserts the two problems are distinct and ordered
// easier-first: strictly-lower tier, or same tier with acceptance not increasing.
func checkPairInvariant(t *testing.T, pair [2]models.Problem) {
	t.Helper()
	if pair[0].ID == pair[1].ID {
		t.Fatalf("Q1 == Q2: %+v", pair)
	}
	t0, t1 := tier(pair[0].Difficulty), tier(pair[1].Difficulty)
	if t0 > t1 {
		t.Fatalf("Q1 (%s) is a harder tier than Q2 (%s)", pair[0].Difficulty, pair[1].Difficulty)
	}
	if t0 == t1 && pair[0].Acceptance < pair[1].Acceptance {
		t.Fatalf("same tier but Q1 acceptance %.1f < Q2 %.1f", pair[0].Acceptance, pair[1].Acceptance)
	}
}

func mkProb(id int, difficulty string, freq, acc float64, recent bool) models.Problem {
	return models.Problem{
		ID:         id,
		Title:      fmt.Sprintf("Problem %d", id),
		Difficulty: difficulty,
		Frequency:  freq,
		Acceptance: acc,
		URL:        fmt.Sprintf("https://leetcode.com/problems/p%d", id),
		Recent:     recent,
	}
}
