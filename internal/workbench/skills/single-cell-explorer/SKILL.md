---
name: single-cell-explorer
description: Designs an interactive static website from processed single-cell embeddings, metadata and optional gene-expression columns.
---

# Single-cell explorer

Help users share already analyzed single-cell results as an interactive, standalone
website. Choose the layout and interactions from their data and audience. This is
an authoring skill, not a fixed template. Use it for scRNA-seq or snRNA-seq result
explorers, embedding viewers, annotated cell atlases and marker summaries.

## Inspect before designing

Use `inspect_site_data` on the intended static project. It returns the complete
column list, row count, missingness, numeric ranges, category counts (up to 30
categories) and a small sample. Read further sample rows only when needed.
The user imports CSV/TSV/JSON with **Save data for Agent design**. If no input is
saved, explain that action. Do not ask them to paste thousands of cells into chat.

Identify the stable cell ID, available embedding coordinate pairs (UMAP, t-SNE,
PCA or other named methods), cluster/cell-type/sample fields and numeric expression
or QC columns. Do not assume fixed column names; map the observed schema in code.
Only offer controls backed by actual columns. If no embedding exists, a searchable
metadata/marker explorer can still be useful; do not invent coordinates.

Preserve supplied coordinates and labels. A cluster number is not a biological
cell type. Do not annotate from position or rename t-SNE to UMAP. Do not claim to
rerun QC, normalization, clustering or differential expression. Raw FASTQ, RDS and
H5AD processing is outside these static-site tools; request exported result tables
or explain that a separate analysis workflow is needed. Describe gene expression
units/normalization only when provided. Note absent or invalid coordinates and
report displayed versus total cells; never silently subsample.

## Design and build

Use `build_custom_site` with this skill ID and your own complete HTML document.
DBA injects all imported data before your scripts:

```js
const { title, description, columns, rows } = window.DBA_DATA;
const columnIndex = Object.fromEntries(columns.map((name, i) => [name, i]));
// rows are arrays of strings. Convert numeric fields explicitly; blank is missing.
```

Do not reproduce data in generated code or call an API to retrieve it. Keep HTML,
CSS and JavaScript inline; there is no CDN, npm dependency or network access. Use
Canvas or inline SVG for plots. Do not include CSP/http-equiv, frames, forms, base
URLs, external scripts or stylesheets. Use textContent for untrusted labels and
cell metadata; avoid injecting values through innerHTML. HTTPS citation links are
allowed. Generated code runs in a sandboxed preview and is exported in one HTML.

For cell embeddings, useful choices include categorical coloring with a stable
legend, cluster/sample filtering, cell-ID search, hover/click details, zoom/reset,
and a compact paginated metadata table. Make filtering affect plot, counts and
table consistently. Consider continuous coloring when meaningful numeric columns
exist. Do not manufacture gene-expression controls without expression data.
Use readable labels, responsive layout, keyboard-operable controls and a visible
no-results state. Keep interactions responsive at the actual data size.

Choose the visual design yourself. Avoid forcing every dataset into the same
layout. Preserve data attribution and distinguish computed summaries from supplied
analysis. Show total cells and embedding/clustering provenance prominently.

## Verify and report

A successful tool call confirms storage and structural checks, not working
JavaScript. Check `get_static_site` for the saved title and record count. Direct the
user to the website preview and HTML download; if browser tools are available,
actually test initial rendering, filtering/search, cell detail, reset and the
standalone export. Fix failures with another build_custom_site call. Never claim
visual or offline verification from a save response alone. Publishing to an
external host is separate from generating/downloading and follows the user's
chosen destination and existing approval rules.
