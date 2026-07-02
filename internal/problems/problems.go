// Package problems serves the Company Practice question pools: which companies
// exist, each company's problem list, and the "mock interview" two-problem draw.
//
// The data is static reference metadata (titles, difficulties, frequencies,
// links — never problem statements), generated from an upstream LeetCode
// company-frequency dataset and committed as data/problems.csv. That file is
// embedded at build time and parsed once into memory on first use, so the app
// works offline and CI never needs the network. Refresh it with the gen tool
// (see the //go:generate directive below and internal/problems/gen).
package problems

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-interviewer/internal/models"
)

//go:generate go run ./gen

//go:embed data/problems.csv
var problemsCSV string

const (
	// mockMinPool is the smallest pool where a random two-problem draw is
	// meaningful; below it, browse-and-pick serves the company instead.
	mockMinPool = 5
	// recentPoolThreshold is how many recent-window problems a company needs
	// before the mock draw prefers that subset over the full pool. Below it, the
	// recent set is too thin and would starve variety.
	recentPoolThreshold = 20
)

var (
	parseOnce    sync.Once
	byCompany    map[string][]models.Problem // slug -> problems, frequency desc (CSV order)
	companySlugs []string                    // sorted company slugs for stable Companies() order
	parseErr     error
)

// companyNameOverrides maps slugs whose display name isn't just a title-cased
// version of the slug — acronyms and camelCase brands. Everything else falls
// through to the default transform (hyphens -> spaces, each word capitalised).
var companyNameOverrides = map[string]string{
	"amd":        "AMD",
	"att":        "AT&T",
	"bytedance":  "ByteDance",
	"c3-ai":      "C3 AI",
	"deepmind":   "DeepMind",
	"doordash":   "DoorDash",
	"dp-world":   "DP World",
	"ebay":       "eBay",
	"ey":         "EY",
	"github":     "GitHub",
	"hp":         "HP",
	"hpe":        "HPE",
	"hsbc":       "HSBC",
	"ibm":        "IBM",
	"jpmorgan":   "JPMorgan",
	"kpmg":       "KPMG",
	"linkedin":   "LinkedIn",
	"nvidia":     "NVIDIA",
	"openai":     "OpenAI",
	"oyo":        "OYO",
	"paypal":     "PayPal",
	"phonepe":    "PhonePe",
	"pwc":        "PwC",
	"sap":        "SAP",
	"servicenow": "ServiceNow",
	"tcs":        "TCS",
	"tiktok":     "TikTok",
	"uipath":     "UiPath",
	"vmware":     "VMware",
}

// load parses the embedded CSV once, grouping problems by company slug. It never
// panics: a parse failure is recorded in parseErr and surfaced by the accessors.
func load() {
	parseOnce.Do(func() {
		r := csv.NewReader(strings.NewReader(problemsCSV))
		r.FieldsPerRecord = -1
		records, err := r.ReadAll()
		if err != nil {
			parseErr = fmt.Errorf("problems: parse embedded csv: %w", err)
			return
		}

		byCompany = make(map[string][]models.Problem)
		for i, rec := range records {
			if i == 0 || len(rec) < 8 { // skip header and any short row
				continue
			}
			// Columns: company,id,slug,title,difficulty,frequency,acceptance,recent.
			id, _ := strconv.Atoi(rec[1])
			freq, _ := strconv.ParseFloat(rec[5], 64)
			acc, _ := strconv.ParseFloat(rec[6], 64)
			recent, _ := strconv.ParseBool(rec[7])
			byCompany[rec[0]] = append(byCompany[rec[0]], models.Problem{
				ID:         id,
				Title:      rec[3],
				Difficulty: rec[4],
				Frequency:  freq,
				Acceptance: acc,
				URL:        "https://leetcode.com/problems/" + rec[2],
				Recent:     recent,
			})
		}

		companySlugs = make([]string, 0, len(byCompany))
		for slug := range byCompany {
			companySlugs = append(companySlugs, slug)
		}
		sort.Strings(companySlugs)
	})
}

// Companies returns every company's pool summary, sorted by slug. The list is
// static, so callers can cache it. Returns nil if the embedded data failed to
// parse (a build-time invariant, so this should never happen in practice).
func Companies() []models.CompanyInfo {
	load()
	if parseErr != nil {
		return nil
	}
	out := make([]models.CompanyInfo, 0, len(companySlugs))
	for _, slug := range companySlugs {
		count := len(byCompany[slug])
		out = append(out, models.CompanyInfo{
			Slug:         slug,
			Name:         displayName(slug),
			ProblemCount: count,
			MockEligible: count >= mockMinPool,
		})
	}
	return out
}

// Problems returns a company's full problem list (frequency desc). Browse-side
// filtering and sorting happen on the frontend over this list.
func Problems(slug string) ([]models.Problem, error) {
	load()
	if parseErr != nil {
		return nil, parseErr
	}
	problems, ok := byCompany[slug]
	if !ok {
		return nil, fmt.Errorf("problems: unknown company %q", slug)
	}
	return problems, nil
}

// MockPair draws two interview questions for a company — an easier Q1 then a
// harder Q2, the way a real 45-minute screen runs. It seeds a fresh RNG so each
// call varies; see mockPair for the deterministic, testable core.
func MockPair(slug string) ([2]models.Problem, error) {
	problems, err := Problems(slug)
	if err != nil {
		return [2]models.Problem{}, err
	}
	return mockPair(problems, rand.New(rand.NewSource(time.Now().UnixNano())))
}

// mockPair implements the frequency-weighted, fallback-laden draw over a
// company's full problem pool. It is split from MockPair so tests can inject a
// seeded *rand.Rand for deterministic results. Rules:
//  1. Draw from the recent subset when it has >= recentPoolThreshold problems,
//     else the full pool (recency is the strongest "realistically asked" signal,
//     but only the big companies have enough of it).
//  2. Pick Q2 (the harder one) first: frequency-weighted from Medium+Hard, or
//     the whole draw pool if it has no Medium/Hard.
//  3. Pick Q1 (the easier one): frequency-weighted from strictly-lower tiers.
//     If none exist, fall back to Q2's own tier ordered by acceptance (higher =
//     easier first); if that is also empty, any other problem ordered by tier
//     then acceptance.
//
// Q1 is always different from Q2. It errors when the pool is smaller than
// mockMinPool, where a random draw is theatre rather than practice.
func mockPair(full []models.Problem, r *rand.Rand) ([2]models.Problem, error) {
	if len(full) < mockMinPool {
		return [2]models.Problem{}, fmt.Errorf("problems: pool too small for a mock interview (%d problems)", len(full))
	}

	// 1. Draw pool: recent subset when it's big enough, else the full pool.
	pool := full
	var recent []models.Problem
	for _, p := range full {
		if p.Recent {
			recent = append(recent, p)
		}
	}
	if len(recent) >= recentPoolThreshold {
		pool = recent
	}

	// 2. Q2 (harder): frequency-weighted from Medium+Hard, else the whole pool.
	var harder []int
	for i, p := range pool {
		if tier(p.Difficulty) >= tier("Medium") {
			harder = append(harder, i)
		}
	}
	if len(harder) == 0 {
		harder = allIndices(pool)
	}
	q2i := weightedPick(pool, harder, r)
	q2 := pool[q2i]

	// 3a. Q1 from strictly-lower tiers — guaranteed easier than Q2.
	var lower []int
	for i, p := range pool {
		if tier(p.Difficulty) < tier(q2.Difficulty) {
			lower = append(lower, i)
		}
	}
	if len(lower) > 0 {
		q1 := pool[weightedPick(pool, lower, r)]
		return [2]models.Problem{q1, q2}, nil
	}

	// 3b. No lower tier: Q2's own tier (excluding Q2), ordered by acceptance.
	var same []int
	for i, p := range pool {
		if i != q2i && tier(p.Difficulty) == tier(q2.Difficulty) {
			same = append(same, i)
		}
	}
	if len(same) > 0 {
		q1 := pool[weightedPick(pool, same, r)]
		return orderByAcceptance(q1, q2), nil
	}

	// 3c. Extreme degenerate: Q2 is the lone member of its tier and no lower tier
	//     exists (e.g. a single Medium among all-Hard). Pair with any other
	//     problem, ordered by tier then acceptance so the easier one leads.
	var others []int
	for i := range pool {
		if i != q2i {
			others = append(others, i)
		}
	}
	q1 := pool[weightedPick(pool, others, r)]
	return orderByTier(q1, q2), nil
}

// tier ranks a difficulty for ordering: Easy < Medium < Hard. Unknown values are
// treated as Medium; the committed data is canonical, so this is only defensive.
func tier(difficulty string) int {
	switch difficulty {
	case "Easy":
		return 0
	case "Hard":
		return 2
	default: // "Medium" and anything unexpected
		return 1
	}
}

// weightedPick returns an index into pool chosen from the candidate indices,
// weighted by each candidate's interview frequency. Zero/missing frequencies fall
// back to a uniform pick so an all-zero pool still returns something. idxs must
// be non-empty (callers guarantee it).
func weightedPick(pool []models.Problem, idxs []int, r *rand.Rand) int {
	total := 0.0
	for _, i := range idxs {
		if f := pool[i].Frequency; f > 0 {
			total += f
		}
	}
	if total <= 0 {
		return idxs[r.Intn(len(idxs))]
	}
	x := r.Float64() * total
	for _, i := range idxs {
		f := pool[i].Frequency
		if f <= 0 {
			continue
		}
		if x -= f; x < 0 {
			return i
		}
	}
	return idxs[len(idxs)-1] // guard against float rounding
}

// orderByAcceptance returns the pair with the higher-acceptance (perceived
// easier) problem first — used when the two share a tier.
func orderByAcceptance(a, b models.Problem) [2]models.Problem {
	if a.Acceptance >= b.Acceptance {
		return [2]models.Problem{a, b}
	}
	return [2]models.Problem{b, a}
}

// orderByTier returns the pair with the lower-tier (easier) problem first,
// breaking ties by acceptance.
func orderByTier(a, b models.Problem) [2]models.Problem {
	switch {
	case tier(a.Difficulty) < tier(b.Difficulty):
		return [2]models.Problem{a, b}
	case tier(b.Difficulty) < tier(a.Difficulty):
		return [2]models.Problem{b, a}
	default:
		return orderByAcceptance(a, b)
	}
}

// allIndices returns [0, 1, ..., len(pool)-1].
func allIndices(pool []models.Problem) []int {
	idx := make([]int, len(pool))
	for i := range pool {
		idx[i] = i
	}
	return idx
}

// DisplayName returns the human-readable company name for a slug (e.g.
// "goldman-sachs" -> "Goldman Sachs"), using the same overrides as the picker.
// Exported for callers (app.go) that need the label without scanning Companies().
func DisplayName(slug string) string {
	return displayName(slug)
}

// displayName turns a company slug into a human label: an override when present,
// else hyphens become spaces and each word is capitalised ("goldman-sachs" ->
// "Goldman Sachs").
func displayName(slug string) string {
	if name, ok := companyNameOverrides[slug]; ok {
		return name
	}
	words := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	if len(words) == 0 {
		return slug
	}
	return strings.Join(words, " ")
}
