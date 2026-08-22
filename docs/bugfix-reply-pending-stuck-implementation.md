# Bug 修复专项实现说明：回复已结束但页面卡在"正在生成"

> 对应需求与设计：[bugfix-reply-pending-stuck.md](./bugfix-reply-pending-stuck.md)　|　实施日期：2026-08-22　|　BT-1 ～ BT-7 全部完成（P0 + P1 + P2）

## 各任务实现与验证

### BT-1　服务端 pending 字段【B-1，P0】

- `http.go` 的 `sessionResponse` / `sessionListResponse` 均补 `"pending"` 字段，数据源 `StreamHub.IsSessionReplying`（与 `/api/replying-sessions` 同源内存态，O(1)）。字段名与前端既有消费口径（`fullSession?.pending`）对齐，前端纠偏逻辑零改动激活。
- 验证：Go 三态测试（从未回复 / 回复中 / 已结束，详情与列表双断言）；端到端（沙箱 + 真实会话）确认 `GET /api/sessions?root=` 与 `GET /api/sessions/{key}` 响应均真实携带 `"pending":false`。

### BT-2　前端 pending 看门狗【B-2，P0】

- 新纯逻辑模块 `web/src/services/pendingWatchdog.ts`（无 import、依赖注入、vm 可测）：本地存在 pending 会话时以 10s 周期对账 `/api/replying-sessions`，不在服务端清单的会话调用 `resolveStuck`；无 pending 时完全停止（零常驻）。**新增宽限期保护**（15s，设计未明确、为守住"正常回复期间不误清"红线补充）：刚发出、可能尚未到达服务端的消息不清。
- App 接线：`pokePendingWatchdog`（惰性建实例）；触发点 = `pendingBySessionRef` 仅有的两个写点（stream 附着草稿、accepted 迁移）。`resolveStuck` 顺序：先清缺口 F 的幽灵 ref（`optimisticDequeuedIdsRef` / `queueFrozenBySessionRef`，否则合成 done 会被队列续发逻辑吞掉）→ `sessionService.resolvePendingLocally`（合成 done 走完整链：全局监听 + per-session onDone 复位 isStreaming + 丢弃过期回放积压，`replay:true` 静音）→ `clearLocalPendingForSession` 幂等兜底强制清。
- 验证：vm 测试 7 块（key 解析、宽限期过滤、无 pending 时 poke no-op、卡住清除后自停、服务端仍回复不误清、网络失败续约、与真实 done 的竞态——fetch 期间本地清空则不动手）。

### BT-3　done 不再被丢弃【B-3，P0】

- `session.ts` 的无订阅者分发：`pendingStreams` 队列元素由纯 stream 事件扩展为 `stream | done | error` 三类；`session.done` / `session.error` 与 stream 同等入队，订阅时按序回放、以终态收尾。回放的 done 走 onDone（不触发提示音——提示音在全局 `session.done` 处理且该场景 done 已在无订阅时被全局处理过一次）。
- 验证：vm 测试（无 handler 时 stream+done 入队、按序回放以 done 收尾、error 同等入队、队列排空、有订阅者时行为不变、done 即使无订阅也清 activeStreams 标志）。

### BT-4　重连后 ready 保证送达【B-4，P1】

- `markSessionReady`：socket 未就绪或发送失败不再静默返回 false，改为登记 `pendingReadySessions` 待发集合；每次 WS `onopen`（连接/重连）与既有 `resendPendingMessages` 一起冲刷补发。
- `App.tsx` 的 `restoreActiveSession`：HTTP 同步失败的提前 return 改为**先发 ready 再返回**——重连恢复链不再因一次 HTTP 失败跳过重绑。
- replay 目标清单（缺口 C）按设计不扩大，由 BT-1/BT-2 对账兜底。
- 验证：vm 测试（离线时 ready 入队去重、连接恢复后冲刷发出全部 ready 帧且队列排空、在线时直发不入队）。

### BT-5　服务端广播健壮性【B-5，P1】

- `ClearSessionPending` 的等待 replay 客户端自旋加 2s 上限（超时打 `clear_pending.replay_wait_timeout` 日志后放行清理）；上限与写超时用包级 var 便于测试收缩。
- `StreamHub.WriteJSON` 每次写前 `SetWriteDeadline`（10s）；任何写失败按死客户端处理——关闭连接，读循环经既有 `UnregisterClient` 清理路径退出。慢/半开连接不再永久霸占 conn 锁与其后的广播循环。
- 验证：Go 测试（卡死 replay 态下 ClearSessionPending ~150ms（收缩值）内返回且状态清空；死连接写失败后同一广播序列的存活客户端仍正常收到消息）。

### BT-6　error 带 session_key【B-6，P2】

- 服务端：`sendWSSessionError` 变体在错误帧 payload 带 `session_key`/`root_id`；替换持有会话上下文的调用点（answer_question_failed、plan_mode_failed ×4、cancel_failed）。无会话上下文的调用点保持原样（payload 空，兼容）。
- 前端：`session.error` 的 `error_without_pending` 分支（请求映射丢失）改为按 payload 里的会话身份 `clearLocalPendingForSession` 复位，不再永久等待。
- 验证：Go 测试用真实 WS 连接断言两种帧的 payload 形态。

### BT-7　E2EE 消息串行处理【B-7，P2】

- `session.ts` 的 `onmessage` 处理抽为 `enqueueIncomingMessage`：以 promise 链按**到达顺序**串行解析+分发（E2EE 异步解密不再乱序完成；明文路径立即 resolve，零开销）。
- 验证：vm 测试模拟"先到的 stream 帧解密慢于后到的 done 帧"，断言处理顺序仍为到达序、done 胜出、streaming 标志为 false。

## 明确不做（按需求文档）

`h.completed` 持久化、多设备 pending 实时同步——均由 B-1/B-2 对账覆盖。

## 回归与遗留

- Go 全量、web 全部测试（含新增 `pending-watchdog.test.mjs`、`session-done-replay.test.mjs` 两文件共 ~20 块断言）、typecheck 全绿；BT-3/BT-4 触碰的 `session.ts` 分发与 ready 两函数有专项 vm 覆盖。
- **待真机手工回归**（需真实 agent 会话才能构造"回复中"状态，无法在沙箱模拟）：① 缺口 A 场景"发消息 → 切走 → 回复结束 → 切回"应即时显示完成态；② 构造卡住态后不刷新等待 ≤15s 观察看门狗自愈（Network 面板应出现周期性 `/api/replying-sessions`，恢复后停止）；③ 正常回复期间观察看门狗不误清；④ E2EE 节点重复回复 50 轮无卡住（B-7 验收）。建议装上本次构建后按需求文档 §6 的取证判据观察一段时间。
