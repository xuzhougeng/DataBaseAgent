# DataBaseAgent（DBA）

基于 **Wails v3.1.0 + Go + 原生 HTML/CSS/JavaScript** 的本地主题数据库构建工作台。九个页面沿用提供的设计包：所有项目、项目概览、主题与检索、数据复核、处理流程、运行任务、服务器资源、数据与产物、Agent 中心。

## 运行与构建

运行 Windows 成品需要 WebView2 Runtime，远程执行还需要系统 OpenSSH 客户端。从源码构建需要 Go 1.26+。前端没有 npm 依赖，静态资源直接嵌入可执行文件。

```powershell
./scripts/build.ps1
./bin/DBA.exe
```

默认工作空间为 `%APPDATA%\DataBaseAgent`，保存 `workbench.json` 与 `projects/<项目ID>/results/<任务ID>/`。用 `-workspace "D:\DBA-workspace"` 指定其他目录。同一工作空间的桌面实例复用现有窗口。

浏览器开发预览使用同一个后端和同一套静态资源：

```powershell
go run ./cmd/dba-server -port 3415 -workspace .tmp/dev-workspace
```

服务仅监听 `127.0.0.1`。请勿让开发服务和桌面端同时使用同一工作空间。原始设计保存在 `design/DBA_design_package/`，包括未经修改的 HTML 和校验清单。

## 使用路径

1. 创建项目，填写主题、纳入标准及偏宽的检索表达式。
2. 通过 Europe PMC、PubMed 或 GEO 检索，或导入候选 JSON。每次显示命中总数与实际获取数（上限 1000），保存检索历史并合并去重。GEO 结果可能包含 series、sample 或 platform，需要人工筛选。
3. 配置模型后让 Agent 给出复核建议，核对原文证据，再人工纳入或排除。也支持完全人工复核。复核必须填写理由；可补充来源编号、直接下载地址及分组。
4. 添加 Linux SSH 服务器，先在终端验证免密登录与主机指纹，再在应用中检查连接。填写专用的远程绝对工作目录和 CPU/并发配额。
5. 定义工具、shell 命令和预期输出文件，锁定流程版本，再把人工纳入的数据加入队列。默认下载步骤使用 `curl` 断点续传，后续处理步骤需由用户定义。
6. Agent 定期检查进程和退出码，按配额调度任务，识别少量网络类错误并有限重试；其他命令错误、状态不明或连续失败转为求助。结果按清单增量同步，逐文件 SHA-256 校验后归档。

流程命令可以使用 `DBA_ACCESSION`、`DBA_SOURCE_URL`、`DBA_INPUT`、`DBA_OUTPUT`、`DBA_SAMPLE_GROUPS` 和 `DBA_CPUS`。步骤在 `$DBA_OUTPUT` 内顺序运行，错误即停止。输出路径为相对路径，例如 `expression_matrix.tsv`、`qc/report.html`。应用另生成 `run_manifest.json`。

## 模型与网络

在偏好设置中填写支持 Chat Completions 协议的 API 基础地址、模型名称和密钥环境变量名。密钥只从进程环境读取，不写入项目文件。例如先设置 `$env:DBA_MODEL_API_KEY`，再从同一终端启动应用。模型请求会包含对应项目的范围、候选元信息或任务摘要。端点请填写到服务前缀，如 `https://provider.example/v1`，应用追加 `/chat/completions`。

检索和模型 HTTP 请求遵循启动进程的 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量。GitHub 操作及构建脚本使用 `http://127.0.0.1:7897` 代理。SSH 采用本机密钥或 SSH Agent，强制校验已知主机，不支持交互式密码输入。

## 当前边界

这是可运行的第一版，使用本地 JSON 原子保存，适合先贯通单服务器闭环。执行器目前支持 Linux SSH，不含 Slurm、容器管理、内存/磁盘硬配额或系统服务。CPU 配额控制提交容量，实际线程数须在工具参数中设置。关闭 DBA 后远端进程继续运行；重新打开后恢复巡检。该循环按固定恢复策略工作，模型用于建议和对话，不自动改写分析命令。

新版本不会自动套用到旧任务；终止或删除远端任务、编辑服务器配置、按时间段穷尽海量检索结果等功能尚未提供。失败后的求助允许补充分组或在远端修复环境；需要修改工具或参数时，应锁定新流程并提交新任务。SSH 远端需要 `sh`、`nohup`、`realpath`、`sha256sum` 和所选工具。原始数据下载由已确认的流程命令完成，不会从任意论文自动推断完整 FASTQ 下载方案。

## 验证

```powershell
go test ./...
go vet ./...
node --check frontend/workbench.js
```

测试覆盖项目隔离、人工纳入门槛、流程与输入快照、跨项目服务器配额、连接失败不重复提交、有限恢复和人工接手、校验与本地归档、重启持久化、损坏文件保护、检索去重与获取上限、模型证据校验和 HTTP 来源检查。自动化测试通过可控执行器验证调度闭环；真实服务器和模型端点仍须使用自己的配置完成联调。
