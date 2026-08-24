# MindFS 二开第二轮 软件设计文档

> 对应需求：[round2-prd.md](./round2-prd.md)　|　生成日期：2026-08-22
>
> 本文档只描述设计与依据，不含实现。所有"现状"结论均来自 2026-08-22 对本仓库的三项专项代码调查（数据存储、服务生命周期、通知/定时/日志），关键代码位置随文标注。

## 1. 现状依据速查（设计输入）

| 事实 | 位置 | 影响的设计 |
|---|---|---|
| 清理协程与进程退出竞态：信号一到 main 即 return | `cli/cmd/mindfs.go:367-375`；清理协程在 `server/app/server.go:192-197` | §2.1 |
| Windows `-stop`/`-restart` = `taskkill /PID <pid> /T /F` | `cli/cmd/mindfs_windows.go:38-53` | §2.4 |
| 自更新重启 = spawn 替身 + 500ms 后 `os.Exit(0)` | `server/internal/update/service.go:749-772` | §2.5 |
| `Pool.CloseAll()` 丢弃 sessions 不逐个 Close；`claude.Runtime.CloseAll()` 为空实现且不持 ctx | `agent/pool.go:500-523`、`agent/claude/session.go:60-64,174` | §2.3 |
| commandexec 长驻 shell 显式丢弃 ctx，用 `context.Background()` 启动；只有按会话的 `CloseSession`，无全局关闭 | `commandexec/long_shell.go:49,62,137,161,164` | §2.3 |
| `AppContext` 每 root 懒建 sqlite Manager 与 SharedFileWatcher，退出时无人遍历；全量释放逻辑已存在于 `ReleaseRootResources` | `api/appcontext.go:129,395,628-654` | §2.3 |
| `kanban.Service.Close()` 存在（drain 5s + cancel 兜底）但非测试代码零调用 | `kanban/service.go:106-149` | §2.3 |
| `relay.Manager` 有内部 `cancel` 字段但无导出关闭方法 | `relay/manager.go:41-59` | §2.3 |
| `http.Server.Shutdown` 不覆盖 hijacked 的 `/ws` 连接 | `server.go:158`、`api/ws.go` | §2.2 |
| 裸 `os.WriteFile` 非原子落盘共 9 处 | 清单见 §3 | §3 |
| 会话数据三处分布：项目 `.mindfs/`、`~/.mindfs/<rootId>/`、UserConfigDir 兜底 DB（`.link` 指针） | `fs/fs.go:186-202`、`session/manager.go:1636-1679,1770-1780` | §4 |
| sqlite 连接 `SetMaxOpenConns(1)`，schema 为幂等 CREATE + 容错 ALTER | `session/manager.go:1697-1735`、`kanban/task_store.go:64-149` | §4.2 |
| 删除会话只清 exchange/aux 两个文件，遗漏 debug | `session/manager.go:1069-1086` | §4.4 |
| 日志轮转只在守护启动一刻执行；后台模式由父进程重定向子进程 stdout/stderr 到日志文件；launchd 直接接管 fd | `cli/cmd/mindfs.go:523-533,543-572`、`cli/cmd/autostart_unix.go:116-117` | §5.1 |
| `/health` 仅返回 `"ok"`；无日志 API；前端无诊断面板 | `api/http.go:282,1502` | §5.2 |
| 通知扇出点单一（`notifyPayload`），`session.error` 未接入；标题状态词硬编码中文 | `api/appcontext.go:882,1066-1076`、`notify/notify.go:51-57,85-91` | §6.1 |
| 定时任务执行用 `WithCancel` 无超时；跳过原因覆盖 `LastError`；无历史 | `scheduled/tasks.go:326,408-426,519-529` | §6.2 |
| `ErrorBoundary.tsx` 实现完整但全仓零引用；`Toast.tsx:18` 显式丢弃 fatal | `web/src/components/ErrorBoundary.tsx`、`Toast.tsx:16-21` | §7 |

## 2. 模块一：优雅退出编排（R-1.1 ～ R-1.3）

### 2.1 总体结构：closer 栈 + 同步等待

**新文件 `server/app/shutdown.go`**（留在 `server/app` 包内，因为全部资源都在 `Start` 中创建；不新建包，遵循 N-1 上游友好原则）。

核心抽象是一个**关闭编排器**：
- `Start` 在创建每个长生命周期资源后，把对应的关闭动作按创建顺序**注册**进编排器（带名字，用于日志）。
- 触发退出时，编排器按**注册逆序（LIFO）**逐个执行关闭动作，每步记录耗时日志（`[shutdown] step=<name> ok/err elapsed=…`），整体预算 10 秒，超时打日志后强制退出（exit code 非 0，便于区分）。
- `Start` 的现有旁路协程（`server.go:192-197`）废除；改为 `Serve` 返回后**同步**执行编排器，`Start` 直到编排完成才返回。

**CLI 侧竞态修复**（`cli/cmd/mindfs.go:367-375`）：`select` 中 `ctx.Done()` 分支不再直接 `return`，改为继续等待 `errCh`（即 `app.Start` 返回）——退出的唯一出口是 `Start` 自己走完关闭链。`server/cmd/mindfs-server/main.go` 本就是同步调用，`Start` 语义变化后自动受益。

### 2.2 关闭序列（LIFO 展开后的实际顺序）

| 步 | 动作 | 依据/备注 |
|---|---|---|
| 1 | `http.Server.Shutdown`（子超时约 3s）——停收新请求、等待普通请求结束 | 现状传 `context.Background()` 无超时（`server.go:196`），必须改 |
| 2 | StreamHub 主动关闭全部 WS 连接 | hijacked 连接不受 `Shutdown` 管辖；StreamHub 需新增"广播关闭"能力（`appcontext.go:781` 懒建，注册时机在首个连接后，编排器需容忍 nil） |
| 3 | 停周期协程：`agentProber.Stop()`、projectscan / hosted-agents 刷新 / idle-release / update 检查 / tips / e2ee cleanup（均已挂 ctx，root ctx cancel 即停） | 现状已依赖 ctx，此步只是显式 cancel + 不需逐个等待 |
| 4 | `scheduled`：`cron.Stop()` 并**等待**其返回的 context（robfig/cron 的 Stop 返回"正在执行的 job 全部结束"信号），上限 5s | 现状 `tasks.go:143` 只在旁路协程里 Stop，无人等待 |
| 5 | agent `Pool`：先对活跃 sessions **逐个调用 `session.Close()`**，再走各 runtime 的 `CloseAll` | 修复 `pool.go:500-523` 丢弃 sessions 的缺陷；claude 部分见 §2.3 |
| 6 | commandexec：新增的全局关闭入口，遍历内部 sessions map 逐个走既有 `CloseSession` 终止逻辑 | 见 §2.3 |
| 7 | `AppContext.Close()`：遍历 `s.roots`，对每个 root 执行 `Watcher.Close()`（含 flush，`shared_watcher.go:384-394`）+ `Session.Shutdown()`（内部已是 drain-then-cancel，阶段 0.5 成果） | 逻辑复用 `ReleaseRootResources`（`appcontext.go:636-654`）做全量版本 |
| 8 | `kanban.Service.Close()`（已有 drain 5s + cancel 兜底） | 首个真实调用方终于出现 |
| 9 | `relay.Manager.Stop()`（新导出方法，内部执行已有 `m.cancel` 并关闭 WS 长连） | `manager.go:41-59` |
| 10 | 兜底：总预算 10s 的看门狗计时器，超时打印卡住的 step 名后强制退出 | 保证"停得下来"永远成立 |

**kanban `running` 脏状态的收敛**放在**启动侧**而非关闭侧：服务启动加载 task store 时，把上一次进程留下的 `running` 状态 stage run 统一标记为"中断"终态。理由：启动侧收敛对强杀路径（Windows taskkill 兜底、断电）同样有效，关闭侧标记只覆盖优雅路径；一处启动扫描比两处兜底简单。

### 2.3 单项资源的关闭能力补齐

| 资源 | 现状缺陷 | 设计 |
|---|---|---|
| `claude.Runtime` | 空结构体不跟踪 client，`CloseAll()` 空实现（`claude/session.go:60-64,174`）；`NewRuntime` 未接收 processCtx（`pool.go:52`） | Runtime 内维护活跃 session 注册表（创建登记、Close 注销，加锁），`CloseAll` 遍历调用既有 `session.Close()`（`session.go:547`）。**不改 SDK fork**——SDK 的 `Close` 已能终止子进程，缺的只是"谁记得它们" |
| `codex` / `acp` runtime | 已有 map 与 `CloseAll` | 不动 |
| commandexec | 无全局关闭；`newLongShellSession` 丢弃 ctx（`long_shell.go:137`），进程挂在 `context.Background()` | 新增包级全局关闭入口（内部 sessions 注册表已存在），逐个复用 `CloseSession` 的终止逻辑（含平台分叉的进程树终止）。**本轮不改 ctx 传递链**——把 ctx 穿进创建路径牵连面大且收益与全局关闭重叠 |
| `relay.Manager` | 无导出关闭 | 导出 `Stop()`：调用内部 cancel、主动关 WS（让中继端立即感知下线，而非等超时） |
| Windows ACP 进程树 | `killProcessTree` 实际只 `proc.Kill()` 单进程（`acp/process_windows.go:11-16`），名不符实 | 改为 `taskkill /T /PID` 或按进程快照遍历子进程；作为 R-1.1 验收②（无残留进程）的一部分修复 |

### 2.4 Windows 优雅停止通道（R-1.2）

**新增关机 API**：`POST /api/shutdown`。
- **鉴权**：双重限制——仅接受 loopback 来源的连接，且必须携带有效的本地 CLI token（基础设施已在 `server/app/local_cli_token.go`，`-remove` 命令已走同款鉴权先例）。Relay 通道转发的请求天然不持有本地 token，故远程不可达。
- **语义**：校验通过后立即响应 202，随后异步触发与信号路径完全相同的退出编排（同一个入口，不做第二套）。
- **CLI 接入**（`stopService`，`mindfs.go:605-629`）：先读本地 token 调关机 API → 沿用现有"轮询进程消失"等待（放宽到 12s，覆盖 10s 关闭预算）→ 失败/超时回退现有 `taskkill` 路径。Unix 保持 SIGTERM 优先（行为已达标），关机 API 作为两平台统一的备选。

### 2.5 自更新重启走退出链（R-1.3）

`restartInstalledBinary`（`update/service.go:749-772`）现状是"先 spawn 替身，500ms 后 `os.Exit(0)`"。两个问题：`os.Exit` 跳过一切清理；Unix 替身脚本仅 `sleep 1` 就 exec，若旧进程关闭耗时会撞端口。

**设计**：顺序反转为"**先关闭、后替换**"——
1. 触发退出编排并等待完成（listener 已释放、数据已落盘）；
2. 再 spawn 替换进程（Unix 的 `sleep 1` 从"等旧进程死"变成纯余量；Windows 的 PowerShell 脚本本就轮询等旧 PID 消失、最多 20s，10s 关闭预算在窗口内，时序兼容）；
3. 正常 `return`/exit。
实现位置上，update 服务需要一个"请求进程退出"的回调（由 `server/app` 注入），而不是自己 `os.Exit`——update 包不应知道退出细节。

## 3. 模块二：关键 JSON 原子落盘（R-1.4）

**方案**：在 `server/internal/config`（或 fs 包）提供统一的"写临时文件 + rename"helper，语义对齐已有正确先例 `fs.WriteMetaFile`（`fs/fs.go:311-329`）与 `preferences.Store`（`store.go:277-283`），支持传入权限位。

**改造点清单**（开工时以全仓 `os.WriteFile` 核查为准，当前已确认 9 处）：

| # | 位置 | 内容 | 权限 |
|---|---|---|---|
| 1 | `fs/registry.go:141` | registry.json（托管目录注册表，损坏 = 丢全部项目配置） | 0644 |
| 2 | `relay/credentials.go:76` | credentials.json | 0600 |
| 3 | `relay/credentials.go:100` | 同上（第二写入点） | 0600 |
| 4 | `relay/services.go:144` | relay-services.json | 0644 |
| 5 | `api/usecase/prompts.go:149` | prompts.json | 0644 |
| 6 | `api/agent_config.go:606` | agent 配置清单/文件 | 0644 |
| 7 | `api/agent_config.go:638` | 同上 | 0644 |
| 8 | `api/agent_config.go:687` | 同上 | 0644 |
| 9 | `api/agent_config.go:728` | agents-env.json——**同时落实 R-3.1，权限改 0600** | 0600→ |

注意 Windows 上 rename 到已存在目标的语义（Go 的 `os.Rename` 在 Windows 覆盖已存在文件自 Go 1.5 起可用，但被其他进程打开时会失败）——helper 需要带一次短重试，测试要点见开发文档 T12。

## 4. 模块三：数据备份与导出（R-5）

### 4.1 新包与端点

**新包 `server/internal/backup`**，HTTP 接线在 `api/http.go` 增两条路由：

| 端点 | 语义 |
|---|---|
| `POST /api/backup/export` | body：`{scope: "root"|"all", root?: string, include_credentials: bool}`；响应为 zip 流（`Content-Disposition: attachment`），文件名含时间戳。手机浏览器可直接下载 |
| `GET /api/storage/report?root=<id>` / `POST /api/storage/cleanup` | 见 §4.4 |

### 4.2 导出内容与一致性

**zip 布局**：

```
manifest.json
RESTORE.md
roots/<rootId>/…            ← 该 root 的元数据目录全量（project 模式取 <root>/.mindfs，home 模式取 ~/.mindfs/<rootId>）
fallback-db/<escapedRootId>/session-list.db   ← 仅当存在 .link 兜底时
userconfig/…                ← 仅 scope=all：用户配置目录中的数据类文件
```

**三处分散存储的覆盖规则**：
- 对每个 root，按 `MetaDir()` 的真实解析结果（`fs/fs.go:186-202`）取元数据目录——而不是假设它在项目下。
- 读取 `sessions/session-list.db.link`（`session/manager.go:31`）：若存在，把它指向的兜底 DB 也纳入 `fallback-db/`，manifest 记录该映射。**这是手工 copy 必漏、本功能的核心价值点。**

**sqlite 一致性快照**：导出时服务在运行、DB 可能正在写。设计采用 **`VACUUM INTO`**（modernc.org/sqlite 支持）产出一致性副本后入包，而非直接读文件：
- `session-list.db`：经 session Manager 暴露的快照导出能力执行（连接 `SetMaxOpenConns(1)` 串行化，天然与写互斥）；
- `tasks/task-kanban.db`：同理经 kanban store；
- `commands/history.db`：**直接文件 copy**——它本就是每次调用短连接开关（`command_history.go:158`），且命令补全历史属低价值数据，不值得为它加接口。manifest 中标注该文件为"尽力而为"。

**JSONL 文件**（exchange/aux/debug）为 append-only，直接流式入包，无一致性问题（最坏截断到最后一行边界，读取侧本就逐行解析）。

**凭据排除**（R-5.4，`include_credentials=false` 时跳过）：`credentials.json`、`agents-env.json`、`autostart-environment.json`、`local-cli-tokens.json`、`e2ee.json`、`key.pem`、`agents-config/` 备份目录。manifest 记录 `includes_credentials` 布尔。

### 4.3 manifest 与恢复

`manifest.json` 字段：`format_version`（从 1 起）、`mindfs_version`、`exported_at`、`includes_credentials`、`roots[]`（每项：`root_id`、`root_path`、`meta_location`（project/home）、`archive_path`、`has_fallback_db`）。

**恢复不做 API/UI**（个人场景一年用不到一次，做 UI 是过度设计）：包内 `RESTORE.md` 按三种布局分别写清"停服务 → 解压到哪 → 起服务"的步骤，并说明 `.link` 兜底文件的去向。R-5.2 验收即按此文档执行。

### 4.4 存储体检与清理（R-5.3）

`GET /api/storage/report?root=<id>`：遍历该 root 元数据目录，返回 `{sessions_count, exchange_bytes, aux_bytes, debug_bytes, db_bytes, upload_bytes, orphan_debug_files[], journal_files[]}`。磁盘遍历允许秒级耗时，handler 侧不做缓存但也不阻塞其他请求（N-4）。

**孤儿判定**：`sessions/<key>.debug.jsonl` 的 key 不存在于 `sessions` 表 → 孤儿（历史遗留，因 R-3.2 修复前删除的会话）。
**清理动作**（`POST /api/storage/cleanup`）只删孤儿 debug 文件——这是唯一无风险的删除。
**`*-journal` 文件不直接删除**：journal 是 sqlite 的崩溃恢复日志，直接删会损坏数据。处理方式是"安全回收"——对其对应的 DB 执行一次打开-关闭，sqlite 自动完成回滚并移除 journal；仍消不掉的（说明 DB 正被占用或确有问题）保留并在报表中提示。

## 5. 模块四：日志与远程诊断（R-4）

### 5.1 运行期日志轮转（R-4.1）

现状的结构性问题：后台模式下日志文件 fd 由**父进程**在 spawn 时重定向（`mindfs.go:526-533`），子进程（真正的服务）对日志文件毫无控制，因此运行期不可能轮转；launchd 场景（`autostart_unix.go:116-117`）同理。

**设计——把日志所有权移入服务进程**：
- 服务进程启动早期将标准库 log 的输出定向到一个**轮转 writer**（写入时检查大小，超过沿用现有 `maxLogSizeBytes=10MB` 即滚动 `.1/.2/.3`，复用 `rotateLogIfNeeded` 的滚动语义，`mindfs.go:543-572`）。轮转 writer 内部加锁，滚动瞬间的并发写安全。
- 父进程/launchd 对子进程 stdout/stderr 的重定向**保留但改指向** `<logname>.stderr`——仅承接 panic 与绕过 log 包的裸输出（量极小，启动时截断即可，不轮转）。这样避免"两个写者同一文件"导致的乱序与大小计数失效。
- **多实例分离**：日志文件名与 PID 文件同规则携带地址（`mindfs-<addr>.log`），`-status`（`mindfs.go:688-704`）与启动横幅同步输出新路径。旧的无后缀 `mindfs.log` 留在原地不迁移。

### 5.2 日志与状态 API（R-4.2 / R-4.3）

| 端点 | 设计 |
|---|---|
| `GET /api/logs?lines=N` | 返回当前日志文件尾部 N 行（默认 200、上限 2000）与文件元信息 `{path, size_bytes, truncated}`。实现为从文件尾反向读块，不整文件载入。走既有 `/api/*` 会话鉴权，Relay 模式天然可用。不做 follow/streaming——手机场景"刷新一下"足够，SSE 是过度设计 |
| `GET /api/diagnostics` | 聚合内存态（不做磁盘/网络重扫描，N-4）：`version`、`started_at`、`uptime_seconds`、`os_arch`、`addr`、`roots[]{id, path, meta_location, session_count}`、`agents[]{name, protocol, available, last_probe_at}`、`webpush{enabled, subscription_count}`（复用 `webpush/service.go:362` 已有 status）、`relay{bound, connected}`、`scheduled{task_count, next_run_at}`、`log{path, size_bytes}` |

### 5.3 前端诊断面板

**新组件 `web/src/components/DiagnosticsPanel.tsx`**，内含三个分区：状态总览（diagnostics）、日志尾部（等宽、横向滚动防破版、刷新按钮）、存储与备份（§4 的报表 + 导出按钮 + 凭据提示 checkbox）。

**入口**：侧边栏 FileTree 的菜单区（与 Web Push 通知设置同区，`FileTree.tsx:223` 一带），新增一个菜单项打开面板（BottomSheet/对话框，复用现有容器组件）。对 `App.tsx` 只做挂载点级接线（N-1）。服务封装新增 `web/src/services/diagnostics.ts`，纯 fetch 模块，沿用 vm 沙箱测试方式。

## 6. 模块五：通知与定时任务修缮（R-2.3 / R-6 / R-7）

### 6.1 `session.error` 通知（R-2.3）

扇出基础设施完全就绪（`notifyPayload`，`appcontext.go:1066-1076`；两通道各有 30 分钟去重）。改动收敛为：
- `BroadcastSessionError`（`appcontext.go:882`）中构造 `type: "session.error"` 的 payload 并调用 `notifyPayload`，事件 ID 沿用现有 `session+seq` 去重口径；
- payload 构造放 `notify` 包（与 `session.done` 的 builder 并列，`notify/notify.go`）；正文取错误摘要而非会话尾部；
- 前端 Service Worker 无需改动（`vite.config.ts:83,103` 的 push/click 处理是类型无关的，按 `data.url` 跳转）。
- 与 `session.done` 同款的定时任务豁免逻辑（`appcontext.go:932-934` 的 `scheduled:` 前缀判断）需要对齐：定时任务失败已有 `scheduled.failed`，`session.error` 对 `scheduled:` 前缀同样跳过，避免双推。

### 6.2 定时任务超时与错误保留（R-6.1 / R-6.2）

- `runTask` 的执行 context 从 `WithCancel` 改为 `WithTimeout`（`tasks.go:326`）：默认 60 分钟；任务结构体新增可选的超时分钟数字段（JSON 向后兼容：缺省即默认值，旧文件可直接读）。超时后现有 defer 链释放 `running` 锁，`LastError` 记为超时。
- 并发跳过不再写 `LastError`（`tasks.go:415-425`）：任务结构体新增 `last_skipped_at` 字段单独记录，UI 侧在任务行以次级信息展示"上次因仍在运行而跳过"。
- （P2，R-6.3）执行历史：`scheduled-agent-tasks.json` 的任务对象内嵌 `history[]`，环形保留 20 条 `{started_at, finished_at, ok, error}`；Dialog 内新增可折叠的历史列表。文件级读-改-写竞态（`tasks.go:550-567`）在个人场景概率极低，本轮记录不修。
- （P2，R-6.4）解析器启用 `cron.Descriptor`（`tasks.go:119`）并同步放宽前端 `isCronSegmentValid` 校验（`ScheduledAgentTaskDialog.tsx:91`，descriptor 整体校验而非分段）；`decorateTask` 的 `NextRunAt` 统一转 UTC（`tasks.go:589`），前端负责本地化展示。

### 6.3 通知 i18n（P2，R-7.1）

`notify` 包的两个 builder（`notify.go:51-57,85-91`）状态词硬编码中文。设计：builder 增加语言参数，由调用方（AppContext）从用户偏好取界面语言；词条表就近放在 notify 包内（服务端没有 i18n 基建，为 4 个状态词引入框架不值得——两语言 × 5 词条的映射表即可）。

## 7. 模块六：前端错误可见性（R-2.1 / R-2.2）

- **ErrorBoundary 接线**：`App.tsx` 中主视图渲染区包 `MainViewErrorBoundary`、抽屉面板区包 `DrawerPanelErrorBoundary`（两个特化变体已实现，`ErrorBoundary.tsx:143,158`）。这是纯 JSX 包裹级改动，选择包裹边界时以"崩溃隔离域"为准：主视图崩溃不杀侧边栏，抽屉崩溃不杀主视图。
- **fatal 兜底**：`Toast.tsx:18` 的 `if (severity === "fatal") return` 移除，fatal 渲染为**不自动消失**的持久条目（复用现有 toast 结构，无超时 + 需手动关闭 + 视觉加重）。`error.ts` 的 40 个错误码默认 severity 不变。

## 8. 与现有架构的衔接总结

| 触点 | 衔接方式 |
|---|---|
| `server/app/server.go` | 只加"注册 closer"调用与编排器接线，编排逻辑在新文件 `shutdown.go`；上游对 Start 的改动大概率仍可干净合并 |
| `cli/cmd/mindfs.go` | 两处小改：退出等待逻辑（§2.1）、`stopService` 优先走关机 API（§2.4）；日志路径常量调整（§5.1） |
| `api/http.go` | 新增 4 条路由（shutdown/logs/diagnostics/backup + storage 2 条），handler 实体放各自包，http.go 只做注册——沿用第一轮批量删除的接线模式 |
| usecase 层 | backup/storage 经 usecase 转发到 AppContext/Manager，沿用 `rootScanner` 可选接口断言的既有模式（fork-plan §2.1），避免改 Registry/Manager 接口牵连测试 fake |
| 前端 | 新组件 + 新 service 模块为主；`App.tsx` 仅 ErrorBoundary 包裹与面板挂载两处接线；`FileTree.tsx` 仅加一个菜单项 |
| CI | 无需改动，沿用双平台矩阵；新增 Go 测试自动纳入 `make test-go`，前端纯逻辑模块沿用 vm 沙箱测试进 `make test-web` |

## 9. 技术风险与取舍登记

| 风险/取舍 | 决策与应对 |
|---|---|
| 退出编排改动 `Start` 语义，可能与上游未来重构冲突 | 编排器独立文件；`Start` 内只留注册调用。上游若自己做优雅退出（迟早），届时以上游为准弃用本实现——因此**不做过度工程**，够用即可 |
| Windows 上 `os.Rename` 覆盖被占用文件失败 | 原子写 helper 带一次短重试；失败时保留 tmp 文件并报错，绝不半写目标文件 |
| `VACUUM INTO` 在目标路径已存在时报错、耗时与 DB 大小成正比 | 导出到全新临时路径；个人规模 DB 在 MB 级，秒内完成；失败时该 DB 降级为直接 copy 并在 manifest 标注 `snapshot: false` |
| 备份包含明文凭据外泄 | 默认勾选"包含凭据"但弹出明示提醒；提供排除选项（R-5.4）；文档写明包应视同密码保管 |
| 关机 API 被滥用 | loopback + 本地 CLI token 双限制；Relay 转发请求不持有 token；测试覆盖 401/403 路径 |
| 自更新时序回归（替换脚本对旧进程退出时机的假设） | Windows 脚本轮询窗口 20s > 关闭预算 10s；Unix 改为"先关后 spawn"从根上消除竞态；两平台均需真实走一次更新回归（开发文档 T11 测试要点） |
| 日志所有权迁移导致旧日志路径失效 | `-status` 与启动横幅同步输出新路径；旧 `mindfs.log` 不删除；release note 注明 |
| claude 会话注册表与 SDK 生命周期不同步（泄漏/双关） | 注册表只在 MindFS 侧登记/注销，Close 幂等（SDK Close 二次调用需验证无 panic，测试覆盖） |
| scheduled 超时中断正在写会话的 agent | 超时 cancel 的传播路径与用户手动"停止回复"一致（同一 SendMessage 链路），不引入新中断语义 |
| 前端 ErrorBoundary 包裹边界选错导致局部崩溃扩大 | 按现成两个特化变体的注释意图包裹；测试挂钩人为抛错验证隔离域 |
