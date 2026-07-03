# Problem data source

`problems.csv` in this directory is generated — do not edit it by hand.

- **Source:** [snehasishroy/leetcode-companywise-interview-questions](https://github.com/snehasishroy/leetcode-companywise-interview-questions)
  (scraped from LeetCode's premium company-frequency filter).
- **Snapshot:** 2026-07-03
- **Rows:** 16914 problems across 647 companies.
- **Columns:** `company,id,slug,title,difficulty,frequency,acceptance,recent`.

We ship **factual metadata only** — titles, difficulties, frequencies, and
acceptance rates — never problem statements. LeetCode links are rebuilt from
the slug (`https://leetcode.com/problems/{slug}`), so only the slug is stored.

**Algorithm problems only.** Rows are filtered against LeetCode's `algorithms`
category, so SQL (database), Pandas, Shell, Concurrency and JavaScript problems
are excluded — this app is an algorithmic coding interview.

Regenerate with:

    go generate ./internal/problems/

The upstream repository has no license file; we regenerate the data rather
than vendor the repo, and credit the author here and in the project README.
