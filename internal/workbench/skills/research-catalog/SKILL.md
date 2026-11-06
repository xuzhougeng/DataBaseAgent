---
name: research-catalog
description: 将已有的研究数据集整理为可搜索的静态数据目录，适合按编号浏览数据集、物种和测序类型。
metadata:
  dba:
    label: 研究数据目录
    layout: table
    accent: '#237c63'
    required-columns: [id, title]
    title-column: title
---

# 研究数据目录

用于 DBA 已有数据（static）项目。先读取项目与用户提供的真实数据。

每行表示一个数据集，必须有 `id` 和 `title` 列。建议保留 `organism`、`assay`、`source`、`url` 等原始字段；没有的信息保持空白。用户字段名不一致时，解释映射并保留原始含义，不编造来源或编号。

使用 `list_skills` 获取目录，`read_skill` 阅读本模板。确认标题、简介和字段后，调用 `build_static_site`，提供本技能的 `skillId` 与完整 CSV、TSV 或 JSON 数据。生成结果是本地可搜索表格，用户可在静态网站页面预览并下载独立 HTML。

数据不足时询问缺少的输入，不启动检索或远程处理。完成后说明实际记录数和预览入口；没有执行发布操作时，不称网站已上线。
