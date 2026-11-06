# Preprint manuscript and figures

`manuscript.tex` is the LaTeX source of the DataBaseAgent preprint, originally exported from the manuscript document and converted with pandoc. The current revision (6 November 2026, America/Los_Angeles) incorporates Agent-authored websites guided by data and domain SKILLs, the recorded PBMC5k case, and large-scale dynamic websites as a current limitation and future direction. Earlier collection and MCP evidence retains its own dates in the manuscript. `manuscript.pdf` is the compiled reading copy. Figures are included from the PDF files in this folder, so the document compiles from this folder alone:

```bash
docker run --rm -v "$PWD/docs/manuscript:/data" -w /data texlive/texlive:latest-small sh -c 'xelatex manuscript.tex && xelatex manuscript.tex'
```

The current 35-page PDF was rebuilt with Tectonic 0.17.0. The author block and updated figures were visually checked. An alternative to the XeLaTeX command above is:

```bash
tectonic --untrusted docs/manuscript/manuscript.tex
```

## Figures

Figures for the DataBaseAgent preprint draft, exported on 5 November 2026 from the schematic diagrams in the manuscript document. Figures 1–4 exist in three forms: `.svg` (vector, editable), `.pdf` (vector, for LaTeX or direct upload) and `.png` (2375 px wide = 7.9 inches at 300 dpi). Colours are inlined; the typeface is Helvetica Neue with Arial fallback.

| File stem | Manuscript | Title inside the figure |
| --- | --- | --- |
| figure1-argument | Figure 1, Introduction | The loop one institute ran once becomes a tool any laboratory can run and keep |
| figure2-workflow | Figure 2, Results | Two workflows build a repository; the tinted stages wait for a person |
| figure3-architecture | Figure 3, Results | One binary orchestrates; only the job on the server runs continuously |
| figure4-job-states | Figure 4, Results | A job retries only network failures; everything else waits for a person |
| figure5-gpsadb-update | Figure 5, Results | GPSAdb: a database maintained through repeated data updates |

Legends are in the manuscript under each figure. Figures 1 to 3 now distinguish domain-guided Agent authoring from optional fixed templates; Figure 1 also names large-scale dynamic websites as future work. To regenerate after editing an SVG:

```bash
rsvg-convert -f pdf -o figure2-workflow.pdf figure2-workflow.svg && rsvg-convert -f png -w 2375 -o figure2-workflow.png figure2-workflow.svg
```

Figure 5 (`figure5-gpsadb-update.svg`, `.pdf`, `.png`) combines a vector update-cycle diagram with the unchanged GPSAdb browser capture from `../../assets/DBA-overview.jpg`. The cycle emphasizes repeated discovery, eligibility review, SSH server processing and verified archival, followed by the next update. The screenshot shows three known SK-N-SH contrasts, three completed jobs and nine archived files on Local SSH. Repeated searches require instruction; this is not evidence of an unattended full-database update. The SVG embeds the original screenshot for portability; the PDF retains vector labels with a raster screenshot. Its screenshot detail is limited by the original 1280-pixel capture; the 2375-pixel PNG does not add source resolution.

Only the manuscript Figure 5 is distributed here. The earlier acceptance screenshot was removed during publication cleanup. `submission/manuscript-source.zip` contains this exact Figure 5 PDF and the matching LaTeX source.

## Case study

The [SK-N-SH perturbation RNA-seq case](gpsadb-case/README.md) provides the GEO candidate pair, the confirmed sample groups and the two pipeline commands needed to reproduce the count archive.

The [PBMC5k website walkthrough](../walkthroughs/pbmc5k/README.md) records a live DBA Agent generating an interactive t-SNE site from 5,025 cells and eight graph clusters. It includes inputs, provenance, generated HTML, screenshots, tool traces and verification limits. This tests the current static authoring path; it does not establish dynamic-site scalability or public deployment.

## Preprint submission

The `submission/` folder contains abstract text, the LaTeX source package and case-study materials. Manuscript `BIORXIV/2026/764849` was submitted with both supplements and verified in the bioRxiv screening queue on 6 November 2026. This identifier is not a DOI; the preprint is not yet online. Local submission receipts and administrative metadata are excluded from the Git release. Author order, Qin's current affiliation and the contributions follow the authors' instructions; Xu's affiliation and correspondence were checked against [Genomics Communications e018](https://www.maxapress.com/article/doi/10.48130/gcomm-0026-0018). Application source code is now public at [GitHub](https://github.com/xuzhougeng/DataBaseAgent). The submitted PDF retains its original code-availability statement. The former generative-AI declaration section was removed at the authors' request.

## Citation and validation records

See [CITATION.cff](../../CITATION.cff) and the [repository citation guide](../../README.md#citation). Until a public bioRxiv DOI is verified, the citation identifies the submitted manuscript hosted here.

The [early validation record](validation-record.md) was moved from `outputs/QA.md` during repository cleanup. The submitted manuscript and its source archive retain that historical path; their bytes, including Figure 5, have not changed.
