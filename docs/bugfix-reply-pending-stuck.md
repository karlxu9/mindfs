# Bug 修复专项：回复已结束但页面仍显示"正在生成"

> 生成日期：2026-08-22　|　性质：上游既有缺陷（已排除第二轮二开回归，见 §2.3）　|　本文档含需求、设计与任务拆解三部分

## 1. 现象

Agent 回复实际已结束（后端已完成、数据已落盘），但前端页面持续显示"正在生成"（或"已发送，等待中"）；**刷新页面后立即显示为已完成**。用户报告为高频发生。

## 2. 根因分析

### 2.1 结构性根因（为什么"刷新就好"）

前端的"回复中"状态由两个变量决定：`session.pending`（App 层，控制列表圆点与"等待中"）与 `isStreaming`（`useSessionStream.ts:463`，控制"正在生成"文案）。两者都是**纯浏览器内存镜像，唯一的复位途径是 WS 推送的 `session.done` 事件**：

- 服务端**任何 HTTP 接口都不返回 pending 字段**——`sessionResponse` / `sessionListResponse`（`api/http.go:1166-1239`）的字段清单里没有它。讽刺的是，前端消费代码**已经写好了**（`App.tsx:3536-3546`：`serverPending === false` 则清本地 pending），只是永远收到 `undefined`、永远不生效。
- 浏览器端**没有任何轮询/看门狗兜底**。`replyPoller.ts` 是 Capacitor 原生桥（只服务安卓系统通知，不回写 Web UI）；`connection.ts` 是零引用死代码；`/api/replying-sessions` 仅在默认关闭的多项目开关下被动调用一次。

因此 `session.done` 这一条消息在任一环节丢失，UI 就**永久卡住**；刷新清空内存态（pending 默认 false），所以看起来"好了"。

### 2.2 done 会在哪里丢（九个已定位的缺口）

按可能性排序，代码位置为准确锚点：

| # | 缺口 | 机制 | 关键位置 |
|---|---|---|---|
| A ★★★ | **无订阅者时 done 被静默丢弃** | 会话不在前台时 `session.stream` 会进 `pendingStreams` 队列等重新订阅时回放，但 `session.done` 在无 handler 时**直接 return 丢弃**；切回会话后回放的 stream 事件把 `isStreaming` 顶回 true，且再也不会有 done | `services/session.ts:693-702`（丢弃）、`:808-814`（回放）、`useSessionStream.ts:504-506`（任意事件置 true） |
| B ★★★ | **重连后 `session.ready` 未重发 → 服务端解绑** | WS 断开时服务端把该 client 从 `sessionClients` 全部摘除；重绑唯一入口是 `session.ready`，但重连恢复链上 HTTP 同步失败会提前 return 跳过 `markSessionReady`，且 `markSessionReady` 在 socket 未 OPEN 时静默失败、无重试 | `stream_hub.go:295-316`、`App.tsx:3507-3509,3553`、`services/session.ts:1051-1074` |
| C ★★★ | **重连 replay 目标覆盖不全** | 重连后只对"当前选中/绑定/抽屉"会话补 ready；子会话（subagent）、看板任务会话、非当前 root 的会话不在清单内，done 落在断线窗口即永久丢失 | `App.tsx:9078-9135` |
| D ★★ | **E2EE 模式下消息乱序** | 每条 WS 消息独立异步解密，完成顺序不保证等于到达顺序；done 先被处理后，晚到的 stream 事件把 pending/isStreaming 重新置位 | `services/session.ts:433-447,723-732`、`App.tsx:9340-9343` |
| E ★★ | **服务端 `ClearSessionPending` 自旋无上限 + WS 写无超时** | done 广播前先自旋等 replay 客户端清空（10ms 循环、无上限）；而回放写路径 `WriteJSON` 无写超时，半开连接（手机休眠/中继抖动）可阻塞数分钟——期间 done 对**所有**客户端都不发 | `stream_hub.go:807-813,936-962`、`appcontext.go:1069-1070` |
| F ★★ | **队列幽灵态：done 到了也被改回 pending** | done 处理器里若判定"还有排队的后续消息"会 early-return 并重新 `markSessionPending`；而队列镜像 ref 依赖 `session.queue.updated` 广播清理，该广播一旦丢失，**此后每一次 done 都被吞掉**——这是唯一能解释"每次都卡"的机制 | `App.tsx:9170-9179,9927-9941` |
| G ★ | 多标签/多设备各自绑定，未发过 ready 的标签收不到 done | `stream_hub.go:335-356` |
| H ★ | `session.error` 报文无 session_key，无法复位 pending | `ws.go:1121-1132`、`services/session.ts:676` |
| I ★ | 回复期间服务重启：`h.completed` 纯内存，重启后 ready 补发拿不到 done | `stream_hub.go:30,861-864` |

### 2.3 回归排查结论

第二轮二开（`a949825..b6653be`）对上述链路的全部改动仅为：`stream_hub.go` 新增关机专用 `CloseAllClients`、`ws.go` 错误广播多传一个参数、`App.tsx` ErrorBoundary 包裹、`session.ts` 尾部新增 Markdown 导出——**没有触碰 done 的构造、广播、订阅、消费或 pending 复位的任何一行，判定为上游既有问题**。

两个 fork 侧**放大因素**（非引入因素）需要知晓：① 第二轮的优雅退出让 `-stop`/更新会主动踢掉全部 WS，每次重启都制造一次"断线窗口 + completed 内存丢失"（缺口 B/I 的触发机会变多）；② ErrorBoundary 接管 SessionViewer 崩溃后，其卸载会使后续 done 落入缺口 A。

上游历史也佐证此路径长期脆弱：`1206dcf`（修"手机后台太久卡 pending"）、`d36cc53`（重连 seq 同步，但未覆盖"ready 没发出去"的情况）。

## 3. 修复需求（PRD 部分）

**产品策略：兜底自愈优先，高概率缺口对症修复次之。** 九个缺口逐一修根源既不现实（部分在上游深水区，如 E2EE 乱序），也不可靠（未来上游改动还会制造新的丢失路径）。单条推送消息丢失是常态，**必须有对账机制让状态最终收敛**——这是治本；之后再按概率修高发缺口降低"需要兜底"的频率。

| 编号 | 需求 | 优先级 | 验收标准 |
|---|---|---|---|
| B-1 | **服务端返回 pending 事实**：会话详情与列表响应补充 pending（回复中）字段，数据源为 StreamHub 内存态（与 `/api/replying-sessions` 同源） | P0 | 回复中/结束后分别请求 `GET /api/sessions/{key}`，字段值正确；前端既有纠偏逻辑（`App.tsx:3536-3546`）被激活——会话数据任何一次刷新都能纠正卡住的 pending |
| B-2 | **前端 pending 看门狗**：本地存在 pending 会话期间，以 10 秒周期调用 `/api/replying-sessions` 对账；不在服务端清单中的会话清除 pending 与 isStreaming（并同步清理缺口 F 的队列幽灵 ref）；无 pending 会话时看门狗完全停止（零常驻开销） | P0 | 人为构造缺口 A/B/F/I 任一场景，UI 在 ≤15 秒内自动恢复正确状态，无需刷新；无 pending 时 Network 面板无周期请求 |
| B-3 | **done 不再被丢弃**：无订阅者时 `session.done` 与 stream 事件同等入队回放（或等效机制：回放队列中存在 done 时，重订阅后以完成态收尾） | P0 | "发消息 → 切走 → 回复结束 → 切回"场景下，切回瞬间即显示完成态，不再显示"正在生成" |
| B-4 | **重连后 ready 保证送达**：`markSessionReady` 失败不再静默——socket 未就绪时挂起待重发，重连成功后对全部 replay 目标强制补发；HTTP 同步失败不再跳过 ready | P1 | 断网 10 秒恢复后（回复仍在进行），WS 帧中可见新连接上的 `session.ready`，其后 stream 与 done 正常到达 |
| B-5 | **服务端广播健壮性**：`ClearSessionPending` 自旋加上限；WS 数据写路径加写超时，慢/半开连接不再阻塞对其他客户端的 done 广播 | P1 | 模拟一个不读数据的客户端连接，其他客户端仍在正常时限内收到 done；自旋上限触发有日志 |
| B-6 | `session.error` 报文补 session_key，错误路径可复位 pending | P2 | 构造发送失败，UI 从"回复中"转为错误态而非永久等待 |
| B-7 | E2EE 模式下 WS 消息按到达顺序串行处理（或按 seq 排序），消除解密乱序 | P2 | E2EE 节点上重复 50 次回复无一卡住 |

**明确不做**：`h.completed` 持久化（服务重启场景已被 B-1/B-2 对账覆盖，为它引入存储不值得）；多设备 pending 实时同步（同样被对账覆盖）。

## 4. 设计要点（设计文档部分）

- **B-1**：字段挂在 `sessionResponse` / `sessionListResponse`（`http.go:1166-1239`）构造处，从 `StreamHub.ListReplyingSessions` 同源的内存态读取（`stream_hub.go:749-767`），O(1) 查询、不碰磁盘。字段命名与前端已有的 `fullSession?.pending` 消费口径对齐（`App.tsx:3536`），**前端理论上零改动即激活**；实测若列表路径未消费，再做最小接线。
- **B-2**：看门狗落在新的纯逻辑模块（建议 `web/src/services/pendingWatchdog.ts`，无 import 纯模块，沿用 vm 沙箱测试方式），App 侧只做启停接线。触发条件 = `pendingBySessionRef` 非空；对账动作复用既有 `handleSessionStreamDone` 的清理链（`App.tsx:9149-9231`），并在对账确认完成时一并清 `optimisticDequeuedIdsRef` / `queueFrozenBySessionRef`（缺口 F）。注意对账结果与 done 事件竞态：以服务端清单为准，幂等清理。
- **B-3**：改 `services/session.ts:693-702` 的分发规则——done 与 stream 同等入队 `pendingStreams`；回放侧（`:808-814`）保持顺序回放即可自然收尾。注意回放的 done 不应再次触发提示音（沿用 `replay: true` 的既有静音口径，`App.tsx:10067` 附近）。
- **B-4**：`markSessionReady`（`session.ts:1051-1074`）失败改为登记到待发集合，`ws.connected/reconnected` 时统一冲刷；`restoreActiveSession` 的提前 return（`App.tsx:3507-3509`）改为"HTTP 失败也要发 ready"。replay 目标清单（缺口 C）本轮不扩大——子会话/任务会话由 B-1/B-2 对账兜底，扩大清单会放大重连风暴。
- **B-5**：自旋上限建议 2s（超时打日志放行）；写超时用 `SetWriteDeadline` 口径，超时按客户端断开处理（触发既有 `UnregisterClient` 清理）。
- **上游冲突面**：B-1/B-5 为服务端小改（各 <30 行）；B-2 为新模块 + 接线；B-3/B-4 触碰 `session.ts` 上游活跃区,改动收敛在分发与 ready 两个函数内。合并上游时若上游自行修复同类问题（关注 `pending`/`replay` 相关提交），以上游为准弃用对应项。

## 5. 任务拆解（开发计划部分）

| 任务 | 需求 | 前置 | DoD 与测试要点 |
|---|---|---|---|
| BT-1 服务端 pending 字段 | B-1 | 无 | 详情/列表两响应含字段；Go 测试覆盖回复中/已完成/从未回复三态；前端纠偏路径手工验证一次（卡住态 → 切换会话再切回 → 自动恢复） |
| BT-2 前端看门狗模块 + 接线 | B-2 | BT-1 可并行 | vm 沙箱测试：启停条件、对账清理幂等、与 done 事件竞态；手工构造缺口 A/F/I 三场景各验证 ≤15s 自愈 |
| BT-3 done 入队回放 | B-3 | 无 | vm 沙箱测试：无 handler 时 done 入队、回放顺序、回放 done 静音；手工验证"切走再切回"场景 |
| BT-4 ready 补发 | B-4 | 无 | 待发集合冲刷测试；断网恢复场景手工验证（WS 帧取证 ready 重发） |
| BT-5 服务端自旋上限 + 写超时 | B-5 | 无 | Go 测试：慢客户端不阻塞其他客户端收 done；自旋超时日志断言 |
| BT-6 error 带 session_key | B-6 (P2) | 无 | 发送失败场景 UI 转错误态 |
| BT-7 E2EE 串行处理 | B-7 (P2) | 无 | E2EE 开启下压测 50 轮无卡住 |

**实施顺序**：BT-1 → BT-2（兜底先行，上线即"永久卡住"绝迹）→ BT-3（最高概率缺口）→ BT-4 / BT-5 → P2 视余量。估算：P0 约 1.5 人日，P1 约 1 人日。

**回归红线**：BT-3/BT-4 触碰消息分发与 IME 无关，但需跑一遍现有 web 测试全量；BT-2 看门狗必须验证"正常回复期间不误清"（回复中会话始终在 `/api/replying-sessions` 清单内，不会被清）。

## 6. 复现取证判据（测试与用户自查用）

| 观察点 | 指向 |
|---|---|
| 完成提示音响过 + 会话列表小圆点已消失，但正文仍"正在生成" | **缺口 A 铁证**（done 已到、isStreaming 卡住） |
| 提示音不响、正文停在半截 | done 没到达（缺口 B/C/E/I），看 WS 帧有无重连后的 `session.ready` |
| 用过消息排队/立即发送功能后开始"每次都卡" | 缺口 F |
| Console 出现 `[session/ws] error_without_pending` | 缺口 H |
| 卡住时 `GET /api/replying-sessions` 仍返回该会话 | 服务端侧未清（缺口 E）；返回空则纯前端问题 |
