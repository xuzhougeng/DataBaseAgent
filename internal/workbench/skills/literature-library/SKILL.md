---
name: literature-library
description: Builds a static card library from existing papers, tutorials or academic resource lists, for searching by title and reading summaries.
metadata:
  dba:
    label: Literature library
    layout: cards
    accent: '#466b9c'
    required-columns: [title, summary]
    title-column: title
---

# Literature library

For DBA existing-data (static) projects, using literature or resource records the user already has.

Each record must have `title` and `summary` columns and may keep `authors`, `year`, `doi`, `url` and `tags`. Show what the user provided; do not invent summaries, authors, DOIs or years from a title. Explain the mapping when fields need renaming. If there is no summary, first confirm whether to use an existing description or leave it blank.

Get this skill's `skillId` with `read_skill`. Pass the real CSV, TSV or JSON to `build_static_site` together with the site title, description and `skillId`. The template uses the title as the card heading, shows the other fields as given, and supports local keyword search.

When done, report the real record count and point to the preview and HTML download on the Static website page. This template does not configure a database, deploy a dynamic app or fetch missing records online.
