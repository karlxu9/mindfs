# MindFS 二开计划 —— 需求与架构 Context 文档

> 生成日期：2026-08-17　|　基线提交：`5600d4e`　|　上游：`github.com/a9gent/mindfs`

## 0. 前提与边界

| 项 | 结论 |
|---|---|
| 使用场景 | 自己 / 团队**内部**使用，不对外提供网络服务 |
| AGPL v3 约束 | **无实际约束**。网络条款只在对外提供服务时触发，内部用可自由改，无需开源 |
| 二开方式 | fork `a9gent/mindfs`，长期跟随上游 |
| 技术栈 | Go 1.25 后端 + React 19 / Vite 5 / Tailwind 4 前端 + Capacitor(Android) / 鸿蒙壳 |

### 必须先知道的地基状况

- `web/src/App.tsx` **15,306 行**，154 个 `useState` / 92 个 `useEffect` / 178 个 `useCallback`。无状态管理库，无路由库。
- 最近 300 个提交里 `App.tsx` 被改动 **99 次**——每 3 个提交就有 1 个要动它。
- 485 个提交中 `fix` 123 + `refine` 122，`feat` 仅 64，**`refactor` 只有 1 个**。
- Prop drilling 实测：`FileTree.tsx` **60 个 props**、`ActionBar.tsx` 28 个、`SessionViewer.tsx` 17 个。
- 服务端巨型文件：`usecase/session.go` 3,868 行、`api/http.go` 2,646 行（112 个路由全在内）、`session/manager.go` 2,245 行。
- **无 CI**（`.github/` 不存在）。`web/tests/` 下 8 个 `.test.mjs` 用手写 `typescript.transpileModule` + `node:vm` 跑，**没有任何 Makefile target 会执行它们**；`make test` 只跑 `go test ./...`。

**结论**：不做专项大重构（无测试网 + 无 CI，风险不可控）。改为「每次动一个功能，顺手把它用到的状态从 `App.tsx` 切出去」。但**阶段 0 的 CI 与测试接线必须先做**——成本 1 小时，之后每一步都有回归保护。

---

## 1. 需求清单（已确认）

### 明确要做的 5 项

| # | 需求 | 性质 | 优先级 |
|---|---|---|---|
| 1 | 项目手动扫描——删除的项目重启后不再复活 | **Bug 修复** | P0 |
| 2 | Agent CLI 自动加载（Windows 端免手动刷新） | 受上游 SDK 约束 | P2 |
| 3 | 批量删除会话 | 新功能 | P1 |
| 4 | 显示项目所在目录 | 纯展示 | P0 |
| 5 | 会话列表按 Agent 分类折叠 | 新功能 | P1 |

### 体验短板（用户点选）

- **输入与补全手感**——已定位到具体成因，见 §2.6
- **会话管理**——fix 重灾区（46 次），需求 #3 #5 即属此域
- **任务看板 / worktree**——**本轮延期**，见 §4

---

## 2. 逐项实施方案

### 2.1　#1 删除的项目重启复活【P0 · Bug】

**根因（已定位）**：`server/app/server.go:278` 的 `autoAddExternalProjectRoots` 在启动时（调用点 `server.go:85`，另有 `:343`）扫描 Agent CLI 历史会话（`server/internal/agent/discovery.go:13` `DiscoverExternalProjectPaths`），把发现的项目路径**自动重新注册**。

`Registry.Remove`（`server/internal/fs/registry.go:171`）能正确删除并落盘，但**代码里没有任何地方记录「用户主动删除过这个路径」**。唯一的跳过条件是 `server.go:298` `hasMindFSMetadataDir`——只跳过带 `.mindfs/` 目录的路径。所以「被自动发现、但你从未真正打开过（因此没生成 `.mindfs/`）」的项目，删一次、下次启动复活一次。

**改动**：

1. `server/internal/fs/registry.go`
   - 持久化结构（`Load()` 的 `stored` 匿名 struct，第 50-53 行）现为 `{dirs, order}`，增加第三个字段 `removed []string`。
   - `Load()`（:40）读入，`saveLocked()`（:90）写出。
   - `Remove()`（:171）成功后把该 root 的**规范化路径**写入 tombstone。
   - ⚠️ **必须按路径而非按 name 记录**：`Remove` 内部以 `filepath.Base(root)` 作 map key，但 `autoAddExternalProjectRoots` 是用 `agent.NormalizeComparablePath(root.RootPath)` 比对的。tombstone 存规范化路径才能对上，否则拦不住。
   - 新增 `IsRemoved(path string) bool` 与 `ClearRemoved(path string) error`（用户手动重新添加时要能解除拉黑）。
2. `server/app/server.go:290` 的循环内，紧跟现有 `hasMindFSMetadataDir` / `IsTemporaryWorkDir` 判断之后，增加 `registry.IsRemoved(normalized)` 跳过。
3. `server/internal/api/appcontext.go:578`（`s.Dirs.Upsert(path)`，即用户手动添加路径）成功后调 `ClearRemoved`。

**「手动扫描」（用户明确要求）**：

4. 新增启动参数 `-no-auto-scan`，为 true 时 `server.go:85` 不调用 `autoAddExternalProjectRoots`。
5. 新增 `POST /api/dirs/scan` 端点，手动触发一次扫描并返回新发现的项目列表。
6. 前端在文件树菜单加「扫描项目」按钮。

**测试**：`server/app/server_test.go:112` 已有 `autoAddExternalProjectRoots` 的测试，在旁边加「删除后重新扫描不应复活」用例。

---

### 2.2　#4 显示项目所在目录【P0 · 纯前端】

**现状**：后端**早已在返回**。`server/internal/fs/fs.go:50` —— `RootPath string \`json:"root_path"\``，且前端类型 `ManagedRootPayload`（`web/src/App.tsx:624`）也已声明 `root_path?: string`。数据一路到了组件层，**只是从未渲染**（`grep -rn root_path web/src` 只命中 `App.tsx`，UI 组件里零使用）。

**改动**：纯展示。项目列表 / 项目选择器里，在项目名下方以次级字号显示 `root_path`。移动端空间紧张，建议只在 `title` / 长按气泡里给全路径，列表内显示尾部两级（如 `…/workspace/mindfs`）。

> 这是全部需求里最便宜的一项——建议第一个做，用来验证 fork + CI 链路是否通畅。

---

### 2.3　#3 批量删除会话【P1】

**现状**：全链路都是单条。前端 `web/src/services/session.ts:1285` `deleteSession(rootId, sessionKey)`；后端 `server/internal/api/usecase/session.go:918` `DeleteSession`，路由在 `server/internal/api/http.go:1061`。

**⚠️ 关键陷阱——不要在前端写循环**：`DeleteSession` 内部调 `deleteSessionCascadeKeys`（`session.go:950`），而该函数**每次都执行一遍 `manager.ListMetas(ctx)` 全量会话列表扫描**。前端循环删 N 条 = **N 次全量扫描**。删 50 条会明显卡顿甚至打爆服务端。

**必须做真正的批量端点**：

1. 后端新增 `DeleteSessionsInput{ RootID string; Keys []string }` 与 `Service.DeleteSessions`：
   - **只调一次 `ListMetas`**，一次性算出所有传入 key 的 cascade 闭包**并集**（父子同时被选中时要去重，避免重复删同一 key）。
   - 复用现有单条流程的每一步：`cancelActiveSessionTurn` → `manager.Delete` → `root.RemoveSessionFileMeta` → `commandexec.CloseSession` → `ReleaseFileWatcher`。
   - **部分失败要能报告**：返回 `{deleted: []string, failed: [{key, error}]}`，不要一条失败就整批回滚——用户重试成本高。
2. 新增路由 `POST /api/sessions/batch-delete`。
3. 前端 `services/session.ts` 加 `deleteSessions(rootId, keys)`。可直接参照现有批量先例 `importExternalSessionsBatch`（调用处 `web/src/App.tsx:5962`）。
4. `SessionList.tsx` 增加多选：目前**无任何多选基建**（无 checkbox、无 `selectedKeys`）。需要
   - 选择模式开关（长按进入 / 顶部「选择」按钮）
   - 行内 checkbox + 全选
   - 底部批量操作条 + **二次确认弹窗**（删除不可逆，且会级联删子会话——确认文案必须写明「将同时删除 N 个子会话」）

---

### 2.4　#5 会话列表按 Agent 分类折叠【P1】

**好消息——分组折叠的基建已存在，照抄即可**。`web/src/components/SessionList.tsx` 里已有一整套按**项目**分组的实现可直接镜像：

| 已有件 | 位置 | 用途 |
|---|---|---|
| `ProjectSessionGroup` 类型 | `SessionList.tsx:78` | 分组数据结构模板 |
| `ToggleRowButton` | `SessionList.tsx:108` | 已含 expand / collapse 图标与 a11y label |
| `expandedChildren` state | `SessionList.tsx:324` | 折叠状态管理模板 |
| `COLLAPSED_CHILD_SESSION_LIMIT = 3` | `SessionList.tsx:62` | 折叠阈值模板 |
| 会话的 agent 字段 | `SessionList.tsx:18` `agent?: string` | **分组依据已在数据里** |

**改动**：新增 `AgentSessionGroup`（镜像 `ProjectSessionGroup`），按 `session.agent` 聚合；折叠状态复用 `expandedChildren` 的模式并持久化到 localStorage（参照同文件 `PINNED_PROJECTS_STORAGE_KEY`，`:66`）。

**用户原话是「只使用图标下标区分不明显」**——所以除了分组，分组头部应带 **Agent 名称文字 + 图标 + 会话数**，而不是只换个图标。图标组件已有：`web/src/components/AgentIcon.tsx`，资源在 `web/public/assets/agents/`。

**与现有项目分组的关系需定夺**：当前多项目模式已按项目分组（且有 `MULTI_PROJECT_VISIBLE_LIMIT = 6` 硬限，`SessionList.tsx:63`）。建议**做成二选一的分组维度切换**（按项目 / 按 Agent），而不是两层嵌套——嵌套分组在手机窄屏上会挤到不可用。

---

### 2.5　#2 Windows Agent CLI 自动加载【P2 · 有上游约束】

**这是唯一一项「现状是故意为之」的需求，不能简单打开开关。**

`server/internal/agent/probe.go:323`：

```go
func shouldRunBackgroundRuntimeProbe(goos string) bool {
	return goos != "windows"
}
```

调用点 `probe.go:262`（启动首次全量探测）与 `probe.go:318`（`UpdateConfig` 后探测）。原因写在 `probe.go:260-261`：

> Windows 下深度探测会启动外部 Agent CLI；部分 SDK/CLI 无法由 MindFS 注入 `CREATE_NO_WINDOW`，后台启动时会出现空白控制台窗口。

**约束边界（已逐条验证）**：

| 协议 | 进程如何启动 | Windows 窗口是否已隐藏 |
|---|---|---|
| `acp`（16 个 agent） | `acp/process.go:402` 唯一 spawn 点 → `configureProcessCommand`（`:906`）→ `configurePlatformProcessCommand`（`process_windows.go:18`）设 `HideWindow: true` + `CREATE_NO_WINDOW` | ✅ **已安全** |
| `claude-sdk` | `claude/session.go:65` 交给第三方 `claudeagent` SDK，**进程由 SDK 内部 spawn**，MindFS 拿不到 `*exec.Cmd` | ❌ 无法注入 |
| `codex-sdk` | 同上（`codex-go-sdk`） | ❌ 无法注入 |

**方案 A（推荐，MVP）——按协议分流**：ACP 协议的 agent 在 Windows 上放开后台探测，`claude-sdk` / `codex-sdk` 保持手动。把 `shouldRunBackgroundRuntimeProbe(goos)` 改为 `shouldRunBackgroundRuntimeProbe(goos, protocol)`，在 `:262` / `:318` 两处按 `def.Protocol` 过滤。**一次改动覆盖 18 个 agent 中的绝大多数**，且零黑窗风险。

**方案 B（彻底解决）——改 SDK fork**：`go.mod:36-40` 显示**三个 SDK 早已 replace 到作者自己的 fork**：

```
replace github.com/fanwenlin/codex-go-sdk       => github.com/yandc/codex-go-sdk       ...
replace github.com/coder/acp-go-sdk             => github.com/yandc/acp-go-sdk         ...
replace github.com/roasbeef/claude-agent-sdk-go => github.com/yandc/claude-agent-sdk-go ...
```

所以「改 SDK」的先例和机制都已就位——在 `yandc/claude-agent-sdk-go` 与 `yandc/codex-go-sdk` 里加一个 `SysProcAttr` / `configureCmd func(*exec.Cmd)` 注入钩子，即可全量放开。**但这要维护两个额外的 fork**，跟随上游成本翻倍。

**建议**：先做方案 A 验证收益。只有当你确认 claude/codex 在 Windows 上的手动刷新仍然是日常痛点，再投入方案 B。

---

### 2.6　输入与补全手感【P1 · 便宜】

**已定位到三个具体成因**：

1. **512ms 防抖**——`@` 文件补全 / `/` 斜杠命令 / `#` 提示词都要等半秒才弹列表，手感发黏。
2. **常量重复定义两处**：`web/src/components/ActionBar.tsx:171` 与 `web/src/App.tsx:662` 各写了一遍 `const CANDIDATE_FETCH_DEBOUNCE_MS = 512;`——改一处会漏另一处。
3. **无客户端缓存**——`web/src/services/candidates.ts` 每次（防抖后的）按键都发一次 `/api/candidates` HTTP 请求。

**已经做对的部分**：`AbortController` 用得很规范（`ActionBar.tsx:709-711` 每次新请求前 abort 旧的），不需要动。

**改动**：

1. 常量收敛到 `web/src/services/candidates.ts` 单一来源，两处引用改为 import。
2. 防抖降到 **120-150ms**。
3. **加前缀缓存**（这才是让它「感觉即时」的关键）：已取到前缀 `fo` 的候选后，继续输入 `foo` 直接本地过滤，不发请求。缓存按 `(rootId, type, agent)` 分桶，会话切换时失效。降防抖 + 前缀缓存**同时做**，否则单纯降防抖会把请求量放大 3-4 倍。

> 顺带：`fix` 提交里有 14 项与 input 相关，含讯飞/搜狗 IME 回车提交问题（`17fb6e8`）。**动 ActionBar 输入逻辑时务必用国产输入法回归测试**，这块历史上反复出问题。

---

## 3. 执行顺序

### 阶段 0 —— 地基　✅ 已完成（2026-08-17）

已完成：

1. **Makefile 接线**——`test` 拆为 `test: test-go test-web`，新增 `test-go` / `test-web` / `typecheck` 三个 target。`test-web` 会先检查 `web/node_modules` 是否存在并给出可执行的报错提示；**所有测试全部跑完再汇总失败**，而不是遇错即停（一趟就能看到全部问题）。
2. **`.github/workflows/ci.yml`**——两个 job：
   - `go-test`：**ubuntu + windows 双矩阵**。理由：MindFS 的 Windows 进程处理（`CREATE_NO_WINDOW`、进程树 kill、shell 选择）藏在 build tag 后面，只有 Windows runner 才会编译和执行到，而这正是需求 #2 的战场。含 `go vet ./...`。
   - `web`：Node 24 + `npm ci` + `typecheck` + `make test-web`。
3. **upstream remote 已配置**，并把 upstream 的 push URL 设为 `DISABLED_use_origin`，防止误推到上游仓库。

**发现的问题（已修）**：`web/tests/agent-lifecycle-restart.test.mjs` 断言 en-US 文案为 `"Switch and restart Agent config"`，但实际是 `"Agent config switch & restart"`。`git log -S` 证实前者**从未在 `en-US.ts` 中存在过**——引入该文案与引入该测试是**同一个提交** `34ae7dc`。也就是说**这个测试从诞生起就是红的，因为没有 CI 跑它，18 个版本无人发现**。这就是阶段 0 的价值证明。

**验收结果**：8/8 web 测试通过；`typecheck` 干净；插入一个必然失败的临时测试后 harness 正确捕获并 exit 1（红路径验证通过）。

**⚠️ 本机环境缺失（开发前必装）**：当前机器**没有 `go`、没有 `make`、没有 `gh`**，只有 Node 24 + npm + git。所以：

- 上述验收是用等价 shell 循环手工执行的，**`make` 命令本身与整个 Go 侧（`go vet` / `go test`）尚未在本机验证过**——首次 CI 运行才会真正检验它们。若 `go vet` 在上游代码上本就不干净，从 workflow 里去掉该步即可。
- 要本地开发必须装：**Go 1.25+**（`go.mod` 要求）、**make**、以及 fork 用的 **gh**（或用网页 fork）。

**尚未完成——需要你操作**：fork 本身。`gh` 未安装，且创建 fork 会在你的 GitHub 账号下建仓库，属于对外可见操作，我没有代劳。请在网页 fork `a9gent/mindfs` 后执行：

```bash
git remote set-url origin https://github.com/<你的用户名>/mindfs.git
git push -u origin main
```

推上去后 CI 会首次运行，那一刻才算真正拿到绿色基线。

### 阶段 1 —— 当天见效（低风险）

4. **#4 项目目录显示**（§2.2）——纯展示，用来验证链路。
5. **#1 项目删除不复活**（§2.1）——P0 bug，含 tombstone + 手动扫描。
6. **输入补全手感**（§2.6）——常量收敛 + 降防抖 + 前缀缓存。

### 阶段 2 —— 会话管理（中等）

7. **#5 Agent 分组折叠**（§2.4）——照抄现有项目分组基建。
8. **#3 批量删除会话**（§2.3）——**务必走批量端点，不要前端循环**。

### 阶段 3 —— 有技术风险

9. **#2 Windows Agent 探测**（§2.5）——先只做方案 A。

---

## 4. 本轮明确延期（Aggressive Pruning）

| 项 | 为什么砍 |
|---|---|
| **任务看板 / worktree 优化** | 你点选了这块，但没给出任何具体痛点。它有 14 个 task fix + 6 个 worktree fix，水很深，涉及 `server/internal/kanban/`（`service.go` 1,248 行）+ `TaskTemplateDialog.tsx`（1,055 行）+ `ScheduledAgentTaskDialog.tsx`（1,073 行）。**在你能说出「具体哪一步让我难受」之前，任何改动都是猜。** 用一周，把每次觉得别扭的地方记下来，我们再开一轮。 |
| **`App.tsx` 专项拆分** | 15,306 行确实是头号阻力，但无测试网时专项大重构是纯风险。改为**随手做**：阶段 1-3 每次动到某功能，顺手把它的状态切成独立 hook / 模块。第一刀天然落在 §2.6（补全缓存要独立成 hook）和 §2.4（分组状态）。若几轮后仍嫌慢，再评估引入 `zustand`——**别在第一天引入**，那会变成一次和功能改动纠缠的大重构。 |
| **MCP / 多用户 / 桌面壳 / 语音输入** | 你没选。已验证确实都不存在，随时可以开新轮。 |

---

## 5. 一眼速查：改动点索引

| 需求 | 后端 | 前端 |
|---|---|---|
| #1 项目复活 | `fs/registry.go:40,90,171` · `app/server.go:85,278,290,298` · `api/appcontext.go:578` | 文件树菜单加「扫描项目」 |
| #2 Windows 探测 | `agent/probe.go:262,318,323` · （方案 B 需改 `go.mod:36-40` 的 SDK fork） | — |
| #3 批量删除 | `usecase/session.go:918,950` · `api/http.go:1061` 新增批量路由 | `services/session.ts:1285` · `SessionList.tsx` 多选 |
| #4 目录显示 | 无需改动（`fs/fs.go:50` 已返回） | `App.tsx:624` 类型已就绪，仅需渲染 |
| #5 Agent 分组 | 无需改动 | `SessionList.tsx:18,62,66,78,108,324` |
| 补全手感 | — | `services/candidates.ts` · `ActionBar.tsx:171,709` · `App.tsx:662` |

## 6. 风险登记

| 风险 | 应对 |
|---|---|
| 批量删除前端循环打爆服务端 | 走批量端点，只调一次 `ListMetas`（§2.3） |
| 级联删除误删子会话 | 确认弹窗写明子会话数量 |
| tombstone 按 name 记录导致拦不住 | 必须按 `NormalizeComparablePath` 规范化路径记录（§2.1） |
| Windows 放开探测弹出黑窗 | 只对 `acp` 协议放开（§2.5 方案 A） |
| 改 ActionBar 破坏国产输入法 | 讯飞 / 搜狗 IME 回车回归测试（§2.6） |
| fork 与上游分叉 | 阶段 0 建 upstream remote；改动尽量收敛在少数文件，避开 `App.tsx` 高频冲突区 |
