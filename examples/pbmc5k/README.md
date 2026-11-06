# PBMC5k：真实数据的 Agent 建站案例

这组文件来自 10x Genomics 公开的 **5k_pbmc_v3** 分析结果，不是 DBA 设计稿中的演示记录。数据按 CC BY 4.0 使用，保留来源署名。详细过程和实际截图见 [walkthrough](../../docs/walkthroughs/pbmc5k/README.md)。

- [cells.tsv](cells.tsv)：5,025 个细胞的 barcode、原始 t-SNE 坐标和 graph cluster。
- [provenance.json](provenance.json)：官方来源、SHA-256、cluster 数量及处理说明。
- [prepare.py](prepare.py)：按 barcode 对齐官方坐标和聚类结果，不执行分析或抽样。
- [agent-site.html](agent-site.html)：DBA 内模型实际生成的页面源码。它依赖 DBA 注入的 `window.DBA_DATA`；可独立打开的完整成品是 [index.html](../../docs/walkthroughs/pbmc5k/index.html)。
- [single-cell-explorer/SKILL.md](../../internal/workbench/skills/single-cell-explorer/SKILL.md)：领域建站指引，无固定渲染布局。

复现数据准备（Python 标准库，无第三方依赖）：

```sh
curl -fL -o /tmp/5k_pbmc_v3_analysis.tar.gz https://cf.10xgenomics.com/samples/cell-exp/3.0.2/5k_pbmc_v3/5k_pbmc_v3_analysis.tar.gz
python3 examples/pbmc5k/prepare.py /tmp/5k_pbmc_v3_analysis.tar.gz
```

然后在 DBA 新建“已有数据”项目，导入 `cells.tsv`，点击“保存数据供 Agent 建站”，请助手使用 `$single-cell-explorer` 自主设计网站。新工作空间内置该 skill；已有工作空间可手动导入。这里没有重新分析、细胞类型注释或基因表达矩阵。

[10x Genomics 官方数据页面](https://www.10xgenomics.com/datasets/5-k-peripheral-blood-mononuclear-cells-pbm-cs-from-a-healthy-donor-v-3-chemistry-3-1-standard-3-0-2) · [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)
