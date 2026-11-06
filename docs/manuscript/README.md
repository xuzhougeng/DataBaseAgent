# Preprint manuscript and figures

`manuscript.tex` is the LaTeX source of the DataBaseAgent preprint, exported from the manuscript document on 5 November 2026, converted with pandoc, and revised by hand on 6 November 2026 against commit abf9075 (static projects, SKILL templates, MCP client); `manuscript.pdf` is its XeLaTeX build. Figures are included from the PDF files in this folder, so the document compiles from this folder alone:

```bash
docker run --rm -v "$PWD/docs/manuscript:/data" -w /data texlive/texlive:latest-small sh -c 'xelatex manuscript.tex && xelatex manuscript.tex'
```

## Figures

Figures for the DataBaseAgent preprint draft, exported on 5 November 2026 from the schematic diagrams in the manuscript document. Each figure exists in three forms: `.svg` (vector, editable), `.pdf` (vector, for LaTeX or direct upload) and `.png` (2375 px wide = 7.9 inches at 300 dpi). Colours are inlined; the typeface is Helvetica Neue with Arial fallback.

| File stem | Manuscript | Title inside the figure |
| --- | --- | --- |
| figure1-argument | Figure 1, Introduction | The loop one institute ran once becomes a tool any laboratory can run and keep |
| figure2-workflow | Figure 2, Results | Two workflows build a repository; the tinted stages wait for a person |
| figure3-architecture | Figure 3, Results | One binary orchestrates; only the job on the server runs continuously |
| figure4-job-states | Figure 4, Results | A job retries only network failures; everything else waits for a person |

Legends are in the manuscript under each figure. Figures 1 to 3 were redrawn by hand in the SVG files on 6 November 2026 for the two project kinds (static and collecting). To regenerate after editing an SVG:

```bash
rsvg-convert -f pdf -o figure2-workflow.pdf figure2-workflow.svg && rsvg-convert -f png -w 2375 -o figure2-workflow.png figure2-workflow.svg
```

Figure 5 is the acceptance screenshot `figure5-overview-screenshot.jpg` (browser preview, Chinese locale; the record is outputs/QA.md). It is only 1265 px wide, which is below 300 dpi at a 7-inch column; recapture the same page at a higher device pixel ratio before submission.

## Case study

The [SK-N-SH perturbation RNA-seq case](gpsadb-case/README.md) provides the GEO candidate pair, the confirmed sample groups and the two pipeline commands needed to reproduce the count archive.
