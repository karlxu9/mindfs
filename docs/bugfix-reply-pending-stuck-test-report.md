# Bug 修复专项测试报告：回复已结束但页面卡在"正在生成"

> 测试日期：2026-08-22　|　被测版本：main @ b6eda49（服务端 `0262840` + 前端 `b6eda49`，BT-1～7 全部实施）　|　测试环境：Windows 11（win32/amd64）、Go 1.26.5、Node 24.14
>
> 依据：[bugfix-reply-pending-stuck.md](./bugfix-reply-pending-stuck.md) §3 验收标准、§5 任务 DoD、[实现说明](./bugfix-reply-pending-stuck-implementation.md)

## 0. 结论速览

- **B-1 ～ B-7 自动化可覆盖范围全部通过，不通过 0 项**；基线回归无损（Go 全仓无缓存重跑绿、web 16/16 绿、typecheck 绿）。
- **CI run #13（head `b6eda49`）双平台全绿**（go test ubuntu/windows + web），由测试方经 GitHub API 亲自确认。
- 修复文档 §3 各验收标准中依赖"真实 agent 回复中"状态的场景（缺口 A/B/F/I 的活体构造、≤15s 自愈观察、E2EE 50 轮压测）**无法在沙箱模拟**（与研发结论一致），列入真机手工清单（§4），建议随日常使用按需求文档 §6 取证判据观察。

## 1. 基线回归（全部真实执行）

| 项 | 结果 |
|---|---|
| `go test -count=1 ./...`（全仓无缓存） | 全绿 |
| web 测试全量（回归红线要求） | **16/16 绿**（14 既有 + 2 新增） |
| `tsc --noEmit` | 绿 |
| CI run #13 @ `b6eda49` | 三 job 全绿（自查确认，非转述） |

## 2. 逐条验收结论

| 编号 | 结论 | 依据（均为本次真实执行/实测） |
|---|---|---|
| B-1 服务端返回 pending 事实 | **通过** | ① `TestSessionResponsesCarryPendingFlag` 严格对应 DoD：从未回复 false → `SetPendingReply` 后 true → `ClearSessionPending` 后 false，详情/列表双断言，真实执行通过；② 沙箱黑盒实测（HEAD 重建二进制）：`GET /api/sessions?root=` 列表逐项、`GET /api/sessions/{key}` 详情均真实携带 `"pending": false`（修复前该字段完全不存在，根因 §2.1）；`/api/replying-sessions` 返回 200 空清单。**生产实例上线后补充活体验证（2026-08-22 22:35）**：真实回复中的会话在列表响应中 `pending=true`、其余会话 `false`——三态中沙箱无法构造的"回复中"态在生产上实测正确。"前端纠偏激活后任意刷新可纠正卡住"的活体确认列手工项 |
| B-2 前端看门狗 | **通过（自动化范围）** | `pending-watchdog.test.mjs`（19 断言）真实执行：key 解析、**宽限期过滤**（15s 新增保护，防误清刚发出的消息）、无 pending 时 poke 零调度、卡住会话 resolve 后自停、**服务端仍在回复不误清（回归红线）**、fetch 失败续约、**与真实 done 竞态**（fetch 期间本地清空则不动手）。缺口 A/B/F/I 活体 ≤15s 自愈观察列手工项 |
| B-3 done 不再被丢弃 | **通过（自动化范围）** | `session-done-replay.test.mjs`（21 断言）真实执行：无订阅者时 done 与 stream 同等入队、按到达序回放并以终态收尾、error 同等入队、有订阅者行为不变（无回归）、无订阅时 done 仍清 active-stream 标志、合成 done 走 `replay:true` 静音口径。"切走→回复结束→切回"活体场景列手工项 |
| B-4 ready 保证送达 | **通过（自动化范围）** | 同文件 vm 块：离线时 `markSessionReady` 入队去重、连接恢复冲刷发出全部 ready 帧且队列排空、在线直发不入队。断网 10s 恢复后 WS 帧取证列手工项 |
| B-5 服务端广播健壮性 | **通过** | `TestClearSessionPendingReplayWaitIsBounded`：卡死 replay 态下 0.16s（收缩上限）返回且状态清空（原为无上限自旋）；`TestWriteFailureDoesNotBlockOtherClients`：真实 WS 死连接写失败后，同一广播序列的存活客户端 2.01s 内正常收到消息。两测试均本机真实执行通过 |
| B-6 error 带 session_key | **通过（自动化范围）** | `TestSendWSSessionErrorCarriesSessionIdentity`：真实 WS 连接断言带/不带会话身份两种帧形态。**标注**：前端 `error_without_pending` 分支按帧内身份复位 pending 的改动未见专项自动化测试（源码核查已落地），"构造发送失败 → UI 转错误态"列手工项 |
| B-7 E2EE 串行处理 | **通过（自动化范围）** | vm 块：模拟"先到 stream 帧解密慢于后到 done 帧"，断言处理仍按到达序、done 胜出、streaming=false。E2EE 节点 50 轮压测列手工项 |

## 3. 测试执行说明

- 服务端 4 个新测试（`0262840`）逐一显式 `-run` 执行确认（避免被包级汇总掩盖）；前端 2 个新测试文件单独执行 + 全量套件双确认。
- B-1 黑盒验证使用 `verify-round2/` 沙箱（隔离 APPDATA/USERPROFILE，二进制重建至 `b6eda49`），验证后优雅停止、无残留进程。
- "回复中"（pending=true）状态需真实 agent 会话才能在 HTTP 层构造，沙箱无 agent 凭据，该态的正确性以 Go 单测（直接操纵 StreamHub 内存态，与生产同一数据源）为据——这与 `/api/replying-sessions` 的既有可信链一致。

## 4. 真机手工回归清单（合并研发清单，无法自动化项）

1. 缺口 A 场景：发消息 → 切走 → 回复结束 → 切回，应即时显示完成态；
2. 构造卡住态后不刷新，≤15s 内看门狗自愈（Network 面板出现周期性 `/api/replying-sessions`，恢复后停止请求）；
3. 正常回复期间观察看门狗不误清（回复中会话始终在服务端清单内）;
4. 发送失败场景：UI 从"回复中"转错误态而非永久等待（B-6 前端分支）；
5. E2EE 节点重复回复 50 轮无卡住（B-7 验收原文）；
6. 断网 10s 恢复（回复进行中）：WS 帧可见新连接上的 `session.ready` 补发（B-4 验收原文）。

以上均可随日常使用观察，取证判据见需求文档 §6（提示音/圆点/正文三信号组合可直接定位缺口归属）。

## 5. 观察项

1. **【无缺陷】宽限期 15s 为设计未明确的新增保护**（研发已在实现说明声明）：刚发出、可能尚未到达服务端的消息不参与对账清理——这是守住"不误清"红线的正确取舍，vm 测试已覆盖其边界（无 `startedAt` 的条目立即可清）。
2. **【提示】B-6 前端复位分支缺专项自动化**（见 §2 表），风险低（改动 3 行内、路径清晰），建议下轮顺手补一条 vm 断言。
3. 本次修复未触碰第二轮验收过的任何路径的行为语义（回归红线的 web 全量 16/16 与 Go 全仓可证），第二轮测试报告结论不受影响。

## 6. 上线记录（2026-08-22 22:28）

- 生产实例（127.0.0.1:7331）经计划任务完成二进制与前端产物切换（`restart-mindfs.bat`，日志 `restart-log.txt`），13 秒完成，health ok；旧二进制备份为 `mindfs-old-backup.exe`，配置备份在 `%APPDATA%\mindfs\backup-pre-bugfix\`。
- **切换前取证**：旧实例 `/api/replying-sessions` 存在 4 个停滞数小时的"回复中"会话——本 bug 在服务端侧的直接证据；切换后清单只剩真实活跃会话，pending 字段三态在生产上全部实测正确。
- 本次停止是旧版本最后一次 taskkill 强杀；自此所有停止/重启路径均走第二轮的优雅退出链。
