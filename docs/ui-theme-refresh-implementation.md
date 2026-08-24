# UI 主题换肤实现说明（研发侧）

> 对应产品文档：`docs/ui-theme-refresh.md`。本文档只记录实现偏差、豁免清单与验证证据，不改需求。

## UI-T1 逃逸色审计与语义收编 ✅

### 新增语义 token（`web/src/index.css` `:root` 块末尾）

| token | `:root` 旧值（全部旧主题经 CSS 继承取此值） | graphite 值 |
| --- | --- | --- |
| `--danger-color` | `#dc2626` | `#e5484d` |
| `--warning-color` | `#f59e0b` | `#d9a054` |
| `--success-color` | `#15803d` | `#6cb28e` |
| `--on-accent-text` | `#ffffff` | `#ffffff` |

旧主题块（light 默认 / system 深色 / dark / meadow / moss）均不覆盖这 4 个 token，继承 `:root` 原值 —— 满足 U-2 验收②"旧主题观感不变"。

### 收编明细（批量替换，共 139 处）

- `#dc2626`×31、`#ef4444`×12、`#b91c1c`×10 → `var(--danger-color)`
- `#f59e0b`×7 → `var(--warning-color)`
- `#15803d`×10、`#16a34a`×2、`#10b981`×1、`#22c55e`×3（状态绿）→ `var(--success-color)`
- `#2563eb`×19（+3 处 border 简写）、`#3b82f6`×10 → `var(--accent-color)`
- `#1d4ed8`×11 → `var(--accent-hover)`
- `#fff`×4（仅同行含语义 token 背景的按钮文字）→ `var(--on-accent-text)`

**旧主题微变（已知偏差，量级极小）**：`#ef4444`/`#b91c1c` 统一为 `#dc2626`，`#16a34a`/`#10b981`/`#22c55e` 统一为 `#15803d`，`#3b82f6` 统一为 `#2563eb`——同语义色的相邻色阶合并，属文档 §2"收编到语义 token"的预期结果。

### 豁免清单（残留 hex，逐类理由）

| 类别 | 代表色值（处数） | 理由 |
| --- | --- | --- |
| ANSI 终端色板 | `ToolCallCard.tsx` ansiBaseColors/ansiBright 两数组 | 文档明示豁免：终端输出还原色，不随主题 |
| Agent/品牌图标固有色 | `#2f80ed`×4、`#48ba34`、`#3b88c3`、`#ffac33`、`#ffe8b6` 等单次系列（AgentIcon/ModeIcon/ToolCallCard SVG fill） | 文档明示豁免：品牌识别色 |
| warning 深浅辅助档 | `#b45309`×14、`#d97706`×7、`#92400e`、`#fcd34d`、`#facc15` | amber-700/600 为浅底上的深警告文字，统一到 `--warning-color`（amber-500）会损失 AA 对比度；graphite 下属语义彩色，不违反色相纪律 |
| danger/success 深档 | `#991b1b`×3、`#166534`×2、`#fecaca`、`#fef2f2` | 同上：深强调文字/浅色语义底，独立档位 |
| 独立语义紫（skill/远端） | `#7c3aed`×7、`#c4b5fd`×3、`#9b51e0`×3 | skill token 文字色与远端 commit 标记，第三语义通道，4 token 未覆盖；graphite 走查观察项 |
| 固定灰阶 | `#9ca3af`×7、`#94a3b8`×5、`#64748b`、`#475569`、`#344054`、`#0f172a`、`#f8fafc` 等 | 固定深/浅表面（Toast、遮罩、代码卡片）上的配套灰阶，不随主题翻转；slate 系蓝调极弱，graphite 走查观察项 |
| `#fff` 残留 | ×49 | 固定彩色底上的白字（徽章、Toast）与深色遮罩上的白字，非 on-accent 语境 |
| rgba 透明变体 | `rgba(239,68,68,0.14)` 等 | 语义色的透明底色变体，收编需引入透明度 token 体系，超出本任务范围；graphite 走查观察项 |
| 青色单点 | `#0ea5e9`×2、`#0f766e`×2 | 单点信息色，走查观察项 |

### 验证

- `npm run typecheck` ✅
- `web/tests/*.test.mjs` 16 个全绿 ✅
- 旧主题回归：4 token 靠继承取原值，主题块零改动（截图走查随 UI-T3 一并做）

## UI-T2 graphite 主题落地 ✅

- `web/src/index.css`：新增 `:root[data-theme="graphite"]` 块（插在 dark 语法高亮规则后、meadow 前），全部变量按产品文档 §3.1 表逐项填入，未做偏差调整；语义 token 用 §3.2 graphite 值；附 graphite 版语法高亮修正规则（同 dark 结构，走 `--mindfs-code-*` 变量）
- `web/src/services/appearance.ts`：`AppearanceMode` 类型、`appearanceModes`、`themeColors`（`graphite: "#171717"`，PWA theme-color）、`getEffectiveAppearanceMode` 返回类型与直返分支
- `web/src/components/FileTree.tsx`：`APPEARANCE_OPTIONS` 在 dark 之后插入 graphite
- i18n：`appearance.graphite` = 「石墨」/「Graphite」

### 验证

- `npm run typecheck` ✅、web 测试 16 个全绿 ✅
- 截图走查与 token 微调归 UI-T3

## UI-T3 截图走查 — 待做

## UI-T4 / UI-T5 — P2，graphite 定稿后由用户决定（UI-T5 需用户确认）
