---
name: literature-library
description: 将已有文献、教程或学术资源列表构建为静态卡片式资源库，适合按标题搜索和阅读摘要。
metadata:
  dba:
    label: 文献资源库
    layout: cards
    accent: '#466b9c'
    required-columns: [title, summary]
    title-column: title
---

# 文献资源库

用于 DBA 已有数据（static）项目；使用用户已有的文献或资源记录。

每条记录必须有 `title` 和 `summary` 列，可保留 `authors`、`year`、`doi`、`url`、`tags`。按用户提供的内容展示，不根据标题编造摘要、作者、DOI 或年份。字段需重命名时解释对应关系；如果没有摘要，先确认是否使用已有说明或留空。

通过 `read_skill` 获取本技能的 `skillId`。将真实 CSV、TSV 或 JSON 交给 `build_static_site`，连同网站标题、简介和 `skillId`。模板以标题为卡片标题，其余字段按原始数据展示，支持本地关键词搜索。

完成后报告真实记录数，指向静态网站页面的预览与 HTML 下载。当前不配置数据库、不部署动态应用、不自动联网补齐记录。
