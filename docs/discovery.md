# Discovery

Discovery is deliberately *broad*: the goal is recall, and precision is restored later by evidence-backed review. Every search records how many records the source reported and how many DBA actually fetched, so the ledger always shows what was and was not examined.

## Sources

| Source | API | Paging | Record fields |
| --- | --- | --- | --- |
| Europe PMC | `europepmc/webservices/rest/search` (`resultType=core`) | `cursorMark`, 100 per page | title, abstract, DOI, `PMID:` accession (or EPMC id), year, article URL |
| PubMed | NCBI E-utilities `esearch` (JSON) + `efetch` (XML) | id list, fetched in batches of 100 with a 400 ms pause | title, abstract paragraphs, `PMID:` accession, DOI, year, PubMed URL |
| GEO | NCBI E-utilities `esearch` (`db=gds`) + `esummary` (JSON) | id list, batches of 100 with a 400 ms pause | title, summary, accession (GSE / GSM / GPL), organism, GEO URL |
| Import | JSON array pasted in the UI | — | any subset of the candidate fields; `title` is required; 1–1000 records per import |

Limits per search: 1–1000 records (default 100). Searches are serialized — a second search is refused while one is running — and the HTTP client follows `HTTP_PROXY` / `HTTPS_PROXY`. HTML tags are stripped from Europe PMC titles and abstracts.

GEO results mix series, samples and platforms. DBA keeps them all; filtering to series is a review decision.

## De-duplication

Candidates are merged into the project with three keys, any of which marks a duplicate:

1. normalized title (lower-cased, whitespace collapsed),
2. DOI (lower-cased, `https://doi.org/` and `doi:` prefixes removed),
3. accession (lower-cased).

New records enter as `pending` with every agent and review field cleared. Re-running the same query therefore reports *fetched N, added 0* instead of growing the list.

## Search ledger

Each run appends a `SearchRecord`:

```
{ id, query, source, total, fetched, added, at }
```

`total` is the hit count reported by the source at that moment (Europe PMC `hitCount`, NCBI `count`); `fetched` is bounded by the limit; `added` is what survived de-duplication. The project's current query and source are updated to the last run. Failed searches are recorded as `error` events with the source and message; nothing is fabricated.

## Import format

```json
[
  {
    "title": "Study or dataset title",
    "source": "GEO",
    "accession": "GSE...",
    "organism": "Homo sapiens",
    "year": "2024",
    "doi": "10.1000/example",
    "url": "https://...",
    "downloadUrl": "",
    "abstract": "Abstract or experiment description"
  }
]
```

Imports go through the same merge, so curated lists from other tools can be combined with API searches without duplicates.

## Review hand-off

After discovery, candidates appear on the review page grouped by decision. The Reviewer agent can propose a suggestion with a confidence level and a verbatim quote; the quote is checked against the stored title and abstract before it is saved. Human review requires a written reason and may add the dataset accession, a direct download URL (HTTP/HTTPS without credentials) and sample groups (`sample, group` per line, at least two samples and two groups). Only `accepted` candidates are eligible for jobs.
