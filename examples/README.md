# Design-demo examples

English guide. 中文说明：[README.zh-CN.md](README.zh-CN.md).

These files are the sample records from the interactive prototype in [design/DBA_design_package/](../design/DBA_design_package/README.md) (`index.html`, packaged 2026-11-05). They are here so a static-site import and the collect-mode demo path can be tried from the repository. Every `DEMO-*` string is a prototype label. Quotes are the prototype’s `演示摘要` text. Counts on the project cards (1,248 / 386 / 214) and the job summary numbers are fixed prototype figures; the prototype itself calls the three review cards operable examples and the other counts placeholders.

The GEO reproduction, with real accessions and checksums, stays in [docs/manuscript/gpsadb-case/](../docs/manuscript/gpsadb-case/README.md).

## Files

| File | Use in DBA |
| --- | --- |
| [static/research-catalog.tsv](static/research-catalog.tsv) | **Existing data**. Template **Research data catalogue** (`research-catalog`) |
| [static/literature-library.tsv](static/literature-library.tsv) | **Existing data**. Template **Literature library** (`literature-library`) |
| [collect/candidates.json](collect/candidates.json) | **Collect data**. **Topic & search → Import candidates JSON** |
| [collect/DEMO-0198-sample-groups.txt](collect/DEMO-0198-sample-groups.txt) | Sample-group box for the DEMO-0198 help dialog (`sample, group` per line) |
| [collect/dba_demo_manifest.json](collect/dba_demo_manifest.json) | Same JSON the prototype downloads as `dba_demo_manifest.json` |
| [collect/artifacts/](collect/artifacts/) | Short previews from **Data & outputs** for DEMO-0067 |
| [collect/data-contract.json](collect/data-contract.json) | One metadata object using the prototype’s contract field names |

## Static websites

**All projects → New project → Existing data · Static website.** On **Static website**, choose the template, import the TSV (the `.tsv` extension selects TSV), set a title and description, then build. The site previews and downloads locally. [Template rules](../docs/skills.md).

Suggested title for either file: `GPSA 虚拟扰动数据库`. Suggested description: `整合人类基因扰动转录组，构建可检索的表达响应数据库。` Add that the rows are design-demo data.

### Research catalogue

Required columns are `id` and `title`. Optional columns use prototype fields where the cards have them:

| Column | Prototype field |
| --- | --- |
| `organism` | `species` |
| `assay` | `type` (CRISPR knockout, siRNA knockdown, CRISPR screen) |
| `cell`, `perturbed_gene` | `cell`, `gene` |
| `design_suggestion`, `confidence`, `reason` | review-card suggestion, confidence, and reason |
| `summary` | `quote` (`演示摘要`) |
| `samples` | `n` |
| `demo` | `设计稿示例` |

The cards do not carry a repository source or a URL, so this table has no `source` or `url` column.

### Literature library

Required columns are `title` and `summary`. `summary` is the prototype quote. `id`, `organism`, `cell`, `perturbed_gene`, and `perturbation` are the other fields printed on the same cards. The cards do not carry authors, year, DOI, or URL.

## Collect data

**New project → Collect data · Analysis.** The prototype’s jobs and result previews belong to the first project below. Plant and crop are theme text only; the prototype has no separate candidate list for them.

| Project | Goal (项目描述) | Organism | Assay | Inclusion criteria |
| --- | --- | --- | --- | --- |
| GPSA 虚拟扰动数据库 | 整合人类基因扰动转录组，构建可检索的表达响应数据库。 | 人类 · Homo sapiens | 基因扰动 RNA-seq | 明确的基因扰动实验；具备匹配对照与生物学重复；可获取原始测序数据或表达矩阵。 |
| 植物单细胞图谱 | 收集植物单细胞与单核测序研究，建立统一的数据资源目录。 | 植物 · Viridiplantae | scRNA-seq / snRNA-seq | 实际分析植物单细胞或单核测序数据；原创研究或方法研究；允许公共数据再分析。 |
| 作物表型与多组学 | 关联作物表型与多组学数据，整理可用于预测建模的数据集。 | (empty) | (empty) | (empty) |

GPSA query stored on the prototype (检索式 v3):

```text
("gene perturbation" OR knockout OR knockdown OR CRISPRi OR RNAi)
AND ("RNA-seq" OR transcriptome OR "gene expression")
AND (human OR "Homo sapiens")
```

Plant query:

```text
(plant OR Arabidopsis OR crop) AND ("single-cell" OR "single-nucleus" OR scRNA-seq OR snRNA-seq)
```

The crop project’s query box is empty. Running either query in DBA calls the live source and returns that source’s records. Load [candidates.json](collect/candidates.json) when the three prototype cards are what you want.

### Candidates

Paste the file into **Import candidates JSON**. Each object has `title`, `source`, `accession`, `organism`, and `abstract`. `accession` is the prototype id (`DEMO-0142`, `DEMO-0198`, `DEMO-0236`), which the review form accepts as a custom accession. `abstract` is the quote, so a later evidence check can match that sentence verbatim. `source` is `设计稿示例`.

Import stores the rows as pending and drops any suggestion fields. A second import of the same file adds zero rows. Record the prototype’s suggestions yourself:

| Accession | Button | Reason to paste |
| --- | --- | --- |
| DEMO-0142 | Include | 包含明确的单基因扰动与匹配对照，提供原始 RNA-seq 数据。 |
| DEMO-0198 | Later | 检测到基因敲低实验，但对照样本的分组信息不完整。 |
| DEMO-0236 | Exclude | 筛选数据为细胞存活率读出，不符合转录组数据要求。 |

Card confidence is 高, 中, and 高. DEMO-0142 also shows cell A549, gene TP53, type CRISPR knockout, and `6 个样本`. DEMO-0198 shows 多细胞系, MYC, siRNA knockdown, and `12 个样本`. DEMO-0236 shows K562, 多基因, CRISPR screen, and `未匹配 RNA-seq`.

### DEMO-0198 sample groups

The prototype’s help dialog (**需要协助 · 确认样本分组**) asks for one `sample, group` line per row and shows this placeholder:

```text
sample_01, control
sample_02, control
sample_03, treatment
sample_04, treatment
```

[DEMO-0198-sample-groups.txt](collect/DEMO-0198-sample-groups.txt) is that placeholder. Paste it into the review sample-group box, or into **Extra sample groups** on a job that is actually waiting for help. The app accepts the text when there are at least two samples and two groups. The review card’s `12 个样本`, and the help copy “无法确定对照样本”, do not list sample ids; these four lines are the dialog’s format example for that scenario.

### Jobs the prototype lists

The job table is four representative rows. Summary counters above it are display figures.

| Id | Step | Status | Progress | Server label |
| --- | --- | --- | --- | --- |
| DEMO-0084 | STAR 比对 | 运行中 | 68% | gzserver01 |
| DEMO-0112 | fastp 质控 | 运行中 | 42% | gzserver01 |
| DEMO-0067 | 差异表达 | 已完成 | 100% | cpu-node02 |
| DEMO-0198 | 样本分组 | 需要协助 | — | gzserver01 |

The help card title is `DEMO-0198 · 无法确定对照样本`. The activity line says the download for DEMO-0084 was resumed once (1 / 3) after a timeout. Server cards in the prototype are `gzserver01` (SSH) and `cpu-node02` (shown as Slurm). DBA’s executor is SSH; those hostnames are prototype labels.

The locked GPSA flow is labeled `gpsa-rnaseq@1.2` / v1.2, with 8 CPU · 32 GB and concurrency 4. Step labels and the parameter text in the editor:

| Step | Tool label | Parameter text |
| --- | --- | --- |
| 数据获取 | SRA Toolkit 3.1 | `--split-files --threads 8` |
| 质量控制 | fastp 0.24 | `--thread 8 --detect_adapter_for_pe` |
| 参考基因组比对 | STAR 2.7 | `--runThreadN 8 --genomeDir /ref/GRCh38` (GRCh38 · GENCODE v46) |
| 基因表达定量 | featureCounts 2.0 | `-T 8 -p -a /ref/gencode.v46.gtf` |
| 差异表达分析 | DESeq2 1.46 | `design = ~ condition; contrast = treatment vs control` |

Those strings are the prototype’s step text. Locking a pipeline in the app still means entering a shell command for each step.

### Manifest, artifacts, and the data contract

[dba_demo_manifest.json](collect/dba_demo_manifest.json) is the payload from **导出产物清单** while the GPSA project is open: `demo`, `project`, `workflow` `gpsa-rnaseq@1.2`, dataset `DEMO-0067`, and the four file names. The prototype note is `DBA 设计稿示例，不包含真实分析数据`.

[artifacts/](collect/artifacts/) is the text from **产物预览 · 示例**. The result table’s display sizes (24.8 MB, 3.2 MB, 1.1 MB, 8.2 KB) and the “已校验” badge are prototype figures; the files here are the short previews. `qc_report.html` is that preview text under the prototype filename. `artifacts/run_manifest.json` is the preview object (`dataset`, `pipeline`, `reference`, `status`, `demo`). The manifest the app writes after a succeeded job is the schema in [docs/pipelines.md](../docs/pipelines.md).

[data-contract.json](collect/data-contract.json) is one object for review card DEMO-0142. Field names are the ones in the prototype dialog **用于后续建库的数据契约** and in [需求与页面说明](../design/DBA_design_package/docs/需求与页面说明.md):

| Field | Meaning on the dialog |
| --- | --- |
| `dataset_id` | 数据集唯一标识 |
| `source_accession` | 来源数据编号 |
| `organism` | 研究对象 |
| `perturbed_gene` | 扰动基因 |
| `perturbation` | 扰动方式 |
| `sample_groups` | 样本与分组 |
| `pipeline_version` | 流程版本 |
| `output_files` | 产物路径与校验值 |
| `review_decision` | 人工纳入结论 |
| `run_provenance` | 执行与重试历史 |

`review_decision` is the card text `建议纳入`. `source_accession`, `sample_groups`, and `pipeline_version` are empty, and `output_files` is an empty list, because this card leaves those values blank. The artifact preview uses DEMO-0067. Perturbation fields are the GPSA example; other topics extend the contract.
