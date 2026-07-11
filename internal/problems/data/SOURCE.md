# Problem data source

`problems.csv` in this directory is generated — do not edit it by hand.

- **Source:** [liquidslr/leetcode-company-wise-problems](https://github.com/liquidslr/leetcode-company-wise-problems)
  (company-tagged question lists with interview frequencies and recency windows).
- **Enrichment:** problem ids and acceptance rates are joined from LeetCode's
  public `algorithms` API — the same call that filters the data (below).
- **Snapshot:** 2026-07-11
- **Rows:** 13901 problems across 438 companies.
- **Columns:** `company,id,slug,title,difficulty,frequency,acceptance,recent,name`.

We ship **factual metadata only** — titles, difficulties, frequencies, and
acceptance rates — never problem statements. LeetCode links are rebuilt from
the slug (`https://leetcode.com/problems/{slug}`), so only the slug is stored.

**Algorithm problems only.** Rows are filtered against LeetCode's `algorithms`
category, so SQL (database), Pandas, Shell, Concurrency and JavaScript problems
are excluded — this app is an algorithmic coding interview.

Regenerate with:

    go generate ./internal/problems/

The biweekly refresh workflow (`.github/workflows/refresh-problems.yml`) runs
the same command on a schedule and opens a PR when the data changed.

The upstream repository has no license file; we regenerate the data rather
than vendor the repo, and credit the author here and in the project README.
