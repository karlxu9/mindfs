# 会话环「左滑新建会话」优化 —— 新建会话入口重做

> 立项日期：2026-08-28　|　性质：交互优化（前端专项，零后端改动）　|　优先级：P1　|　预估：0.5–1 天
>
> 开发文档：[ui-new-session-entry-devplan.md](./ui-new-session-entry-devplan.md)（2026-08-28 修订本文档两处：移动端收起的是会话列表所在**右侧栏**而非左侧栏；「新建后聚焦输入框」取消，与原手势行为对齐、避免移动端强制弹键盘）

## 1. 背景

用户反馈：输入框右侧会话环的「左滑新建会话」手势体验差（原话："有点神经"）。

经代码盘点，问题不只是手势本身别扭，而是**入口设计错位**：「新建会话」是高频显式动作，目前在聊天界面里的唯一入口却是一个隐藏拖拽手势，且误触代价不小。本专项将新建会话改为显式按钮，左滑手势整体下线。

## 2. 现状盘点（代码事实）

### 2.1 交互全貌

会话环是输入框右侧一个 32×32 触点、14px 视觉的圆环（绑定会话时着色 + 光晕），身上压了两个语义：

| 手势 | 判定 | 行为 |
|---|---|---|
| 点击 | 位移 < 5px（`ActionBar.tsx:1851-1855`） | 开/关当前绑定会话的抽屉 |
| 左拖 ≥ 40px 松手 | `DRAG_THRESHOLD = -40`（`ActionBar.tsx:490,1237-1245`） | 解绑当前会话 = 新建会话 |

配套教学：拖动 >10px 时环左侧出现 10px 小字提示（`ActionBar.tsx:1918-1922`）；绑定会话且非 pending 时输入框 placeholder 常驻「左滑蓝环开始新会话...」（`ActionBar.tsx:1287-1288`）；onboarding 引导有专门一步（`OnboardingTour.tsx:26`）。

### 2.2 关键事实

1. **唯一入口**：`SessionList.tsx` 没有新建按钮（全文无 new-session 相关命中）；`session.new` 文案只用作未命名会话的显示名。聊天界面里除左滑外没有任何显式的「新建会话」入口。
2. **误触代价不对称**：触发即执行、无确认无撤销。`resetForNewSession`（`ActionBar.tsx:1215-1229`）会把 model/effort/agentMode 重置为默认；`App.tsx:6976-6978` 还会把原会话记入 `suppressedAutoBindSessionByRootRef`——**误触后不会自动回绑，必须去会话列表手动找回原会话**。
3. **点/拖挤在同一 32px 小目标**：<5px 算点击、5–40px 是无效区、≥40px 触发新建。手机上想点环开抽屉，手指稍滑即落入无效区；想左滑又可能被判点击。
4. **手势吞滚动**：环上 `touchAction: "none"`（`ActionBar.tsx:1868`），从环起手的页面滚动被吞。
5. **桌面端同样生效**：鼠标按住拖 40px 触发（`ActionBar.tsx:1849`），鼠标拖拽做「新建」不符合任何桌面惯例。
6. **无测试依赖**：`web/tests/` 下无 drag/swipe/session-ring 相关断言，下线手势不动测试资产（onboarding 文案除外，见 §4.4）。

## 3. 目标与非目标

**目标**：新建会话有显式、可发现、单手可达的入口；会话环回归单一语义（点击开关抽屉）；误触路径消除。

**非目标**：不改「新建会话 = 解绑当前会话」的语义与 `handleNewSession`（`App.tsx:6973-6996`）逻辑本身；不动会话抽屉交互；不做新建确认弹窗（入口显式化之后不存在误触，无需确认）。

## 4. 方案

### 4.1 N-1 环旁「＋」按钮（P0，一步直达入口）

- 位置：会话环**左侧**紧邻处新增「＋」图标按钮，与环同尺寸（32×32 触点，图标视觉 ≤16px，次级颜色，hover/active 用 accent）。放左侧的理由：右侧紧贴 ModeSelector/AgentSelector（`ActionBar.tsx:1926` 起），左侧朝向输入区留白，且位置上接近原手势的「拖出方向」，肌肉记忆迁移自然。
- 显示条件：`hasBoundSession === true` 时显示（含 pending 中——会话后台继续跑、并发开新会话是既有合法场景，与原手势行为一致）；未绑定时隐藏（此时本来就是新会话状态）。
- 行为：点击 = 原 `handleDragEnd` 达阈值分支的完整逻辑，即 `resetForNewSession()` + `onNewSession?.()`（`ActionBar.tsx:1239-1242`），不得只调其一。
- 无障碍：`aria-label` / `title` 用新增文案 `action.newSession`（「新建会话」/ "New session"）。
- **布局风险与退路**：多行输入时 `editorRightInset`（`ActionBar.tsx:1292`）需要重新核算；若移动端实测输入可视区被挤压到不可接受（±32px），退路是把「＋」放进会话抽屉头部（点环开抽屉 → 头部第一项「＋ 新会话」），两步但零空间成本。由研发在真机上定夺，PRD 不锁死。

### 4.2 N-2 会话列表头部「＋」按钮（P0，通用心智入口）

- `SessionList.tsx` 头部工具区（搜索/分组按钮同排）新增「＋」按钮，点击 = `handleNewSession`；移动端点击后同时收起会话列表所在侧栏（接线照抄 `onSelect` 的收栏模式，`App.tsx:14167-14170`），不自动聚焦输入框。
- 这是所有主流会话类产品的标准位，补上它之后「新建会话」在任何状态下都有稳定入口，不再依赖绑定态的环。
- 多项目视图（`MultiProjectSessionList`）本轮不加，避免「新建到哪个项目」的歧义；单项目列表先行。

### 4.3 N-3 左滑手势整体下线（P0）

删除清单（全部在 `ActionBar.tsx`）：

- `DRAG_THRESHOLD`（:490）、`dragStartRef` / `isDragging` / `dragX` 相关 state；
- `handleDragStart`（:1231-1235）、`handleDragEnd`（:1237-1245）、拖拽监听 effect（:1247-1263）；
- 环上的 `onMouseDown` / `onTouchStart`（:1849-1850）、`touchAction: "none"`（:1868）、`transform: translateX(dragX)`（:1860）；
- 拖动提示浮层（:1918-1922）；
- 点击的 `Math.abs(dragX) < 5` 防抖 guard（:1852）简化为直接 `onSessionClick?.()`。

注意：`resetForNewSession` **保留**（N-1 复用）；环的点击开关抽屉逻辑（`App.tsx:14647-14666`）**一行不动**。

### 4.4 N-4 文案与 onboarding 同步（P0，随 N-3）

| 项 | 处理 |
|---|---|
| `action.placeholder.newSessionSwipe`（zh:639 / en 对应） | 删除；`ActionBar.tsx:1287-1291` 的 placeholder 分支改为回落到常规 mode placeholder |
| `action.swipeNewSession`（zh:678）、`action.releaseNewSession` | 删除（zh + en） |
| `action.newSession` | 新增（zh:「新建会话」/ en: "New session"），N-1/N-2 共用 |
| `onboarding.sessionRing.body`（zh:771 / en:773） | 改写：「点击圆环可以展开或收起当前绑定会话；点击旁边的＋按钮可以开始一个新会话。」（en 同步）；title 不变 |

## 5. 被否决的备选

| 备选 | 否决理由 |
|---|---|
| 保留手势、加固防误触（提高阈值/意图判定/触发后可撤销） | 治标。可发现性问题原样保留，且从此维护两套入口心智；用户对手势的否定是定性的（"神经"），不是参数问题 |
| 长按环 → 新建 | 用一个隐藏手势换另一个隐藏手势 |
| 触发前确认弹窗 | 给高频动作加摩擦；入口显式化后误触不存在，确认无意义 |

## 6. 验收标准

1. 绑定会话时（含 pending 中）环旁显示「＋」，点击后：解绑当前会话、回到新会话主视图、原会话不自动回绑（与原左滑行为逐项一致）；未绑定时「＋」不渲染。
2. 会话列表头部「＋」在单项目视图可见，点击开新会话；移动端同时收起会话列表侧栏（不强制聚焦输入框）。
3. 对环做任意方向、任意距离的拖动：环无位移跟随、不触发新建；拖后点击开关抽屉行为正常。
4. placeholder 在「绑定会话且非 pending」态显示常规 mode 占位文案，不再出现「左滑蓝环」教学。
5. onboarding 走查 session-ring 步骤，文案与实际交互一致，无「拖动」表述（zh/en 双语）。
6. 全部 6 套主题 × 桌面/移动宽度下「＋」按钮可见、对比度达标（沿用 graphite 专项的 AA 标准）；多行输入态不与发送区重叠。
7. `make typecheck` + `make test-web` 通过；i18n 两个 locale 无残留死 key（grep `swipeNewSession|releaseNewSession|newSessionSwipe` 零命中）。

## 7. 测试要点

- 回归重点：环点击开关抽屉（`App.tsx:14647-14666` 三分支：绑定会话在主视图/抽屉已开/抽屉未开）；pending 中开新会话后原会话后台继续产出、通知正常。
- 真机项：iOS Safari 与 Android Chrome 各一轮——「＋」单手可达性、从环/按钮位置起手的页面滚动不再被吞（原 `touchAction:none` 副作用消除的验证）。
- 研发确认项：①新建会话后输入框草稿是否保留（现状 `resetForNewSession` 不清草稿，确认这是期望行为并保持）；②`data-onboarding="session-ring"` 锚点若移到按钮组外层容器，onboarding 高亮框位置需复核。

## 8. 风险

| 风险 | 应对 |
|---|---|
| ActionBar 是第一轮认定的 IME 雷区 | 本次改动不触碰键盘/composition 逻辑，仅删拖拽与增按钮；改后跑讯飞/搜狗回车回归 |
| 移动端右下角控件密度上升 | N-1 有抽屉头退路（§4.1）；真机定夺 |
| 老用户肌肉记忆 | 环位置/点击语义不变；「＋」就在原拖出方向上，成本可忽略 |
