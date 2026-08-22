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
