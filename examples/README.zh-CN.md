# 设计稿示例

中文说明。English: [README.md](README.md).

这些文件来自交互原型 [design/DBA_design_package/](../design/DBA_design_package/README.md)（`index.html`，打包于 2026-11-05），用来在仓库里直接试用静态网站导入和收集模式的演示路径。`DEMO-*` 都是原型编号。摘要是原型里的「演示摘要」。项目卡片上的 1,248 / 386 / 214 和任务页上方的汇总数字是原型里的固定展示值；原型把三条复核卡片标为可操作示例，其余数字标为统计占位。

带真实 GEO 编号和校验值的复现案例在 [docs/manuscript/gpsadb-case/](../docs/manuscript/gpsadb-case/README.md)。

## 应用内预览

这些文件编译进应用，安装包里也有。打开侧栏「示例」，或「所有项目」页的「看看示例」。示例页只做预览，不改动工作空间：两个静态模板直接显示生成的网站；收集模式依次展示研究主题、候选、样本分组、处理产物和数据契约。每个文件都可下载。「创建示例项目」会新建一个载入示例数据的项目（网站已生成，或候选已导入为待复核）；之后再点同一个示例，会打开这个项目。

页面内容由 [catalog.json](catalog.json) 描述。新增示例时在其中列出文件，并在 `frontend/i18n.js` 补上展示文字的英文；`node scripts/check-i18n.js` 会检查。

## 文件

| 文件 | 在 DBA 中的用途 |
| --- | --- |
| [static/research-catalog.tsv](static/research-catalog.tsv) | **已有数据**。模板「研究数据目录」（`research-catalog`） |
| [static/literature-library.tsv](static/literature-library.tsv) | **已有数据**。模板「文献资源库」（`literature-library`） |
| [collect/candidates.json](collect/candidates.json) | **收集数据**。主题与检索 → **导入候选 JSON** |
| [collect/DEMO-0198-sample-groups.txt](collect/DEMO-0198-sample-groups.txt) | DEMO-0198 求助弹窗的样本分组（每行 `样本编号, 分组`） |
| [collect/dba_demo_manifest.json](collect/dba_demo_manifest.json) | 与原型「导出产物清单」下载的 `dba_demo_manifest.json` 相同 |
| [collect/artifacts/](collect/artifacts/) | 数据与产物页里 DEMO-0067 的预览片段 |
| [collect/data-contract.json](collect/data-contract.json) | 按原型数据契约字段名填写的一条元数据 |

## 静态网站

**所有项目 → 新建项目 → 已有数据 · 静态网站。** 在「静态网站」页选择模板，导入 TSV（扩展名 `.tsv` 会选中 TSV），填写标题和简介后生成。站点在本地预览和下载。[模板说明](../docs/skills.md)。

两个文件都可以用标题 `GPSA 虚拟扰动数据库`，简介 `整合人类基因扰动转录组，构建可检索的表达响应数据库。` 并写明这些行是设计稿示例。

### 研究数据目录

必需列是 `id` 和 `title`。原型卡片里有的字段按下面的列名放进表中：

| 列 | 原型字段 |
| --- | --- |
| `organism` | `species` |
| `assay` | `type`（CRISPR knockout、siRNA knockdown、CRISPR screen） |
| `cell`、`perturbed_gene` | `cell`、`gene` |
| `design_suggestion`、`confidence`、`reason` | 复核卡片上的建议、置信度和理由 |
| `summary` | `quote`（演示摘要） |
| `samples` | `n` |
| `demo` | `设计稿示例` |

卡片没有来源库和 URL，因此表里没有 `source` 和 `url`。

### 文献资源库

必需列是 `title` 和 `summary`。`summary` 是原型摘要。`id`、`organism`、`cell`、`perturbed_gene`、`perturbation` 是同一张卡片上的其他字段。卡片没有作者、年份、DOI 和 URL。

## 收集数据

**新建项目 → 收集数据 · 分析准备。** 原型里的任务和产物预览属于下表第一个项目。植物和作物项目只有主题文字，原型没有单独的候选列表。

| 项目 | 研究目标 | 研究对象 | 数据类型 | 纳入标准 |
| --- | --- | --- | --- | --- |
| GPSA 虚拟扰动数据库 | 整合人类基因扰动转录组，构建可检索的表达响应数据库。 | 人类 · Homo sapiens | 基因扰动 RNA-seq | 明确的基因扰动实验；具备匹配对照与生物学重复；可获取原始测序数据或表达矩阵。 |
| 植物单细胞图谱 | 收集植物单细胞与单核测序研究，建立统一的数据资源目录。 | 植物 · Viridiplantae | scRNA-seq / snRNA-seq | 实际分析植物单细胞或单核测序数据；原创研究或方法研究；允许公共数据再分析。 |
| 作物表型与多组学 | 关联作物表型与多组学数据，整理可用于预测建模的数据集。 | （空） | （空） | （空） |

原型保存的 GPSA 检索式（检索式 v3）：

```text
("gene perturbation" OR knockout OR knockdown OR CRISPRi OR RNAi)
AND ("RNA-seq" OR transcriptome OR "gene expression")
AND (human OR "Homo sapiens")
```

植物项目检索式：

```text
(plant OR Arabidopsis OR crop) AND ("single-cell" OR "single-nucleus" OR scRNA-seq OR snRNA-seq)
```

作物项目的检索式为空。在 DBA 里运行这些检索式会访问真实来源，返回的是该来源的记录。要看到原型里的三条卡片，导入 [candidates.json](collect/candidates.json)。

### 候选

把文件贴进 **导入候选 JSON**。每条有 `title`、`source`、`accession`、`organism`、`abstract`。`accession` 是原型编号（`DEMO-0142`、`DEMO-0198`、`DEMO-0236`），复核页把这种编号当作自定义来源编号。`abstract` 就是演示摘要，之后若引用这句话，可以与原文逐字对应。`source` 为 `设计稿示例`。

导入后记录是待复核，建议字段不会保留。同一文件再导入一次，新增为 0。按原型建议自行记录：

| 编号 | 按钮 | 可粘贴的理由 |
| --- | --- | --- |
| DEMO-0142 | 确认纳入 | 包含明确的单基因扰动与匹配对照，提供原始 RNA-seq 数据。 |
| DEMO-0198 | 稍后再看 | 检测到基因敲低实验，但对照样本的分组信息不完整。 |
| DEMO-0236 | 排除 | 筛选数据为细胞存活率读出，不符合转录组数据要求。 |

卡片置信度依次为高、中、高。DEMO-0142 还有细胞 A549、基因 TP53、类型 CRISPR knockout、`6 个样本`。DEMO-0198 还有多细胞系、MYC、siRNA knockdown、`12 个样本`。DEMO-0236 还有 K562、多基因、CRISPR screen、`未匹配 RNA-seq`。

### DEMO-0198 的样本分组

原型求助弹窗（**需要协助 · 确认样本分组**）要求每行一条 `样本编号, 分组`，占位文本是：

```text
sample_01, control
sample_02, control
sample_03, treatment
sample_04, treatment
```

[DEMO-0198-sample-groups.txt](collect/DEMO-0198-sample-groups.txt) 就是这段占位文本。可以贴进复核页的样本分组框，或贴进一条真正处于求助状态的任务的「补充样本分组」。至少两个样本、两个分组时，应用会接受这段文本。复核卡片上的 `12 个样本` 和求助标题「无法确定对照样本」都没有列出样本编号；这四行是该场景下弹窗给出的格式示例。

### 原型中的任务

任务表是四条代表性记录。表上方的汇总数字是展示值。

| 编号 | 当前阶段 | 状态 | 进度 | 服务器标签 |
| --- | --- | --- | --- | --- |
| DEMO-0084 | STAR 比对 | 运行中 | 68% | gzserver01 |
| DEMO-0112 | fastp 质控 | 运行中 | 42% | gzserver01 |
| DEMO-0067 | 差异表达 | 已完成 | 100% | cpu-node02 |
| DEMO-0198 | 样本分组 | 需要协助 | — | gzserver01 |

求助卡片标题是 `DEMO-0198 · 无法确定对照样本`。动态里写着 DEMO-0084 因超时做了第 1 / 3 次断点续传。原型服务器卡片是 `gzserver01`（SSH）和 `cpu-node02`（界面写为 Slurm）。DBA 的执行器是 SSH；这些主机名是原型标签。

GPSA 锁定流程的标签是 `gpsa-rnaseq@1.2` / v1.2，资源为 8 CPU · 32 GB，并发 4。编辑器里的步骤标签和参数文本：

| 步骤 | 工具标签 | 参数文本 |
| --- | --- | --- |
| 数据获取 | SRA Toolkit 3.1 | `--split-files --threads 8` |
| 质量控制 | fastp 0.24 | `--thread 8 --detect_adapter_for_pe` |
| 参考基因组比对 | STAR 2.7 | `--runThreadN 8 --genomeDir /ref/GRCh38`（GRCh38 · GENCODE v46） |
| 基因表达定量 | featureCounts 2.0 | `-T 8 -p -a /ref/gencode.v46.gtf` |
| 差异表达分析 | DESeq2 1.46 | `design = ~ condition; contrast = treatment vs control` |

这些字符串是原型步骤里的文本。在应用里锁定流程时，每一步仍要填写 shell 命令。

### 清单、产物和数据契约

[dba_demo_manifest.json](collect/dba_demo_manifest.json) 是打开 GPSA 项目时「导出产物清单」的内容：`demo`、`project`、流程 `gpsa-rnaseq@1.2`、数据集 `DEMO-0067` 和四个文件名。原型备注为 `DBA 设计稿示例，不包含真实分析数据`。

[artifacts/](collect/artifacts/) 是「产物预览 · 示例」里的文本。结果表上的大小（24.8 MB、3.2 MB、1.1 MB、8.2 KB）和「已校验」是原型展示值；这里的文件是那段短预览。`qc_report.html` 用原型文件名保存这段预览文本。`artifacts/run_manifest.json` 是预览对象（`dataset`、`pipeline`、`reference`、`status`、`demo`）。应用在任务成功后写入的清单见 [docs/pipelines.md](../docs/pipelines.md)。

[data-contract.json](collect/data-contract.json) 对应复核卡片 DEMO-0142。字段名来自原型对话框「用于后续建库的数据契约」和 [需求与页面说明](../design/DBA_design_package/docs/需求与页面说明.md)：

| 字段 | 对话框中的含义 |
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

`review_decision` 使用卡片上的「建议纳入」。`source_accession`、`sample_groups`、`pipeline_version` 为空，`output_files` 为空数组，因为这张卡片把这些值留空。产物预览使用编号 DEMO-0067。扰动字段用于 GPSA 示例；其他主题在此基础上扩展。
