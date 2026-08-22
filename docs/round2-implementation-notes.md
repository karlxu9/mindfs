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
