# SKILL templates and project modes

DBA supports two project modes. **Existing data / `static`** skips discovery and remote pipelines: choose a SKILL template, import existing data, generate a searchable website, preview it, and download a standalone `index.html`. **Collect data / `collect`** keeps discovery, review, processing, analysis and archival. This mode stops at data and analysis outputs; database integration and dynamic website development are not implemented. Existing projects migrate to `collect`. A project's mode is selected on creation and cannot be changed in place.

## Using templates

Open **All projects**, choose **Build a website from your data**, name the project, then select a template on **Static website**. Paste data or import a UTF-8 CSV, TSV or JSON file. CSV/TSV use their first row as headers. JSON uses an array of objects; columns are the sorted union of object keys. Missing JSON values display as empty cells, numeric values retain their precision, and nested values display as JSON text. Limits: 2 MB, 10,000 records and 100 columns. Header names are case-sensitive and must match the template's required fields.

Two fixed templates and one authoring skill ship with new workspaces:

| SKILL | Layout | Required columns | Intended use |
| --- | --- | --- | --- |
| `research-catalog` | Searchable table | `id`, `title` | Research datasets; optional organism, assay and source fields |
| `literature-library` | Searchable cards | `title`, `summary` | Literature and resources; optional authors, year and DOI fields |
| `single-cell-explorer` | Agent-designed HTML | Inspects the imported schema | Processed single-cell embeddings and metadata; no fixed layout |

For an Agent-designed website, import the file and choose **Save data for Agent design**, then **Let the Agent design**. The Agent reads the appropriate SKILL and uses `inspect_site_data` before writing HTML/CSS/JavaScript with `build_custom_site`. This does not require a declarative template. Existing workspaces can import [single-cell-explorer/SKILL.md](../internal/workbench/skills/single-cell-explorer/SKILL.md); deleted skills are not automatically restored.

Each generated website embeds its data and works offline. The fixed templates need no model. Agent authoring requires a configured model; neither path requires a database or hosting setup to generate a local preview. Generation does not publish the site. Downloading the HTML includes the imported data in that file.

## Author a SKILL template

The **SKILL templates** page supports importing a `SKILL.md`, inspecting its full content, enabling/disabling it, deleting it, or inserting `$skill-name` into the current conversation. Imports are stored in `workbench.json`; the original file is not modified. Duplicate names are rejected. To replace a template, delete the installed version and import its replacement. Existing websites retain a snapshot of their template and data, including after a skill is disabled or deleted. The next build must use an enabled skill (and an enabled template for fixed layouts).

A template is a declarative extension under the standard SKILL frontmatter's `metadata.dba` key:

```markdown
---
name: protein-catalog
description: Build a static catalogue from an existing protein annotation table.
metadata:
  dba:
    label: Protein catalogue
    layout: table
    accent: '#237c63'
    required-columns: [accession, name]
    title-column: name
---

# Protein catalogue

Use the user's existing protein annotations. Each record needs accession and
name columns. Retain optional species, function and source columns. Do not
infer missing identifiers or annotations.

Read the project and this skill, then call build_static_site with this skill's
skillId, the site title, description, format, and complete input data.
Report the real record count and the preview/download entry point.
```

Supported template fields:

| Field | Meaning |
| --- | --- |
| `label` | Required display name, at most 120 bytes |
| `layout` | Required: `table` or `cards` |
| `accent` | Optional six-digit hex color; defaults to `#237c63` |
| `required-columns` | Optional list of required header names (presence, not nonempty-value validation) |
| `title-column` | Card heading field; required for cards. If specified for either layout, it must exist in the input |

The complete UTF-8 SKILL.md is limited to 12,000 bytes, with a lowercase hyphenated name of up to 64 characters, a description of up to 1,024 bytes, and nonempty body instructions. Up to 50 skills may be installed. Ordinary instruction skills without `metadata.dba` are also supported but do not appear in the website template selector. Declarative templates do not contain executable code. Separately, the main Agent can author inline HTML/CSS/JavaScript with `build_custom_site`; supporting asset files, filesystem discovery and bundled skill-script execution are not supported.

## Agent integration and boundaries

The main agent and specialists receive enabled skill names/descriptions and can call `list_skills` and `read_skill`. Skill bodies load on demand. Users can invoke `$name`, or let the agent choose a matching skill. `read_skill` returns the stable `skillId`, instructions and parsed template. Only the main agent has `build_static_site`, `inspect_site_data`, `build_custom_site` and `get_static_site`; skills cannot expand specialist permissions or register executable tools. Skill imports/deletion/enabling are manual configuration operations.

`create_project` accepts `mode: "static" | "collect"`, defaulting to `collect` for older callers. `get_project` and `list_projects` expose the mode. Agent prompts include the mode boundary in both main and delegated conversations. Static projects reject discovery, review and pipeline mutation commands; collection projects reject website generation.

`POST /api/buildStaticSite` accepts `projectId`, `skillId`, `name`, `description`, `format` and `data`. Parsing, template validation and storage happen in one store transaction: a failed build preserves the previous website. `GET /api/site?projectId=...` renders the saved snapshot; add `&download=1` for an HTML attachment. Preview frames are sandboxed, imported cells are escaped by Go's `html/template`, and a fixed local script implements search. Fixed templates cannot add scripts or network requests. Agent-authored sites use the separate custom-site tool described below. Building through an Agent uses the same command, permission and validation layer as the UI; the model's context limits may make manual file import more suitable for large datasets.

Built-in source files are embedded from `internal/workbench/skills/*/SKILL.md` and are written in English; the Chinese interface shows their label and description in Chinese. They are installed once when a workspace has no skills field. Existing workspaces retain their skill list; import newly added skills explicitly. An intentionally empty skills list is preserved across restarts. This is an additive schema-3 migration. Workspaces created while the built-ins were Chinese switch them to English on open, but only copies whose text is unchanged; edited or re-imported skills and the template snapshots in generated websites keep their text.


## Data-driven Agent authoring

`POST /api/importSiteData` accepts `projectId`, `name`, `description`, `format` and
`data`. It validates and saves a project's input separately from its generated
website. Re-importing data does not change the previously generated site.
`inspect_site_data` returns columns, counts, missingness, finite numeric ranges,
category counts when there are at most 30 categories, and a bounded sample
(default 5 rows, maximum 100). It never sends the complete cell table by default.

`build_custom_site` / `POST /api/buildCustomSite` accepts `projectId`, `skillId`,
`name`, `description` and `html`. An enabled instruction skill is sufficient;
`metadata.dba` is optional. The HTML limit is 512 KiB. The builder snapshots the
saved input, and injects `window.DBA_DATA = {title, description, columns, rows}`
before any author script. All row values are strings. JSON encoding escapes HTML
metacharacters, including script-closing strings. The Agent chooses charts,
layout and interactions; the service does not impose a single-cell renderer.

HTML is parsed, external resource attributes and navigation metadata are rejected,
and a CSP is inserted before scripts. Inline scripts/styles and data images are
allowed; fetch, external assets, frames and forms are blocked. Preview additionally
uses an HTTP CSP sandbox and an iframe without same-origin permission. HTTPS source
links can be included. This is structural validation, not proof that model-written
JavaScript is correct. Test the preview and exported HTML before sharing. A generated
page may misrepresent data or fail to render; inspect the result and ask the Agent
to rebuild when necessary. No deployment happens automatically.

See the [PBMC5k walkthrough](walkthroughs/pbmc5k/README.md) for a real Agent run,
recorded interactions, screenshots, data provenance and the exported HTML.

Model calls allow up to five minutes for complete-page generation; cancellation still interrupts the request. Discovery requests retain their 90-second timeout.
