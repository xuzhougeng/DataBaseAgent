# SKILL templates and project modes

DBA supports two project modes. **Existing data / `static`** skips discovery and remote pipelines: choose a SKILL template, import existing data, generate a searchable website, preview it, and download a standalone `index.html`. **Collect data / `collect`** keeps discovery, review, processing, analysis and archival. This mode stops at data and analysis outputs; database integration and dynamic website development are not implemented. Existing projects migrate to `collect`. A project's mode is selected on creation and cannot be changed in place.

## Using templates

Open **All projects**, choose **Build a website from your data**, name the project, then select a template on **Static website**. Paste data or import a UTF-8 CSV, TSV or JSON file. CSV/TSV use their first row as headers. JSON uses an array of objects; columns are the sorted union of object keys. Missing JSON values display as empty cells, numeric values retain their precision, and nested values display as JSON text. Limits: 2 MB, 5,000 records and 100 columns. Header names are case-sensitive and must match the template's required fields.

Two templates ship with the app:

| SKILL | Layout | Required columns | Intended use |
| --- | --- | --- | --- |
| `research-catalog` | Searchable table | `id`, `title` | Research datasets; optional organism, assay and source fields |
| `literature-library` | Searchable cards | `title`, `summary` | Literature and resources; optional authors, year and DOI fields |

Each generated website embeds its data and works offline. No model, server configuration, database or external assets are required to generate it manually. Generation does not publish the site. Downloading the HTML includes the imported data in that file.

## Author a SKILL template

The **SKILL templates** page supports importing a `SKILL.md`, inspecting its full content, enabling/disabling it, deleting it, or inserting `$skill-name` into the current conversation. Imports are stored in `workbench.json`; the original file is not modified. Duplicate names are rejected. To replace a template, delete the installed version and import its replacement. Existing websites retain a snapshot of their template and data, including after a skill is disabled or deleted. The next build must use an enabled template.

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

The complete UTF-8 SKILL.md is limited to 12,000 bytes, with a lowercase hyphenated name of up to 64 characters, a description of up to 1,024 bytes, and nonempty body instructions. Up to 50 skills may be installed. Ordinary instruction skills without `metadata.dba` are also supported but do not appear in the website template selector. Arbitrary HTML/JavaScript templates, supporting assets, filesystem discovery and bundled script execution are outside this first version.

## Agent integration and boundaries

The main agent and specialists receive enabled skill names/descriptions and can call `list_skills` and `read_skill`. Skill bodies load on demand. Users can invoke `$name`, or let the agent choose a matching skill. `read_skill` returns the stable `skillId`, instructions and parsed template. Only the main agent has `build_static_site` and `get_static_site`; skills cannot expand specialist permissions or register executable tools. Skill imports/deletion/enabling are manual configuration operations.

`create_project` accepts `mode: "static" | "collect"`, defaulting to `collect` for older callers. `get_project` and `list_projects` expose the mode. Agent prompts include the mode boundary in both main and delegated conversations. Static projects reject discovery, review and pipeline mutation commands; collection projects reject website generation.

`POST /api/buildStaticSite` accepts `projectId`, `skillId`, `name`, `description`, `format` and `data`. Parsing, template validation and storage happen in one store transaction: a failed build preserves the previous website. `GET /api/site?projectId=...` renders the saved snapshot; add `&download=1` for an HTML attachment. Preview frames are sandboxed, imported cells are escaped by Go's `html/template`, and a fixed local script implements search. Templates cannot add scripts or network requests. Building through an Agent uses the same command, permission and validation layer as the UI; the model's context limits may make manual file import more suitable for large datasets.

Built-in source files are embedded from `internal/workbench/skills/*/SKILL.md`. They are installed once when a workspace has no skills field. An intentionally empty skills list is preserved across restarts. This is an additive schema-3 migration.
