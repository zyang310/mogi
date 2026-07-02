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
	companies := 0
	for company, cd := range data {
		if len(cd.all) < 2 { // no all.csv, or header only
			continue
		}
		companies++
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
		"Regenerate with:\n\n"+
		"    go generate ./internal/problems/\n\n"+
		"The upstream repository has no license file; we regenerate the data rather\n"+
		"than vendor the repo, and credit the author here and in the project README.\n",
		sourceRepo, time.Now().UTC().Format("2006-01-02"), rowCount, companyCount)
	return os.WriteFile(path, []byte(content), 0o644)
}
