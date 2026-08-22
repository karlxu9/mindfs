# 二开第二轮 实现说明（研发记录）

> 按任务记录实现过程中的决策、偏差与验证结果；需求与设计以 round2-prd.md / round2-design.md 为准，本文件不改需求。

## T1　ErrorBoundary 接线 + fatal toast 兜底【R-2.1 / R-2.2】

**实现**（2026-08-22）：

- `App.tsx`：`AppShell` 的 `main` 插槽整体包 `MainViewErrorBoundary`；抽屉包裹点选在 **BottomSheet 内部**（children 外层）而非包住整个 BottomSheet——崩溃时降级 UI 显示在抽屉容器内，抽屉自身的关闭/展开按钮仍可用，主视图不受影响。合计 5 行接线级新增，未重排既有缩进（上游友好）。
- `Toast.tsx`：移除 fatal 的 `return` 过滤；过期逻辑抽出为纯逻辑模块 `toastModel.ts`（`toastExpiresAt` 返回 `null` 表示永不过期，`pruneExpiredToasts` 保留 `null` 条目），沿用 `gitDiffModel.ts` 先例以便 vm 沙箱测试。fatal 视觉加重：深红底（red-900）、描边、加重阴影、🛑 图标。
- 新增测试 `web/tests/toast-fatal.test.mjs`：fatal 持久/非 fatal 定时过期的纯逻辑断言 + Toast.tsx 不再丢弃 fatal、App.tsx 两处包裹存在的源码断言。

**验证记录**：

- `npm run typecheck` 绿；`web/tests` 全量 13 个测试绿。
- 开发模式手工验证（vite dev + 本机常驻后端，临时崩溃挂钩验证后已撤除，截图在仓库根 `verify-t1/`，未入库）：
  1. 主视图渲染抛错 → 降级 UI，文件树/会话列表/ActionBar 均存活，"重试"后看板恢复；
  2. 抽屉内渲染抛错 → 降级 UI 限于抽屉容器内，主视图无恙；
  3. report fatal 后出现持久提示，9 秒不消失（对照 warning 3 秒自动消失），✕ 手动关闭生效。

**实现发现（对后续任务有用）**：React 18 开发模式对渲染期错误会同步重试一次，只抛一次的"瞬时"错误会被当作 recoverable error 吞掉、不触发 ErrorBoundary；人为验证挂钩必须在解除前持续抛错。

## T2　`agents-env.json` 权限收紧【R-3.1】

**实现**（2026-08-22）：`agent_config.go` 的 `writeAgentEnvBackups` 写入权限 0644 → 0600（该文件其余三处 0644 写入点属 T12 原子写收编范围，本任务不动）。新增 `TestWriteAgentEnvBackupsOwnerOnlyPermissions`，`runtime.GOOS == "windows"` 时跳过。

**验证记录**：`go build ./...` 绿；api 包全量测试绿（含既有备份/切换用例）；本机（Windows）确认新测试正确 SKIP，Unix 分支由 CI ubuntu runner 覆盖。

**边界说明**：`os.WriteFile` 的权限位只对**新建**文件生效，升级用户既有的 0644 文件重写后权限不变。PRD 验收口径即"新写入的文件"，故未额外 chmod；T12 的临时文件 + rename 方案落地后，旧文件会被新建文件替换，权限残留自然消除。

## T3　删除会话清理 `.debug.jsonl`【R-3.2】

**实现**（2026-08-22）：`session/manager.go` 的 `deleteSessionUnsafe` 文件删除清单补第三项。debug 路径按本包既有模式落地：`debugFileTpl` 常量 + `debugPath(key)` 方法（含与 exchange/aux 相同的 key 校验），常量旁注释指向写入方 `agent/logs` 的 `toolCallDebugFileTpl`，防两处漂移。单删、批量删、级联删三条路径均收敛于 `deleteSessionUnsafe`（`usecase.DeleteSession` → `DeleteSessions` → `manager.Delete`），一处修复全覆盖。

**验证记录**：`go build ./...` 绿；session 与 usecase 两包全量测试绿。新增 `TestDeleteSessionRemovesAllSessionFiles`（三文件删净 + 无文件会话删除不报错）与 `TestDeleteSessionsCascadeRemovesDebugLogs`（级联删除连未点名的子会话 debug 文件一并清掉）。

## T4　`session.error` 通知【R-2.3】

**实现**（2026-08-22）：

- `notify` 包：error payload 实现为 `BuildSessionPayload` 的 `session.error` 分支（状态词"出错"、tag 带 eventID 后缀与 done 同款、Renotify true），而非独立 builder 函数——三种 session 通知结构完全一致，独立函数是 30 行重复。开发文档写的是"新增 error payload builder"，此处按零重复原则落地，语义等价。
- `BroadcastSessionError` 增加 `requestID` 参数（对齐 `BroadcastSessionDone`）：ws 路径传真实 requestID，kanban 路径传空串，scheduled 路径传 `scheduled:<taskID>`；`scheduled.SessionActivityBroadcaster` 接口同步改签名（该包无测试 fake，牵连面即三个调用点）。
- 新增 `notifySessionError`：`scheduled:` 前缀直接豁免（定时任务已有 `scheduled.failed`，避免双推）；eventID 取 requestID，空时回落 `session.error:<root>:<session>:<pending.UpdatedAt>`（与 `notifySessionDone` 同口径）。
- **有意的行为**：error 与同轮 done 共用 requestID 作为 eventID，error 先发出后，同轮的 `session.done` 通知会被两通道各自的 30 分钟去重窗口抑制——失败的轮次用户只收到一条"出错"，不会再收一条"完成"。
- `AppContext` 增加未导出的 `notifyPayloadOverride` 测试钩子（3 行分流），因 `WebPush`/`Notify` 均为具体类型无法注入假实现；比起用真脚本做异步进程级假 notifier，这个钩子更稳定。

**验证记录**：全仓 `go test ./...` 绿。新增 `notify` 包 builder 字段快照测试（error 全字段 + done/ask_user 状态词不回归）与 `TestBroadcastSessionErrorNotifiesAndExemptsScheduled`（触发 ×2 / 豁免 ×1 三条路径）。真机推送（手机 PWA 收到并点击跳转）属手工回归项，待部署后随阶段验收一起过。

## T12　原子写 helper + 9 处替换【R-1.4】

**实现**（2026-08-22）：

- 新增 `config.WriteFileAtomic(path, data, perm)`（`server/internal/config/atomic.go`）：同目录 CreateTemp → 写入 → chmod → close → rename，语义对齐 `fs.WriteMetaFile`。rename 失败等 10ms 重试一次（**两平台统一**，设计只要求 Windows，但统一重试逻辑更简单且 Unix 上无害、ubuntu CI 也能覆盖重试路径）；重试仍失败则**保留 tmp 文件**（新数据不丢）并在错误信息中点名；写入/chmod 阶段失败则清理 tmp（半截数据无保留价值）。`renameFile` 抽为包变量作为测试注入点。
- 9 处替换全部完成：`fs/registry.go`、`relay/credentials.go` ×2、`relay/services.go`、`usecase/prompts.go`、`api/agent_config.go` ×4（含 T2 的 agents-env.json，权限 0600 收编进 helper）。5 个文件本就 import configpkg，均为单行平移，调用方的 apperr.Wrap / MkdirAll / Chmod 原样保留。
- **与设计表格的一处出入**：`relay-services.json` 设计 §3 表格权限列写 0644，但代码现状是 0600——按"不放宽权限"原则保持 0600，表格值疑为笔误，请产品复核。

**验证记录**：helper 4 例单测全过（新写权限位、覆盖旧内容、rename 永久失败时目标保持旧内容且 tmp 保留新数据、瞬时失败重试一次成功）；受影响 5 包 + 全仓 `go test ./...` 绿（含 `fs/registry_test.go` 既有用例）。Windows 本机绿，ubuntu 侧由 CI 矩阵覆盖。

## T5　退出编排器 + CLI 等待竞态修复【R-1.1 骨架】

**实现**（2026-08-22）：

- 新文件 `server/app/shutdown.go`：`shutdownOrchestrator`——按创建顺序 `register(name, fn)`、LIFO 执行、每步 recover + 耗时日志（`[shutdown] step=<name> ok/err elapsed=…`）、总预算 10s 看门狗（超时打印卡住的 step 后 `exitFn(3)` 强退，退出码 3 区分正常退出；`exitFn`/`budget` 为字段以便测试注入）。`run()` 为 once-and-wait 语义：信号协程与 Serve 返回后的同步调用不会双执行，后到者等待同一次执行完成。
- `server/app/server.go`：废除 192-197 旁路协程；T5 范围内注册三步（创建顺序 agent-pool → agent-prober → http-server，LIFO 执行时 http.Shutdown（3s 子超时）最先、让 Serve 返回）；`Start` 直到编排完成才返回。**顺带修复**：优雅关闭时 `Serve` 返回的 `http.ErrServerClosed` 在 `Start` 内归一为 nil——否则 `mindfs-server` 入口会把正常退出当错误 exit 1（手工验证时发现）。
- `cli/cmd/mindfs.go`：`select` 中 `ctx.Done()` 立即 return 的分支删除，改为直接等 `errCh`（即等 `Start` 走完关闭链）；看门狗保证等待有界。`mindfs-server` 本就同步调用，自动受益。

**验证记录**：编排器 4 例单测（LIFO、panic 不中断后续、run 幂等并发、看门狗强退码 3）；全仓 `go test ./...` 绿。前台信号路径本机真实验证：Windows 上以隔离 APPDATA 沙箱启动 `mindfs-server`（端口 7999、-no-relayer），`GenerateConsoleCtrlEvent(CTRL_BREAK)` 后日志出现完整关闭序列（begin → http-server → agent-prober → agent-pool → done），10ms 内退出、exit code 0（验证脚手架为临时文件，未入库）。Unix `kill -TERM` 路径与 Windows 信号路径共用同一 `signal.NotifyContext` 链，逻辑一致，留待 CI/实机回归。

## T6　HTTP/WS 停止完善

**实现**（2026-08-22）：`StreamHub.CloseAllClients()`——快照取全部连接后逐个发 `CloseGoingAway` 控制帧（1s 写超时）再 `Close()`，解除 hijacked 连接读循环的阻塞；`AppContext.CloseAllStreamClients()` 只在 hub 已创建时关闭（不懒建，"没有 hub 就没有要关的"）；编排器注册 `ws-clients` 步骤于 http-server 之前（LIFO 执行序：http-server → ws-clients → prober → pool）。

**验证记录**：新增带 3 条真实 WS 连接的关闭测试（CloseAllClients 耗时上限断言 + 三客户端全部断开）与 hub 未创建的 no-op 断言；api、app 两包及全仓测试绿。前端重连逻辑核实：`session.ts` 的 `onclose` 不区分 close code、无条件 `scheduleReconnect()`，服务端主动断开后 UI 自动恢复有代码依据；真实重启的 UI 手工回归并入阶段 2 收尾 checklist（避免中断本机常驻服务）。

## T7　AppContext / scheduled / kanban 关闭接线 + 启动侧 running 收敛

**实现**（2026-08-22）：

- `AppContext.Close()`：全量遍历 `roots` 释放 Watcher + Session（`ReleaseRootResources` 的全量版）。**注册顺序特例**：app-context 在 `Start` 顶部先于 agent-pool 注册（闭包捕获后赋值的 `services` 变量），使它在 LIFO 中最后执行——sqlite 句柄与 watcher 必须活过所有仍会写入它们的关闭方（pool/scheduled/kanban）。这是对"创建后注册"原则的唯一例外，root 资源本就是懒建贯穿运行期的最底层资源。
- `scheduled.Service.Stop(ctx)`：等待 `cron.Stop()` 返回的 context，上限 5s（外部 ctx deadline 更短时以外部为准，天然可测）；`Start` 里的旁路停止协程删除，停止改由编排器负责。
- `kanban.Service.Close()` 注册进编排器（该方法注释中"server/app 尚无人调用"的债务就此清偿）。
- 启动侧 running 收敛落在 **`NewTaskStore` 打开时**（而非 Service 层）：`recoverInterruptedStageRuns` 把 status='running' 的 stage run 置为 **cancelled**（补 finished_at）。打开时刻本进程必然还没有任何执行中 stage（store 是每 root 单例、一切执行经它），故此时的 running 行必为上个进程残留，对强杀/断电路径同样生效。**状态值选择**：设计文档说"中断终态"未指定字符串，选用现有 `StageStatusCancelled` 而非新造 "interrupted"——前端与服务端的状态枚举处理无需任何改动，语义损失（无法区分人为取消与进程中断）对个人场景可接受，请产品复核。

**执行序与设计 §2.2 表的差异**：实际 LIFO 序为 http → ws → (relay,T10) → kanban → scheduled → prober → pool → app-context；设计表为 … scheduled(4) → pool(5) → appctx(7) → kanban(8) → relay(9)。差异点：kanban 先于 scheduled/pool 关（drain 时 agent 还活着，正确）；appctx 移到最后（比设计表的 7 更安全——kanban(8) drain 中的写库不会撞上已关的句柄）；relay 提前（中继端更早感知下线，符合 §2.3 意图）。依赖关系均满足，表格顺序按实现修正。

**验证记录**：kanban 收敛测试（预置 running + success 两行 → 重开 store → running 变 cancelled 且补 finished_at、success 不受扰）；`AppContext.Close` 测试（真实 session.Manager 开库后 Close，Windows 本机 `os.RemoveAll(metaDir)` 立即成功——正是 DoD 的句柄释放验证）；`scheduled.Stop` 两例（等到 300ms 内完成的 job / 对卡死 job 按 deadline 放弃并报错）。全仓测试绿。端到端：沙箱 `mindfs-server` + CTRL_BREAK，关闭序列 7 步全出现、app-context 殿后、20ms 退出 exit 0。

## T8　agent Pool / claude Runtime 关闭补齐

**实现**（2026-08-22）：

- claude `Runtime` 由空结构体改为持 `map[string]*session` 注册表（对齐 codex 先例，按 sessionKey 登记）：`OpenSession` 成功即 `track`；`session.Close` 的 closeOnce 内 `forget`（带身份校验——同 key 重开的新会话不会被旧会话的 Close 误删）；`CloseAll` 快照后逐个 `session.Close()`（SDK Close 终止 claude CLI 子进程），不改 SDK fork。
- `Pool.CloseAll`：清 map 前先逐 entry 调 `session.Close()`（旧实现直接丢弃 map，等于泄漏全部 agent 子进程），随后照旧 cancel + 各 runtime CloseAll。幂等性由 session 层 closeOnce 保证，双关无 panic。

**验证记录**：注册表两例单测（track/Close 注销/CloseAll 清空幂等/已关会话再关无 panic；同 key 替换会话不被前任 Close 驱逐）；agent 全部子包 + 全仓测试绿。真实 claude 会话退出后进程消失的验证属手工回归项，并入阶段 2 收尾 checklist（需要真实 agent 会话，不在本机常驻服务上做）。

## T9　commandexec 全局关闭 + Windows 进程树修正

**实现**（2026-08-22）：

- `commandexec.CloseAllSessions()`：快照全部长驻 shell 会话、清 map、逐个 `killShell()`（内部 `proc.KillTree()`，Windows 实现本就是 `taskkill /T /F`，无需改）；编排器注册 `command-shells` 于 agent-pool 之前（LIFO 执行序 pool 先、shells 后，agent 关闭中还可能驱动 shell）。ctx 传递链不动（设计 §2.3 既定取舍）。
- `acp/process_windows.go` 的 `killProcessTree`：由名不符实的 `proc.Kill()`（只杀直接子进程）改为 `taskkill /PID <pid> /T /F`（对齐 commandexec 的 Windows KillTree），失败回退 `proc.Kill()`。

**验证记录**：commandexec 全局关闭单测（fake Process：两会话各 KillTree 恰一次、map 清空、二次调用幂等 + 空注册表 no-op）；新增 Windows 真实进程树测试 `TestKillProcessTreeKillsGrandchildren`（cmd.exe 拉起 ping 子进程 → killProcessTree → 轮询断言孙进程全部消失），本机 Windows 真实通过（0.8s），该文件按 `_windows_test.go` 命名仅在 Windows CI 编译执行。全仓测试绿。

## T10　relay.Manager 导出 Stop

**实现**（2026-08-22）：`Manager.Stop()`——取出并清空内部 `cancel` 后调用（nil-safe、幂等）；`service.Run` 对 ctx 取消的既有 defer 链（`muxSession.Close()` + `conn.Close()`）随即主动关闭 yamux 会话与 WS 长连，中继端立即感知下线而非等 keep-alive 超时，无需新增关闭代码。编排器注册 `relay` 步骤（relayMgr.Start 之后，LIFO 中在 ws-clients 之后、kanban 之前执行——比设计表的"最后关"更早，中继端更早感知，符合 §2.3 意图）。TipsService 仍挂 root ctx 自停。

**验证记录**：新增自包含的 Stop 单测（未启动安全、幂等、真实 cancel 传递、nil receiver），刻意不依赖包内执行顺序（fork-plan 阶段 0.5 已记录该包上游测试的顺序敏感缺陷）；relay 包全量 + 全仓测试绿。"中继端立即感知下线"的实机观察（relay 面板节点状态）并入阶段 2 收尾 checklist。
