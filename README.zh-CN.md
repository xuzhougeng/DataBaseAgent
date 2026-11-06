> English: [README.md](README.md) · 专题文档：[docs/](docs/)（架构、Agent、模型协议、检索、流程与远程执行、构建与安装、使用指南与边界）

# DataBaseAgent（DBA）

基于 **Wails v3.1.0 + Go + 原生 HTML/CSS/JavaScript** 的本地主题数据库构建工作台。既支持数据收集、分析与归档，也让 Agent 根据已有数据和领域 SKILL 自主设计网站。整体设计包括静态和动态网站，当前已实现并验证的是静态交互建站路径。十个页面：所有项目、项目概览、主题与检索、数据复核、处理流程、运行任务、服务器资源、数据与产物、AI 助手、偏好设置。

<p align="center"><img src="assets/DBA-overview.jpg" width="960" alt="DBA 英文项目概览，展示 GPSAdb 2.0 的 SK-N-SH 扰动数据归档"></p>

## 运行与构建

从源码构建需要 Go 1.26+；前端没有 npm 依赖，静态资源直接嵌入可执行文件。远程执行在任何平台都需要系统 `ssh` 客户端（Windows 为内置 OpenSSH）。

| 平台 | 运行时依赖 | 本机构建额外依赖 | 产物 |
| --- | --- | --- | --- |
| Windows 10/11 | WebView2 Runtime（Windows 11 自带） | 无 | `bin/DBA.exe` |
| macOS 12+（Go 1.26）或 macOS 13+（Go 1.27） | 系统 WebKit | Xcode 命令行工具：`xcode-select --install` | `bin/DBA.app` |
| Linux | GTK 4 与 WebKitGTK 6.0 | 对应开发包，Debian/Ubuntu 为 `libgtk-4-dev libwebkitgtk-6.0-dev`，Fedora 为 `gtk4-devel webkitgtk6.0-devel` | `bin/DBA` |

macOS / Linux：

```bash
scripts/build.sh
```

```bash
scripts/build.sh all
```

Windows（PowerShell）：

```powershell
./scripts/build.ps1
```

```powershell
./scripts/build.ps1 -Target all
```

两个脚本都接受 `windows`、`darwin`、`linux`、`all` 目标；`all` 一次产出三个平台的成品。`--debug` / `-Debug` 不剥离符号并启用 Wails 开发特性。`GOARCH=arm64` 可切换 Windows / Linux 架构，`TAGS=gtk3` 让 Linux 成品改用 GTK 3 + WebKit2GTK 4.1（旧发行版没有 WebKitGTK 6.0 时使用）。

安装包：

```bash
scripts/build.sh setup
```

```bash
scripts/build.sh dmg
```

`setup` 生成 `bin/DBA-Setup.exe`（PowerShell 为 `./scripts/build.ps1 -Target setup`），需要 NSIS 3 的 `makensis` 在 PATH 中：macOS `brew install makensis`，Debian/Ubuntu `apt install nsis`，Windows 从 [nsis.sourceforge.io](https://nsis.sourceforge.io) 安装。安装包按用户安装到 `%LOCALAPPDATA%\Programs\DBA`，不需要管理员权限，写入开始菜单与「应用和功能」卸载项；缺少 WebView2 Runtime 时提示下载；卸载不删除 `%APPDATA%\DataBaseAgent` 工作空间。安装脚本在 `scripts/installer.nsi`。

`dmg` 生成 `bin/DBA.dmg`，内含 `DBA.app` 和 `Applications` 快捷方式，只能在 macOS 上用系统自带的 `hdiutil` 生成。两种安装包都未签名：macOS 需要右键「打开」或 `xattr -dr com.apple.quarantine`，Windows SmartScreen 可能提示「更多信息 → 仍要运行」。

交叉编译规则：Windows 目标是纯 Go，在任何平台都能构建。为非本机平台构建 macOS 或 Linux 时，脚本切换到 Wails 的 purego 后端（`CGO_ENABLED=0 -tags purego`），不需要 C 工具链，但 Wails 将其标为实验特性；正式发布请在目标平台本机构建。`PUREGO=1 scripts/build.sh` 可在本机也省掉 C 工具链。`build.ps1` 在未设置 `HTTPS_PROXY` 时默认走 `http://127.0.0.1:7897` 代理下载模块，`build.sh` 只沿用当前环境变量。

默认工作空间：Windows `%APPDATA%\DataBaseAgent`，macOS `~/Library/Application Support/DataBaseAgent`，Linux `~/.config/DataBaseAgent`，保存 `workbench.json` 与 `projects/<项目ID>/results/<任务ID>/`。用 `-workspace <目录>` 指定其他目录，macOS 为 `open bin/DBA.app --args -workspace <目录>`。同一工作空间的桌面实例复用现有窗口。

浏览器开发预览使用同一个后端和同一套静态资源，任何平台都不需要 C 工具链：

```bash
go run ./cmd/dba-server -port 3415 -workspace .tmp/dev-workspace
```

服务仅监听 `127.0.0.1`。请勿让开发服务和桌面端同时使用同一工作空间。

## 使用路径：以 GPSAdb 为例

GPSAdb 2.0 收录人类细胞系的单基因扰动 RNA-seq 数据。`2026.10.22` 发布版包含 7,665 个数据集、42,235 个样本，覆盖 2,810 个被扰动的基因。 [GPSAdb 2.0 论文](https://doi.org/10.1093/nar/gkaf1077) · [数据发布记录](https://doi.org/10.5281/zenodo.23585244)

首页英文截图展示了该发布版中 SK-N-SH 细胞的三组扰动对比。DBA 用锁定的流程核对样本分组与计数列，再归档每组对比的计数矩阵、分组表和样本汇总。 [PLLP](https://gpsadb.gzdlab.com/dataset/D25938) · [SUV39H1](https://gpsadb.gzdlab.com/dataset/D26999) · [TSPYL5](https://gpsadb.gzdlab.com/dataset/D27545)

手稿案例另以 FAM167A 敲低为例，展示如何从 GEO 筛选候选、处理分组求助并校验归档。 [复现步骤](docs/manuscript/gpsadb-case/README.md) · [DBA 工作流指南](docs/workflow.md)

## 数据驱动的自主建站与领域 SKILL

核心设计是：**用户目标 + 数据 + 领域 SKILL → Agent 自主设计网站**。Agent 先检查字段、样例、缺失值和统计，再决定页面结构、图表与交互。领域 SKILL 提供数据含义、适合的展示方式和不能越过的解释边界；Agent 编写具体的 HTML/CSS/JavaScript，DBA 负责数据管理、工具执行、预览、迭代和导出。扩展到新的领域，可以增加相应 SKILL，而不必为每类数据预先写死一个专用页面。

新建项目时，**已有数据**适合用户已分析好的结果：导入 CSV、TSV 或 JSON，点击“保存数据供 Agent 建站”，再让助手设计或修改网站。完整数据由 DBA 注入页面，无需 Agent 在回复中重新复制每条记录。内置“研究数据目录”（表格）和“文献资源库”（卡片）仍可用于简单的手工建站。**收集数据**支持检索、复核、处理分析与归档；目前从归档结果进入建站项目，仍需手工导出并导入表格。

`single-cell-explorer` 是领域 SKILL 的一个例子：帮助 Agent 区分降维坐标、cluster、细胞类型注释和表达量，根据真实存在的字段选择展示功能。在 [PBMC5k 实测 walkthrough](docs/walkthroughs/pbmc5k/README.md) 中，DBA Agent 实际根据 5,025 个细胞、8 个 graph cluster 生成了 t-SNE 交互网站，包含筛选、barcode 搜索、细胞详情、缩放/重置和分页。该案例验证的是一份已分析结果的建站流程，不代表已实现原始测序分析或证明所有生成网站的质量；输入、生成代码、截图和验证限制均已记录。

SKILL 可按任务匹配或通过 `$skill-name` 按需读取。生成结果保存数据与代码快照，修改技能不会改变已生成的网站。[SKILL 与自主建站说明](docs/skills.md) · [示例](examples/README.zh-CN.md)。

## Agent 与模型

默认打开“所有项目”，可通过该页的 AI 助手按钮或右侧“工作进展”上方的入口打开独立助手页面。描述目标后，项目助手可以通过工具创建和修改项目、执行检索、导入与复核候选、保存分组、配置并锁定流程、创建与恢复任务、查看产物并导出清单。其他页面展示同一份数据，并保留手工操作。右侧在助手入口下方展示 AI 工作进展、巡检状态、求助与操作动态。

每条对话由主 Agent 统筹，输入 @ 可调度子 Agent，子任务和结果保留在同一条对话中。对话支持独立历史、搜索、重命名、置顶、归档和导出。执行期间可以继续发送消息，同一项目按顺序处理，排队消息可编辑或移除；工具调用以可展开的步骤展示状态、耗时、参数和结果。停止或失败后会暂停后续队列，重启时保留未处理消息并标记中断。草稿与当前对话在刷新后恢复，已完成的工具操作不会自动重做。

预设五个角色，工具范围固定、指令和模型可改：项目助手拥有完整工具集，检索策略师、复核员、流程顾问和运维巡检员分别负责相应环节。工具复用手工操作的业务校验，连续调用读取最新状态；复核理由、操作结果和调用痕迹均会保存，执行中断也保留已完成步骤。无项目时可在“工作空间对话”中让 AI 建项目，关键事实缺失时在对话中补充。模型配置工具使用已有环境变量或保留系统凭据，密钥明文仍在偏好设置中填写。

在偏好设置页添加模型时，选择协议并直接填写 API 基础地址、模型 ID 和 API Key。无需认证的服务可将密钥留空。API Key 保存在系统凭据库，已有模型仍可读取原先配置的环境变量。支持 OpenAI Chat Completions 兼容协议（涵盖 DeepSeek、Qwen、Ollama、vLLM 等）、Anthropic Messages 和 Google Gemini。每个 Agent 可选择不同模型，未选择时使用列表中的第一个。[模型配置指南](docs/models.md)

偏好设置中的 **MCP 插件** 默认带有 GitHub 和 Cloudflare。填写 GitHub PAT / Cloudflare API Token 后保存并测试连接，主 Agent 即可按需发现和调用工具，调用过程保留在对话记录中。也可添加 Streamable HTTP 或本地 STDIO 服务，设置超时、工具允许列表与禁用列表。Token 保存在系统凭据库，也可从启动环境变量读取；当前不含浏览器 OAuth 登录。[MCP 配置指南](docs/mcp.md)

检索、模型和 MCP HTTP 请求遵循启动进程的 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量。GitHub 操作及构建脚本使用 `http://127.0.0.1:7897` 代理。SSH 采用本机密钥或 SSH Agent，强制校验已知主机，不支持交互式密码输入。

## 当前边界与优化方向

“静态”描述的是运行方式：代码和数据一起交付，浏览器完成搜索、筛选、缩放等交互，无需数据库或应用后端。当前导入器支持单表，最多 2 MiB、10,000 行、100 列；这些是实现限制，不是经过性能基准验证的规模保证。

**面向大规模数据集的动态网站属于 DBA 的整体设计目标，也是当前 limitation。** 大型表达矩阵、多表关联，以及超过浏览器内存或绘制能力的数据，需要数据库存储、索引查询、后端 API、按需或分块加载及服务端计算；多人权限和持续更新也可能要求动态架构。当前尚未实现由 Agent 完整构建、部署并持续运维这套动态系统，也未完成大规模动态网站的性能评估。

后续将扩展 Agent 工具与领域 SKILL，让 Agent 根据数据规模和访问需求选择架构，构建并验证存储、API 与前端，支持部署、监控和更新，并在逐步扩大的数据集上评估查询延迟、内存、并发、正确性和运维成本。静态建站是这一设计中首先得到实测的一条路径。

这是可运行的第一版，使用本地 JSON 原子保存，适合先贯通单服务器闭环。执行器目前支持 Linux SSH，不含 Slurm、容器管理、内存/磁盘硬配额或系统服务。CPU 配额控制提交容量，实际线程数须在工具参数中设置。关闭 DBA 后远端进程继续运行；重新打开后恢复巡检。巡检循环按固定恢复策略工作；Agent 可通过工具修改并锁定新流程、创建任务，已有任务保持原快照。旧版（schema 1、2）工作空间打开时自动迁移模型配置与对话历史。

新版本不会自动套用到旧任务；终止或删除远端任务、编辑服务器配置、按时间段穷尽海量检索结果等功能尚未提供。失败后的求助允许补充分组或在远端修复环境；需要修改工具或参数时，应锁定新流程并提交新任务。SSH 远端需要 `sh`、`nohup`、`realpath`、`sha256sum` 和所选工具。原始数据下载由已确认的流程命令完成，不会从任意论文自动推断完整 FASTQ 下载方案。

## 验证

```bash
go test ./... && go test -race ./internal/workbench && go vet ./...
node --check frontend/workbench.js && node --check frontend/conversations.js && node --check frontend/mcp.js
node scripts/check-i18n.js && node --test scripts/test-conversations.js
```

测试覆盖项目隔离、已纳入记录门槛、流程与输入快照、跨项目服务器配额、连接失败不重复提交、有限恢复和人工接手、校验与本地归档、重启持久化、损坏文件保护、检索去重与获取上限、模型证据校验、HTTP 来源检查、Agent 工具范围与调用痕迹、三种模型协议的请求与回填格式，旧工作空间迁移、对话创建项目到产物归档的完整链路，以及执行中断后的操作留存。自动化测试通过可控执行器验证调度闭环；真实服务器和模型端点仍须使用自己的配置完成联调。

## 引用

如果你在研究中使用 DBA，请引用配套[论文稿件](docs/manuscript/manuscript.pdf)：

> Qin, G., & Xu, Z.-G. (2026). DataBaseAgent: a local desktop workbench for building and publishing topic-specific biological data repositories with human-gated AI agents. Manuscript submitted to bioRxiv.

论文已提交 bioRxiv，公开 DOI 尚未核实，目前引用指向仓库中的稿件。投稿编号不是 DOI。机器可读引用信息见 [CITATION.cff](CITATION.cff)。

```bibtex
@unpublished{qin2026databaseagent,
  author = {Qin, Guihui and Xu, Zhou-Geng},
  title = {{DataBaseAgent}: a local desktop workbench for building and publishing topic-specific biological data repositories with human-gated AI agents},
  year = {2026},
  note = {Manuscript submitted to bioRxiv},
  url = {https://github.com/xuzhougeng/DataBaseAgent/blob/main/docs/manuscript/manuscript.pdf}
}
```
