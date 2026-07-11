// Command gen regenerates internal/problems/data/problems.csv from the upstream
// leetcode-company-wise-problems repository. It is run by the biweekly refresh
// workflow (.github/workflows/refresh-problems.yml) and manually via the
// //go:generate directive in the parent package; the app itself never runs it —
// the CSV is embedded at build time, so CI builds never touch the network.
//
// It downloads the repo tarball, keeps only the columns the app needs, joins
// problem ids and acceptance rates from LeetCode's public algorithms API (the
// same call that filters the data to algorithm-category problems), marks
// problems that appear in any recent-window file, and writes one sorted CSV
// plus a SOURCE.md attribution file. We ship factual metadata only (titles,
// links, frequencies) — never problem statements.
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	tarballURL = "https://codeload.github.com/liquidslr/leetcode-company-wise-problems/tar.gz/refs/heads/main"
	sourceRepo = "https://github.com/liquidslr/leetcode-company-wise-problems"
	// algorithmsURL lists every problem in LeetCode's "algorithms" category — the
	// coding problems we keep. Anything absent from it (SQL/database, pandas,
	// shell, concurrency, javascript) is filtered out so Company Practice stays
	// algorithmic. It also carries each problem's public number and submission
	// stats, which fill the id and acceptance columns the upstream repo lacks.
	// Unauthenticated and stable.
	algorithmsURL = "https://leetcode.com/api/problems/algorithms/"
	// minAlgorithms is a floor on the fetched allowlist size: the algorithms
	// category has thousands of problems, so a set smaller than this means the
	// endpoint changed or failed and must not be trusted to trim the CSV.
	minAlgorithms = 1000
	// allFile is the per-company file holding the company's full question pool.
	allFile = "5. All.csv"
	// minCompanies/minRows are floors on the generated output. The refresh job
	// runs unattended, so a silently restructured upstream (renamed files, moved
	// folders) must fail the run loudly rather than ship a gutted dataset.
	minCompanies = 400
	minRows      = 10000
)

// recentWindows are the per-company CSV files whose problems count as "recently
// asked". A problem's recent flag is set when its slug appears in any of them.
// ("5. All.csv" and "4. More Than Six Months.csv" are excluded — the former is
// the full pool, the latter is by definition not recent.) Filenames are matched
// exactly: if upstream renames them, rows collapse and the minRows gate trips.
var recentWindows = map[string]bool{
	"1. Thirty Days.csv":  true,
	"2. Three Months.csv": true,
	"3. Six Months.csv":   true,
}

// outRow is one line of the generated CSV.
type outRow struct {
	company    string // slug the app keys pools by, e.g. "goldman-sachs"
	id         int
	slug       string
	title      string
	difficulty string
	frequency  float64
	acceptance float64
	recent     bool
	name       string // upstream display name, e.g. "Goldman Sachs"
}

// companyData accumulates a single company's parsed files while streaming the
// tarball, keyed by the upstream folder name (a display name like "ByteDance").
// Entries for one company are not guaranteed to be contiguous, so we buffer per
// company and resolve the recent flag once everything is read.
type companyData struct {
	all    [][]string      // parsed "5. All.csv" records, including the header row
	recent map[string]bool // slugs seen in any recent-window file
}

// problemMeta is what the LeetCode algorithms API knows about one problem: its
// public number and acceptance rate. Presence in the map doubles as the
// algorithms-category allowlist.
type problemMeta struct {
	id         int
	acceptance float64
}

func main() {
	out := flag.String("out", "data", "output directory for problems.csv and SOURCE.md")
	flag.Parse()

	rows, companies, err := generate()
	if err != nil {
		log.Fatalf("gen: %v", err)
	}
	if err := writeCSV(filepath.Join(*out, "problems.csv"), rows); err != nil {
		log.Fatalf("gen: write csv: %v", err)
	}
	if err := writeSource(filepath.Join(*out, "SOURCE.md"), len(rows), companies); err != nil {
		log.Fatalf("gen: write source: %v", err)
	}
	fmt.Printf("gen: wrote %d rows across %d companies to %s\n", len(rows), companies, *out)
}

// generate downloads the tarball, parses every company's pool and recent-window
// files, joins ids/acceptance from the LeetCode API metadata, and returns the
// trimmed rows (sorted by company, then frequency desc) plus the number of
// distinct companies.
func generate() ([]outRow, int, error) {
	client := &http.Client{Timeout: 2 * time.Minute}

	// Fetch the problem metadata (allowlist + id/acceptance enrichment) first:
	// if it's unavailable we abort before downloading the (large) tarball, and
	// never risk trimming the CSV against a bad list.
	metaBySlug, err := fetchAlgorithmMeta(client)
	if err != nil {
		return nil, 0, err
	}

	resp, err := client.Get(tarballURL)
	if err != nil {
		return nil, 0, fmt.Errorf("download tarball: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("download tarball: HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	data := map[string]*companyData{}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read tar entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		folder, file, ok := splitEntry(hdr.Name)
		if !ok || (file != allFile && !recentWindows[file]) {
			continue
		}

		// A tar.Reader yields io.EOF at the end of the current entry, so the CSV
		// reader consumes exactly this file. Field counts vary slightly across the
		// upstream data, so we read leniently and validate per row below.
		r := csv.NewReader(tr)
		r.FieldsPerRecord = -1
		records, err := r.ReadAll()
		if err != nil {
			return nil, 0, fmt.Errorf("parse %s: %w", hdr.Name, err)
		}

		cd := data[folder]
		if cd == nil {
			cd = &companyData{recent: map[string]bool{}}
			data[folder] = cd
		}
		if file == allFile {
			cd.all = records
			continue
		}
		for i, rec := range records { // column 4 is the problem link
			if i == 0 || len(rec) < 5 {
				continue // header or short row
			}
			if slug := slugFromURL(rec[4]); slug != "" {
				cd.recent[slug] = true
			}
		}
	}

	var rows []outRow
	droppedRows := 0
	droppedSlugs := map[string]bool{} // distinct non-algorithm problems removed
	slugToFolder := map[string]string{}
	for folder, cd := range data {
		if len(cd.all) < 2 { // no "5. All.csv", or header only
			continue
		}
		company := slugifyCompany(folder)
		if company == "" {
			continue
		}
		// Two folders must never share a slug: merging their pools would corrupt
		// both companies, so refuse and let a human rename or special-case.
		if prev, ok := slugToFolder[company]; ok && prev != folder {
			return nil, 0, fmt.Errorf("company folders %q and %q both slugify to %q", prev, folder, company)
		}
		slugToFolder[company] = folder

		// "5. All.csv" columns: Difficulty,Title,Frequency,Acceptance Rate,Link,Topics.
		// Difficulty arrives uppercase; the acceptance-rate column is an opaquely
		// scaled decimal (not a percentage), so acceptance — like the problem id,
		// which upstream lacks entirely — comes from the API metadata instead.
		for _, rec := range cd.all[1:] {
			if len(rec) < 5 {
				continue
			}
			slug := slugFromURL(rec[4])
			title := strings.TrimSpace(rec[1])
			difficulty := normalizeDifficulty(rec[0])
			if slug == "" || title == "" || difficulty == "" {
				continue
			}
			// Keep only algorithm-category problems; the allowlist drops SQL
			// (database), pandas, shell, concurrency and javascript so Company
			// Practice stays algorithmic coding.
			meta, ok := metaBySlug[slug]
			if !ok {
				droppedRows++
				droppedSlugs[slug] = true
				continue
			}
			rows = append(rows, outRow{
				company:    company,
				id:         meta.id,
				slug:       slug,
				title:      title,
				difficulty: difficulty,
				frequency:  round1(parsePercent(rec[2])),
				acceptance: meta.acceptance,
				recent:     cd.recent[slug],
				name:       folder,
			})
		}
	}

	// Count companies from what survived the filter: a company whose pool was
	// entirely non-algorithmic drops out of the data the app actually sees.
	companySet := map[string]bool{}
	for _, r := range rows {
		companySet[r.company] = true
	}
	companies := len(companySet)
	log.Printf("gen: kept %d rows, dropped %d rows (%d distinct non-algorithm problems) across %d companies",
		len(rows), droppedRows, len(droppedSlugs), companies)

	// Output floors — see minCompanies/minRows. The google sentinel catches a
	// layout change that keeps volume but scrambles what a folder means.
	if companies < minCompanies || len(rows) < minRows {
		return nil, 0, fmt.Errorf("output too small: %d companies (min %d), %d rows (min %d) — upstream layout changed?",
			companies, minCompanies, len(rows), minRows)
	}
	if !companySet["google"] {
		return nil, 0, fmt.Errorf("output is missing google — upstream layout changed?")
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].company != rows[j].company {
			return rows[i].company < rows[j].company
		}
		if rows[i].frequency != rows[j].frequency {
			return rows[i].frequency > rows[j].frequency // most-asked first
		}
		return rows[i].id < rows[j].id // stable tiebreak
	})

	return rows, companies, nil
}

// fetchAlgorithmMeta returns per-slug metadata for every LeetCode algorithm
// problem: membership in the map is the allowlist that trims the company data,
// and the id/acceptance values fill the columns the upstream repo doesn't have.
// It validates the response is plausibly complete (a large set whose sentinel
// entry looks right) so a flaky or changed endpoint can't silently gut the
// committed CSV.
func fetchAlgorithmMeta(client *http.Client) (map[string]problemMeta, error) {
	resp, err := client.Get(algorithmsURL)
	if err != nil {
		return nil, fmt.Errorf("fetch algorithms list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch algorithms list: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		StatStatusPairs []struct {
			Stat struct {
				Slug           string `json:"question__title_slug"`
				FrontendID     int    `json:"frontend_question_id"` // the public problem number
				TotalAccepted  int64  `json:"total_acs"`
				TotalSubmitted int64  `json:"total_submitted"`
			} `json:"stat"`
		} `json:"stat_status_pairs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode algorithms list: %w", err)
	}

	meta := make(map[string]problemMeta, len(payload.StatStatusPairs))
	for _, p := range payload.StatStatusPairs {
		s := strings.TrimSpace(p.Stat.Slug)
		if s == "" {
			continue
		}
		m := problemMeta{id: p.Stat.FrontendID}
		if p.Stat.TotalSubmitted > 0 {
			m.acceptance = round1(float64(p.Stat.TotalAccepted) / float64(p.Stat.TotalSubmitted) * 100)
		}
		meta[s] = m
	}

	// Sanity gate: refuse an implausible list rather than dropping most of the
	// CSV or writing junk ids. two-sum is problem #1 with a mid-range acceptance.
	ts, ok := meta["two-sum"]
	if len(meta) < minAlgorithms || !ok || ts.id != 1 || ts.acceptance <= 0 || ts.acceptance >= 100 {
		return nil, fmt.Errorf("algorithms list looks wrong: %d slugs (min %d), two-sum=%+v (present=%v)",
			len(meta), minAlgorithms, ts, ok)
	}
	return meta, nil
}

// splitEntry parses a tar entry name like "<repo>-main/Google/5. All.csv" into
// ("Google", "5. All.csv", true). It returns ok=false for the top-level dir,
// the company directory entries themselves, and any non-CSV file.
func splitEntry(name string) (folder, file string, ok bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 3 { // repo-root / company / file.csv
		return "", "", false
	}
	folder, file = parts[1], parts[2]
	if folder == "" || !strings.HasSuffix(file, ".csv") {
		return "", "", false
	}
	return folder, file, true
}

// slugifyCompany converts an upstream folder name (a display name like
// "Booking.com" or "Goldman Sachs") into the stable slug the app keys pools by:
// lowercase, with each run of non-alphanumerics collapsed to a hyphen
// ("booking-com", "goldman-sachs"). Matches the previous dataset's slug scheme
// so slugs already stored in preferences survive the source switch.
func slugifyCompany(name string) string {
	var b strings.Builder
	pending := false // a separator run is buffered until the next alphanumeric
	for _, r := range strings.ToLower(name) {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !alnum {
			pending = true
			continue
		}
		if pending && b.Len() > 0 {
			b.WriteByte('-')
		}
		pending = false
		b.WriteRune(r)
	}
	return b.String()
}

// normalizeDifficulty maps the upstream's uppercase difficulty ("EASY") to the
// canonical form the app renders ("Easy"). Unknown values return "" and the row
// is dropped rather than shipping a non-canonical difficulty.
func normalizeDifficulty(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "EASY":
		return "Easy"
	case "MEDIUM":
		return "Medium"
	case "HARD":
		return "Hard"
	}
	return ""
}

// slugFromURL reduces a LeetCode problem URL to its slug, e.g.
// "https://leetcode.com/problems/two-sum" -> "two-sum". A trailing slash and any
// query/fragment are dropped.
func slugFromURL(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "/problems/"); i >= 0 {
		u = u[i+len("/problems/"):]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return u
}

// parsePercent strips a trailing "%" and parses the number, e.g. "57.5%" ->
// 57.5. (The current upstream ships bare numbers; the strip is a no-op there.)
// Unparseable values become 0.
func parsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// round1 rounds to one decimal place. Frequencies and acceptance rates carry no
// meaningful precision beyond that, and short numbers keep the committed CSV
// compact and its refresh diffs small.
func round1(f float64) float64 {
	return math.Round(f*10) / 10
}

// writeCSV writes the trimmed rows with a header, creating the output directory
// if needed. encoding/csv quotes any field containing a comma.
func writeCSV(path string, rows []outRow) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write([]string{"company", "id", "slug", "title", "difficulty", "frequency", "acceptance", "recent", "name"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			r.company,
			strconv.Itoa(r.id),
			r.slug,
			r.title,
			r.difficulty,
			formatPercent(r.frequency),
			formatPercent(r.acceptance),
			strconv.FormatBool(r.recent),
			r.name,
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// formatPercent renders a percentage as the shortest string that round-trips
// (e.g. 100, 57.5, 0), keeping the committed file compact and diff-friendly.
func formatPercent(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// writeSource writes the attribution/provenance file that ships next to the CSV.
func writeSource(path string, rowCount, companyCount int) error {
	content := fmt.Sprintf("# Problem data source\n\n"+
		"`problems.csv` in this directory is generated — do not edit it by hand.\n\n"+
		"- **Source:** [liquidslr/leetcode-company-wise-problems](%s)\n"+
		"  (company-tagged question lists with interview frequencies and recency windows).\n"+
		"- **Enrichment:** problem ids and acceptance rates are joined from LeetCode's\n"+
		"  public `algorithms` API — the same call that filters the data (below).\n"+
		"- **Snapshot:** %s\n"+
		"- **Rows:** %d problems across %d companies.\n"+
		"- **Columns:** `company,id,slug,title,difficulty,frequency,acceptance,recent,name`.\n\n"+
		"We ship **factual metadata only** — titles, difficulties, frequencies, and\n"+
		"acceptance rates — never problem statements. LeetCode links are rebuilt from\n"+
		"the slug (`https://leetcode.com/problems/{slug}`), so only the slug is stored.\n\n"+
		"**Algorithm problems only.** Rows are filtered against LeetCode's `algorithms`\n"+
		"category, so SQL (database), Pandas, Shell, Concurrency and JavaScript problems\n"+
		"are excluded — this app is an algorithmic coding interview.\n\n"+
		"Regenerate with:\n\n"+
		"    go generate ./internal/problems/\n\n"+
		"The biweekly refresh workflow (`.github/workflows/refresh-problems.yml`) runs\n"+
		"the same command on a schedule and opens a PR when the data changed.\n\n"+
		"The upstream repository has no license file; we regenerate the data rather\n"+
		"than vendor the repo, and credit the author here and in the project README.\n",
		sourceRepo, time.Now().UTC().Format("2006-01-02"), rowCount, companyCount)
	return os.WriteFile(path, []byte(content), 0o644)
}
