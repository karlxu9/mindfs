# 开发文档 —— 会话环左滑手势下线与新建会话入口重做

> 生成日期：2026-08-28　|　对应需求：[ui-new-session-entry.md](./ui-new-session-entry.md)　|　纯前端，零后端改动
>
> 本文档粒度：每个任务可直接分派，含实施要点（引用现有代码位置作依据，不含实现）、完成定义（DoD）与测试用例。

## 0. 对 PRD 的修订（已同步回 PRD）

1. **N-2 移动端收栏对象**：会话列表位于**右侧栏**（`App.tsx:14167-14170` 的 `onSelect` 在移动端调 `setIsRightOpen(false)`；左右栏可由用户交换，但 state 语义不变）。PRD 原文「关闭左侧栏」有误，接线一律照抄 `onSelect` 的既有模式。
2. **取消「新建后聚焦输入框」**：原左滑手势触发后并不聚焦（`ActionBar.tsx:1237-1245` 无 focus 调用）；移动端程序化聚焦会强制弹出键盘，反而打扰。本轮不做，验收标准 2 已相应修订。ActionBar 内部已有 `editorRef.focus()` 机制（`ActionBar.tsx:929` 等），未来若要做，通道是现成的，不留技术债。

## 1. 改动总览

| 文件 | 改动性质 | 任务 |
|---|---|---|
| `web/src/components/ActionBar.tsx` | 删拖拽手势、简化环点击、新增「＋」按钮、inset 核算、onboarding 锚点外移 | T1、T2 |
| `web/src/components/SessionList.tsx` | 头部工具区新增「＋」按钮 + 新 prop | T3 |
| `web/src/App.tsx` | SessionList 接线一个 prop（复用 `handleNewSession`） | T3 |
| `web/src/i18n/locales/zh-CN.ts`、`en-US.ts` | 删 3 组 key、增 1 组 key、改 onboarding 文案 | T4 |
| `web/src/components/OnboardingTour.tsx` | 不改代码，仅验证锚点高亮（锚点属性在 ActionBar 侧移动） | T2 |

不改动：后端全部；`App.tsx` 的 `handleNewSession`（:6973-6996）与环点击的抽屉三分支（:14647-14666）；`resetForNewSession`（`ActionBar.tsx:1215-1229`，T2 复用）；会话数据模型与任何接口。

## 2. 任务拆解

### T1　左滑手势下线【0.5h】

**范围**：`ActionBar.tsx` 单文件，净删除。

**实施要点**（删除清单，全部先于 T2 完成）：

1. 常量与状态：`DRAG_THRESHOLD`（:490）及 `dragStartRef` / `isDragging` / `dragX` 三个拖拽 state（声明位置在 :490 附近，随引用一并清）。
2. 处理器与副作用：`handleDragStart`（:1231-1235）、`handleDragEnd`（:1237-1245）、window 级拖拽监听 effect（:1247-1263）。**注意**：`handleDragEnd` 达阈值分支里的 `resetForNewSession() + onNewSession?.()` 组合是 T2 按钮点击的行为规格，删除前在 T2 任务中登记，两者必须同提交。
3. 环元素（:1846-1923 区域）：`onMouseDown` / `onTouchStart`（:1849-1850）、`touchAction: "none"`（:1868）、`transform: translateX(dragX)` 与 `isDragging` 相关的 transition 分支（:1860-1861）、拖动文字提示浮层（:1918-1922）。
4. 点击 guard 简化：`Math.abs(dragX) < 5` 判定（:1852）删除，`onClick` 直接调 `onSessionClick?.()`。
5. placeholder 分支：`inputPlaceholder` 三元的第一分支「绑定且非 pending → newSessionSwipe 文案」（:1287-1288）删除，回落到既有的 mode/blur 占位逻辑（:1289-1291）。

**DoD**：
- 全文件 grep `drag|Drag|swipe|Swipe` 零业务命中（`handleDragStart` 等标识符全部消失）；
- 环点击开关抽屉行为不变（用例 C-1）；对环做任何拖动无位移、无新建（C-2）；
- 绑定非 pending 态 placeholder 显示常规 mode 占位文案（C-8）；
- `make typecheck` 通过（i18n key 引用此时已断，故 T1 与 T4 的 key 删除须同一提交，见 §3）。

### T2　环旁「＋」新建按钮【1.5h，依赖 T1】

**范围**：`ActionBar.tsx`。

**实施要点**：

1. **位置与结构**：在输入控件容器（:1846，`data-onboarding="input-controls"`）内、会话环 div（:1847）**之前**插入按钮；用一个新包裹容器把「＋」与环包起来，并把 `data-onboarding="session-ring"` 属性从环 div（:1848）**移到该包裹容器**上，使 onboarding 高亮同时覆盖两者。
2. **显隐**：`hasBoundSession`（:1267）为 true 时渲染（含 pending 中，与原手势一致，不加 `sending` 禁用）；false 时不渲染（不是隐藏，避免占位）。
3. **行为**：点击 = `resetForNewSession()` 后 `onNewSession?.()`，两个调用缺一不可、顺序不可颠倒（规格来源：原 :1239-1242）。
4. **视觉**：32×32 触点、加号图标视觉 ≤16px（线性风格对齐 SessionList 头部按钮的 svg 规范：stroke 2 / round cap）；idle 色 `var(--text-secondary)`、hover/active 色 `var(--accent-color)`；`aria-label` 与 `title` 用新 key `action.newSession`（T4）。
5. **inset 核算**：`editorRightInset`（:1292）现值 `isMultiLine ? 14 : command ? (mobile 92 : 116) : (mobile 124 : 148)`。「＋」渲染时非 multiline 各值 +32（multiline 态控件沉底行不占右缘，14 不变）；表达式需增加 `hasBoundSession` 条件。**command 模式同样有绑定会话（长期 shell），同样 +32**。
6. **布局退路**（PRD §4.1）：改完后移动端真机（≤768px）核对 chat 模式输入可视区宽度；若不可接受，「＋」改落会话抽屉头部，并回写 PRD。此决策由研发实测后定，本文档默认环旁方案。

**DoD**：
- 绑定/未绑定/pending 三态显隐正确（C-3、C-4、C-5）；
- 点击后与原左滑逐项等价：解绑、回主视图新会话态、agent 保留而 model/effort 回默认、原会话不自动回绑（C-3）；
- chat/command × 移动/桌面 × 单行/多行 六种组合无控件重叠（C-6、C-7）；
- onboarding session-ring 步骤高亮框覆盖「＋」+ 环（C-11）；
- 6 套主题下按钮可见、对比度沿用 graphite 专项 AA 标准（C-13）。

### T3　会话列表头部「＋」按钮【1h，可与 T1/T2 并行】

**范围**：`SessionList.tsx` + `App.tsx` 接线。

**实施要点**：

1. **位置**：头部 36px 工具栏左侧按钮组（:743-839，现有顺序：搜索 → 按 Agent 分组 → 批量选择）**最前**新增「＋」；样式完全照抄现有按钮规格（34×34、圆角 8、透明底、`--text-secondary`/hover accent、`transition 0.15s`，:750-764 即模板）。非开关按钮，**不加** `aria-pressed`。
2. **仅搜索常态显示**：`searchResultsMode` 为 true 时头部整体切换为返回键（:733-741），该分支不受影响；搜索输入展开态下按钮组是否仍在，按现状跟随搜索/分组按钮的既有显隐，不做特殊处理。
3. **Props**：`SessionList` 增加可选 prop `onNewSession?: () => void`（props 类型声明在 :41/:100 两处同步）；未传时按钮不渲染——`MultiProjectSessionList` 不传即天然不出现（PRD 非目标）。
4. **App 接线**：`App.tsx:14188` 的 `<SessionList>` 传入：调 `handleNewSession`，且移动端追加收起右侧栏——模式照抄 :14167-14170 的 `onSelect`。
5. 文案：`aria-label`/`title` 复用 T4 的 `action.newSession`。

**DoD**：
- 桌面点击 → 主视图为新会话态（C-9）；移动端点击 → 侧栏收起 + 主视图新会话态（C-10）；
- 多项目会话视图不出现该按钮；
- 搜索结果模式头部无该按钮、返回键正常；
- `make typecheck` 通过。

### T4　i18n 与 onboarding 文案【0.5h，与 T1/T2/T3 同提交或紧随】

**实施要点**（zh/en 成对操作）：

| 操作 | key | 位置 |
|---|---|---|
| 删除 | `action.placeholder.newSessionSwipe` | zh:639 / en:641 |
| 删除 | `action.swipeNewSession` | zh:678 / en:680 |
| 删除 | `action.releaseNewSession` | zh:679 / en:681 |
| 新增 | `action.newSession` | zh:「新建会话」/ en: "New session" |
| 改写 | `onboarding.sessionRing.body` | zh:771 / en:773 |

`onboarding.sessionRing.body` 目标文案——zh：「点击圆环可以展开或收起当前绑定会话；点击旁边的＋按钮可以开始一个新会话。」en 同义翻译。`onboarding.sessionRing.title`（zh:770 / en:772）不变。`OnboardingTour.tsx:26` 的步骤定义不动（锚点选择器字符串不变，锚点属性位置由 T2 移动）。

**DoD**：
- 全仓 grep `newSessionSwipe|swipeNewSession|releaseNewSession` 零命中；
- zh/en 切换无缺 key 告警、无中英错配（C-15）；
- onboarding 走查文案与实际交互一致（C-11）。

## 3. 实施顺序与依赖

```
T1（删手势）──→ T2（环旁＋）──┐
                              ├──→ 手工回归（§4.2）
T3（列表头＋）────────────────┘
T4（i18n）：key 删除与 T1 同提交（否则 typecheck 断）；key 新增先于/随 T2、T3
```

- T1 与 T2 同文件同区域，**同一研发顺序完成**；建议 T1+T4(删除部分) 一个提交、T2+T3+T4(新增部分) 一个提交，第一个提交后即可独立验证「手势消失、点击如常」。
- 全程避开 `ActionBar.tsx` 的键盘/IME/composition 区域（第一轮 fork-plan §2.6 标记的雷区），本专项任何任务都不触碰 `IME_ENTER_GUARD_MS` 一带。

## 4. 测试计划

### 4.1 自动化

- `make typecheck`、`make test-web` 全量通过（现有 vm 套件作回归网；本专项为纯 UI 结构改动，vm 沙箱测试不覆盖 JSX 层，**不新增自动化用例**，以下手工矩阵为主）。
- CI（ubuntu + windows）双绿。

### 4.2 手工用例

| # | 前置 | 步骤 | 期望 |
|---|---|---|---|
| C-1 | 桌面，已绑定会话 | 分别在「绑定会话显示在主视图 / 抽屉已开 / 抽屉未开」三态下点环 | 依次：无动作 / 关抽屉 / 开抽屉（`App.tsx:14647-14666` 三分支不变） |
| C-2 | 桌面 + 移动 | 按住环向左/右/上/下拖出 >40px 后松开，再点击 | 环无位移跟随、不新建会话；随后点击行为正常 |
| C-3 | 已绑定会话（chat 模式） | 点「＋」 | 立即回新会话主视图；原会话不自动回绑；agent 保留、model/effort 回默认；去会话列表点原会话可重新绑定 |
| C-4 | 未绑定会话 | 观察输入控件区 | 无「＋」按钮；环为灰色空环 |
| C-5 | 会话 pending 中 | 点「＋」新建，观察原会话 | 新会话可用；原会话后台继续产出、完成通知正常 |
| C-6 | command 模式绑定 shell 会话 | 观察「＋」并点击；检查右缘控件 | 「＋」显示、点击后开新；与发送/取消键无重叠 |
| C-7 | 输入多行文本 | 观察控件沉底行 | 「＋」随控件组正常排列，无重叠、无溢出 |
| C-8 | 绑定会话且非 pending | 观察输入框占位文案 | 常规 mode 占位文案，无「左滑蓝环」表述 |
| C-9 | 桌面，会话列表 | 点头部「＋」 | 主视图切新会话态；按钮 hover 有 accent 反馈 |
| C-10 | 移动端，右侧栏开 | 点头部「＋」 | 侧栏收起 + 主视图新会话态；键盘不弹出 |
| C-11 | 重置 onboarding | 走查 session-ring 步骤 | 高亮覆盖「＋」+ 环；zh/en 文案与交互一致，无「拖动/左滑」 |
| C-12 | 移动/桌面 + 讯飞、搜狗输入法 | 输入中文并回车 | IME 行为与改动前一致（雷区回归） |
| C-13 | 全部 6 套主题 | 检查两个「＋」按钮 | 可见、idle/hover 对比度达 AA |
| C-14 | 移动端 | 手指从环/「＋」位置起手上下滑动页面 | 页面正常滚动（`touchAction:none` 副作用消除） |
| C-15 | 切换 zh/en | 全界面走查 | 无缺 key、无残留旧文案 |

### 4.3 回归重点

抽屉三分支（C-1）、IME（C-12）、pending 并发会话（C-5）三项为必测；其余按矩阵执行。真机至少覆盖 iOS Safari + Android Chrome 各一轮（C-2/6/7/10/14）。

## 5. 风险与回滚

| 风险 | 应对 |
|---|---|
| inset 常量核算遗漏某组合导致控件重叠 | C-6/C-7 六组合矩阵强制过一遍；重叠只影响视觉不丢数据 |
| onboarding 锚点外移后高亮框偏移 | C-11 专项验证；OnboardingTour 按选择器取 rect，属性移到包裹容器即自然扩大 |
| 与上游 `ActionBar.tsx` 未来合并冲突 | 改动以净删除为主、新增集中在一个包裹容器内，冲突面小；提交拆两个（§3）便于单独 revert |
| 回滚 | 纯前端两个提交，`git revert` 即完全恢复，无数据/接口兼容问题 |

## 6. 估时汇总

| 任务 | 估时 |
|---|---|
| T1 手势下线 | 0.5h |
| T2 环旁「＋」（含 inset 核算与真机核对） | 1.5h |
| T3 列表头「＋」 | 1h |
| T4 i18n 与 onboarding | 0.5h |
| 手工回归（§4.2 矩阵 + 双真机） | 1.5h |
| **合计** | **约 0.5–0.7 天**（与 PRD 预估一致） |
