---
name: research-catalog
description: Turns existing research datasets into a searchable static data catalogue for browsing datasets by accession, organism and assay.
metadata:
  dba:
    label: Research data catalogue
    layout: table
    accent: '#237c63'
    required-columns: [id, title]
    title-column: title
---

# Research data catalogue

For DBA existing-data (static) projects. Read the project and the real data the user provided first.

Each row is one dataset and must have `id` and `title` columns. Keep original fields such as `organism`, `assay`, `source` and `url`; leave missing information blank. When the user's column names differ, explain the mapping and keep the original meaning. Do not invent sources or accessions.

Use `list_skills` to see the catalogue and `read_skill` to read this template. Once the title, description and fields are confirmed, call `build_static_site` with this skill's `skillId` and the complete CSV, TSV or JSON data. The result is a local searchable table that the user can preview on the Static website page and download as a standalone HTML file.

If data is missing, ask for the missing input; do not start a search or remote processing. When done, report the actual record count and where to preview it. Unless the site has been published, do not say it is live.
