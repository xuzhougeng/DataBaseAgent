# Preprint figures

Figures for the DataBaseAgent preprint draft, exported on 5 November 2026 from the schematic diagrams in the manuscript document. Each figure exists in three forms: `.svg` (vector, editable), `.pdf` (vector, for LaTeX or direct upload) and `.png` (2375 px wide = 7.9 inches at 300 dpi). Colours are inlined; the typeface is Helvetica Neue with Arial fallback.

| File stem | Manuscript | Title inside the figure |
| --- | --- | --- |
| figure1-argument | Figure 1, Introduction | The loop one institute ran once becomes a tool any laboratory can run and keep |
| figure2-workflow | Figure 2, Results | Six stages build the repository; three end in a human decision |
| figure3-architecture | Figure 3, Results | One binary orchestrates; only the job on the server runs continuously |
| figure4-job-states | Figure 4, Results | A job retries only network failures; everything else waits for a person |

Legends are in the manuscript under each figure. To regenerate after editing a diagram in the document, re-export the widget code and render it with `bun` (JSX factory `__jsx`) and `rsvg-convert`; the render script lives outside the repository.

Figure 5 is the acceptance screenshot `figure5-overview-screenshot.jpg` (browser preview, Chinese locale; the record is outputs/QA.md). It is only 1265 px wide, which is below 300 dpi at a 7-inch column; recapture the same page at a higher device pixel ratio before submission.
