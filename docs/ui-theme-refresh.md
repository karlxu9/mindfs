# UI 升级专项：主题层换肤（Graphite 石墨主题）

> 生成日期：2026-08-24　|　含需求、设计规范与任务拆解三部分
>
> 用户已确认的范围决策：痛点 = **配色与整体气质**；目标气质 = **ChatGPT 风**（冷中性、极简克制、内容即界面）；深度 = **仅主题层换肤**（不动布局/组件结构，规避上游 UI 高频冲突区）；第一验收现场 = **桌面浏览器·深色模式**。

## 1. 背景与可行性依据

### 1.1 为什么"换肤"就能换气质（代码事实）

- 全部视觉样式收敛在 **CSS 变量 token 层**：`web/src/index.css`（约 1100 行）集中定义每套主题约 45 个变量；38 个组件中 37 个消费 `var(--…)`。大组件（`SessionViewer.tsx`、`ActionBar.tsx` 等）用 inline style + 变量，**不存在 Tailwind 硬编码色类**——改 token 即全局生效。
- 主题机制成熟：已有 5 种外观模式（dark/light/system/meadow/moss，`web/src/services/appearance.ts:1-14`），`meadow`（`index.css:313` 起）是"整套新增主题"的完整先例；切换 UI 在 `FileTree.tsx`。
- **新增主题的上游冲突面极小**：index.css 的新增块、appearance.ts 的模式清单、FileTree 菜单一项——全部是追加式改动。

### 1.2 当前深色为什么"难看"（问题定性）

现 dark 主题（`index.css:237-281`）是 **slate 蓝灰系**：`#0f172a/#020617` 蓝黑底、`#3b82f6` 高饱和蓝 accent、蓝紫色 root 徽章、launcher 带两层径向渐变、focus 用蓝色光晕。整体色相偏冷蓝且元素间色彩噪音多——与 ChatGPT 式"接近无色相的中性灰 + 极少彩色"的克制气质相反。

### 1.3 关键设计决策：新增主题，不改旧 dark

新增第 6 套主题 **`graphite`（石墨）**，而非覆写现有 dark。理由：① 零破坏、随时可切回对比；② 上游对 dark 块的 refine 不再与我们冲突（我们的改动在独立新增块）；③ 验证满意后再决定是否让 system 深色映射到 graphite（独立小任务，可逆）。

## 2. 需求（PRD 部分）

| 编号 | 需求 | 优先级 | 验收标准 |
|---|---|---|---|
| U-1 | **Graphite 深色主题**：按 §3 设计规范新增整套 token 块，注册到外观模式清单与主题切换菜单，含 PWA `theme-color` | P0 | ① 主题菜单可选"石墨"，切换后全界面（会话流/列表/文件树/看板/对话框/命令卡片/diff）无蓝灰残影、无渐变底；② 正文文字对比度 ≥ WCAG AA（4.5:1），次级文字 ≥ 3:1；③ 现有 5 套主题外观逐一切换回归，与改动前截图一致 |
| U-2 | **逃逸色收编**：审计全部组件中绕过 token 的硬编码色值（已抽样确认存在：`#2563eb`/`#dc2626`/`#ef4444`/`#f59e0b`/`#ffffff` 等，散落于 SessionViewer/ActionBar/SessionList 等），新增语义变量（danger/warning/success 等）后全部替换 | P0 | ① `grep -E "#[0-9a-fA-F]{3,6}" src/components src/App.tsx` 中残留的色值均为有意豁免（附豁免清单及理由，如 Agent 品牌图标色）；② 5 套旧主题下语义色观感不变（语义变量在旧主题中取原值） |
| U-3 | **截图走查验收**：按 §4 界面清单在 graphite 下逐一截图走查，与 ChatGPT 参考气质对照微调 token | P0 | 走查清单全项通过并归档截图；微调只发生在 token 值，不动组件 |
| U-4 | **Paper 浅色伴生主题**：与 graphite 同一气质的冷中性浅色（去蓝调、去渐变） | P2 | 同 U-1 验收口径，浅色模式下走查通过 |
| U-5 | **system 模式映射切换**：graphite 使用满意后，将 `prefers-color-scheme: dark` 的无主题默认块（`index.css:174-219`）指向 graphite 值 | P2 | 需用户确认后执行；system 模式下深色自动呈现 graphite；可单独 revert |

**明确不做**（本轮边界，防蔓延）：不动任何布局/间距/组件结构；不动字体（用户未选排印痛点）；不引入 UI 组件库或重构样式写法；不动 meadow/moss 等既有主题的值。若换肤后仍觉会话区观感不足，另立"会话区深改"轮次（用户已知晓其上游冲突成本）。

## 3. Graphite 设计规范（设计文档部分）

**气质要领**：色相趋零（纯中性灰，无蓝调）；彩色仅用于必要语义（危险/警告/链接）；界面靠**明度层级**而非颜色区分区域；无渐变、阴影极轻、focus 不发光。

### 3.1 Token 值表（对现有变量逐一映射，研发照表填入）

| 变量（现 dark 值） | Graphite 值 | 说明 |
|---|---|---|
| `--content-bg`（#020617） | `#212121` | 主内容区，ChatGPT 主底色 |
| `--sidebar-bg` / `--mobile-sidebar-bg`（#0f172a） | `#171717` | 侧栏比内容区暗一档，靠明度分区 |
| `--panel-bg`（#0f172a） | `#2a2a2a` | 卡片/面板浮起一档 |
| `--menu-bg`（#0f172a） | `#2f2f2f` | 菜单/弹层最亮一档 |
| `--bg-gradient-start` / `--bg-gradient-end` | 均 `#212121` | **渐变取消**（两端同值即纯色） |
| `--mindfs-launcher-bg`（三层渐变） | 纯色 `#171717`（或仅保留一层 ≤3% 白色径向作质感，走查定） | 去装饰 |
| `--mindfs-launcher-surface` 系列 | `#212121` 的 82%/94%/56% 不透明度对应值 | 去蓝底毛玻璃感，保留层级 |
| `--text-primary`（#F8FAFC） | `#ececec` | 主文字，略降刺眼度 |
| `--text-secondary`（#94A3B8 蓝灰） | `#b4b4b4` | **去蓝调**是气质关键 |
| `--mindfs-launcher-muted` | `#9b9b9b` | 同上 |
| `--border-color`（rgba(255,255,255,0.08)） | `rgba(255,255,255,0.08)` | 保持 |
| `--panel-border` / `--menu-border`（蓝灰 148,163,184 系） | `rgba(255,255,255,0.10)` | 中性化 |
| `--menu-divider` | `rgba(255,255,255,0.06)` | 中性化 |
| `--panel-shadow`（0 8px 24px .28） | `0 2px 8px rgba(0,0,0,0.35)` | 更小更收敛 |
| `--mindfs-launcher-shadow` | `0 8px 24px rgba(0,0,0,0.4)` | 减半径 |
| `--panel-focus-shadow`（蓝色 3px 光晕） | `0 0 0 1px rgba(255,255,255,0.28)` | **focus 改细白描边，不发光** |
| `--accent-color`（#3b82f6） | `#66a3e0` | 低饱和蓝，仅链接/必要活动态；明度满足深底 AA |
| `--accent-hover`（#60a5fa） | `#85b6e6` | 同系提亮 |
| `--selection-bg` / `--menu-active-bg`（蓝色透明） | `rgba(255,255,255,0.10)` / `rgba(255,255,255,0.08)` | **选中/悬停态改灰阶**，是 ChatGPT 气质的核心手法 |
| `--mindfs-code-bg`（#0f172a） | `#171717` | 代码块比正文暗一档 |
| `--mindfs-code-text` | `#e3e3e3` | 中性化 |
| `--mindfs-code-border` | `rgba(255,255,255,0.08)` | 中性化 |
| `--root-badge-*`（蓝紫系） | 底 `rgba(255,255,255,0.10)`、边 `rgba(255,255,255,0.16)`、字 `#d4d4d4` | 徽章去彩 |
| `--mindfs-system-bar-bg` / `--mindfs-topbar-bg` | `#171717` | 与侧栏一致 |
| `--mindfs-launcher-error-bg/text` | `rgba(127,29,29,0.3)` / `#f2b8b5` | 语义红保留、微降饱和 |
| appearance.ts `themeColors` 新增 | `graphite: "#171717"` | PWA 状态栏 |

> 表中值是设计基准，允许在 U-3 走查中做 ±1 档明度微调；**色相纪律不可破**：除 accent/语义色外一律零色相。

### 3.2 新增语义变量（服务 U-2，同时反哺全部主题）

新增四个 token 并在各主题块赋值（旧主题取其现散落值，graphite 取右列）：

| 新变量 | 收编对象（现硬编码） | graphite 值 |
|---|---|---|
| `--danger-color` | `#dc2626` / `#ef4444` | `#e5484d` |
| `--warning-color` | `#f59e0b` | `#d9a054` |
| `--success-color` | 审计中发现的绿色系 | `#6cb28e` |
| `--on-accent-text` | 按钮上的 `#ffffff` | `#ffffff` |

豁免类（不收编）：Agent 品牌图标固有色、语法高亮主题色板、第三方渲染器（mermaid 等）内部色。

## 4. 走查界面清单（U-3 验收用）

桌面宽度（第一现场）逐项截图：① 会话消息流（含用户消息、Agent 回复、tool call 卡片、思考块、代码块、权限请求卡）；② 会话列表（含 Agent 分组头、回复中圆点、置顶态）；③ 文件树 + 项目列表；④ 任务看板卡片；⑤ Git diff 查看器；⑥ 定时任务对话框与任务模板对话框；⑦ 命令执行卡片；⑧ 诊断面板；⑨ `@`/`#`/`/` 补全浮层；⑩ Toast（info/error/fatal 三级）。移动宽度抽查 ①②③。每项核对：无蓝灰残影、无渐变、层级靠明度可辨、focus 态可见。

## 5. 任务拆解（开发计划部分）

| 任务 | 需求 | 内容与 DoD | 估算 |
|---|---|---|---|
| UI-T1 逃逸色审计与语义收编 | U-2 | 全仓 `#hex` 与 `rgb(` 审计出清单 → 新增 4 个语义 token 并在 6 套主题块赋值 → 替换非豁免项。DoD：豁免清单归档进 PR 描述；5 套旧主题切换回归截图一致；`make typecheck` 绿 | 0.5 天 |
| UI-T2 graphite 主题落地 | U-1 | index.css 新增 graphite 块（照 §3.1 表）+ appearance.ts 注册（`appearanceModes`/`themeColors`，`appearance.ts:6-13`）+ FileTree 主题菜单项 + i18n 双语词条（"石墨/Graphite"）。DoD：切换生效、刷新持久、PWA theme-color 正确 | 0.5 天 |
| UI-T3 走查与微调 | U-3 | 按 §4 清单逐项截图走查，token 级微调（改值不改结构），截图归档 `verify-ui/`。DoD：清单全项通过；AA 对比度用工具抽验正文/次级文字 | 0.5-1 天 |
| UI-T4 paper 浅色伴生 | U-4 (P2) | graphite 定稿后同法炮制浅色版 | 0.5 天 |
| UI-T5 system 映射切换 | U-5 (P2) | **需用户确认后执行**：`index.css:174-219` 的 system 深色块指向 graphite 值；单独提交可 revert | 0.2 天 |

**顺序**：UI-T1 → UI-T2 → UI-T3（P0 合计约 1.5-2 天）→ 用户试用数日 → 决定 UI-T4/T5 与是否开"会话区深改"下一轮。

**回归红线**：本轮零组件结构改动，唯一的回归风险是 UI-T1 替换错语义（如把某处品牌色误收编）——审计清单必须人工过目；CI 照常全绿。
