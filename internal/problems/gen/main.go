// Command gen regenerates internal/problems/data/problems.csv from the upstream
// leetcode-companywise-interview-questions repository. It is run manually to
// refresh the committed snapshot (see the //go:generate directive in the parent
// package); the app itself never runs it — the CSV is embedded at build time, so
// CI builds never touch the network.
//
// It downloads the repo tarball, keeps only the columns the app needs, marks
// problems that appear in any recent-window file, and writes one sorted CSV plus
// a SOURCE.md attribution file. We ship factual metadata only (titles, links,
// frequencies) — never problem statements.
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
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	tarballURL = "https://codeload.github.com/snehasishroy/leetcode-companywise-interview-questions/tar.gz/refs/heads/master"
	sourceRepo = "https://github.com/snehasishroy/leetcode-companywise-interview-questions"
	// algorithmsURL lists every problem in LeetCode's "algorithms" category — the
	// coding problems we keep. Anything absent from it (SQL/database, pandas,
	// shell, concurrency, javascript) is filtered out so Company Practice stays
	// algorithmic. Unauthenticated and stable.
	algorithmsURL = "https://leetcode.com/api/problems/algorithms/"
	// minAlgorithms is a floor on the fetched allowlist size: the algorithms
	// category has thousands of problems, so a set smaller than this means the
	// endpoint changed or failed and must not be trusted to trim the CSV.
	minAlgorithms = 1000
)

// recentWindows are the per-company CSV files whose problems count as "recently
// asked". A problem's recent flag is set when its slug appears in any of them.
// (all.csv and more-than-six-months.csv are excluded — the former is the full
// pool, the latter is by definition not recent.)
var recentWindows = map[string]bool{
	"thirty-days.csv":  true,
	"three-months.csv": true,
	"six-months.csv":   true,
}

// outRow is one line of the generated CSV.
type outRow struct {
	company    string
	id         int
	slug       string
	title      string
	difficulty string
	frequency  float64
	acceptance float64
	recent     bool
}

// companyData accumulates a single company's parsed files while streaming the
// tarball. Entries for one company are not guaranteed to be contiguous, so we
// buffer per company and resolve the recent flag once everything is read.
type companyData struct {
	all    [][]string      // parsed all.csv records, including the header row
	recent map[string]bool // slugs seen in any recent-window file
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

// generate downloads the tarball, parses every company's all.csv and
// recent-window files, and returns the trimmed rows (sorted by company, then
// frequency desc) plus the number of distinct companies.
func generate() ([]outRow, int, error) {
	client := &http.Client{Timeout: 2 * time.Minute}

	// Fetch the algorithm-problem allowlist first: if it's unavailable we abort
	// before downloading the (large) tarball, and never risk trimming the CSV
	// against a bad list.
	keepSlugs, err := fetchAlgorithmSlugs(client)
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
		company, file, ok := splitEntry(hdr.Name)
		if !ok || (file != "all.csv" && !recentWindows[file]) {
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

		cd := data[company]
		if cd == nil {
			cd = &companyData{recent: map[string]bool{}}
			data[company] = cd
		}
		if file == "all.csv" {
			cd.all = records
			continue
		}
		for _, rec := range records[1:] { // skip header; column 1 is the URL
			if len(rec) < 2 {
				continue
			}
			if slug := slugFromURL(rec[1]); slug != "" {
				cd.recent[slug] = true
			}
		}
	}

	var rows []outRow
	droppedRows := 0
	droppedSlugs := map[string]bool{} // distinct non-algorithm problems removed
	for company, cd := range data {
		if len(cd.all) < 2 { // no all.csv, or header only
			continue
		}
		// all.csv columns: ID,URL,Title,Difficulty,Acceptance %,Frequency %.
		for _, rec := range cd.all[1:] {
			if len(rec) < 6 {
				continue
			}
			slug := slugFromURL(rec[1])
			title := strings.TrimSpace(rec[2])
			if slug == "" || title == "" {
				continue
			}
			// Keep only algorithm-category problems; the allowlist drops SQL
			// (database), pandas, shell, concurrency and javascript so Company
			// Practice stays algorithmic coding.
			if !keepSlugs[slug] {
				droppedRows++
				droppedSlugs[slug] = true
				continue
			}
			id, _ := strconv.Atoi(strings.TrimSpace(rec[0]))
			rows = append(rows, outRow{
				company:    company,
				id:         id,
				slug:       slug,
				title:      title,
				difficulty: strings.TrimSpace(rec[3]),
				acceptance: parsePercent(rec[4]),
				frequency:  parsePercent(rec[5]),
				recent:     cd.recent[slug],
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

// fetchAlgorithmSlugs returns the set of LeetCode algorithm-problem slugs, used
// as an allowlist when trimming the company data. It validates the response is
// plausibly complete (a large set containing a known sentinel) so a flaky or
// changed endpoint can't silently gut the committed CSV.
func fetchAlgorithmSlugs(client *http.Client) (map[string]bool, error) {
	resp, err := client.Get(algorithmsURL)
	if err != nil {
		return nil, fmt.Errorf("fetch algorithms list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch algorithms list: HTTP %d", resp.StatusCode)
	}

	// The endpoint returns one entry per problem; we only need each slug.
	var payload struct {
		StatStatusPairs []struct {
			Stat struct {
				Slug string `json:"question__title_slug"`
			} `json:"stat"`
		} `json:"stat_status_pairs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode algorithms list: %w", err)
	}

	slugs := make(map[string]bool, len(payload.StatStatusPairs))
	for _, p := range payload.StatStatusPairs {
		if s := strings.TrimSpace(p.Stat.Slug); s != "" {
			slugs[s] = true
		}
	}

	// Sanity gate: refuse an implausible list rather than dropping most of the CSV.
	if len(slugs) < minAlgorithms || !slugs["two-sum"] {
		return nil, fmt.Errorf("algorithms list looks wrong: %d slugs (< %d) or missing sentinel two-sum (present=%v)",
			len(slugs), minAlgorithms, slugs["two-sum"])
	}
	return slugs, nil
}

// splitEntry parses a tar entry name like "<repo>-master/google/all.csv" into
// ("google", "all.csv", true). It returns ok=false for the top-level dir, the
// company directory entries themselves, and any non-CSV file.
func splitEntry(name string) (company, file string, ok bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 3 { // repo-root / company / file.csv
		return "", "", false
	}
	company, file = parts[1], parts[2]
	if company == "" || !strings.HasSuffix(file, ".csv") {
		return "", "", false
	}
	return company, file, true
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
// 57.5. Unparseable values become 0.
func parsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// writeCSV writes the trimmed rows with a header, creating the output directory
// if needed. encoding/csv quotes any title containing a comma.
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
	if err := w.Write([]string{"company", "id", "slug", "title", "difficulty", "frequency", "acceptance", "recent"}); err != nil {
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
		"- **Source:** [snehasishroy/leetcode-companywise-interview-questions](%s)\n"+
		"  (scraped from LeetCode's premium company-frequency filter).\n"+
		"- **Snapshot:** %s\n"+
		"- **Rows:** %d problems across %d companies.\n"+
		"- **Columns:** `company,id,slug,title,difficulty,frequency,acceptance,recent`.\n\n"+
		"We ship **factual metadata only** — titles, difficulties, frequencies, and\n"+
		"acceptance rates — never problem statements. LeetCode links are rebuilt from\n"+
		"the slug (`https://leetcode.com/problems/{slug}`), so only the slug is stored.\n\n"+
		"**Algorithm problems only.** Rows are filtered against LeetCode's `algorithms`\n"+
		"category, so SQL (database), Pandas, Shell, Concurrency and JavaScript problems\n"+
		"are excluded — this app is an algorithmic coding interview.\n\n"+
		"Regenerate with:\n\n"+
		"    go generate ./internal/problems/\n\n"+
		"The upstream repository has no license file; we regenerate the data rather\n"+
		"than vendor the repo, and credit the author here and in the project README.\n",
		sourceRepo, time.Now().UTC().Format("2006-01-02"), rowCount, companyCount)
	return os.WriteFile(path, []byte(content), 0o644)
}
