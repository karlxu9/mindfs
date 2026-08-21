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

### 2.1　#1 删除的项目重启复活【P0 · Bug】　✅ 已实现（2026-08-21）

**根因**：项目发现（`server/internal/agent/discovery.go` `DiscoverExternalProjectPaths`）读的是各 Agent CLI 自己的历史记录，而历史**永远不会忘记一个目录**。`Registry.Remove` 只是从 `dirs` 里删掉，没有任何地方记下「用户主动删过这个路径」，所以下一轮发现（启动时 + 每分钟一次）就把它加回来。

唯一的旧防线是 `hasMindFSMetadataDir`——跳过目录下有 `.mindfs/` 的路径。**这道防线在本机是失效的**：用户 `preferences.json` 里 `new_project_meta_location: "home"`，元数据落在 `~/.mindfs`，项目目录下根本没有 `.mindfs/`，于是每个删掉的项目一分钟后必然复活。这也解释了为什么有人复现不了——它只在 meta 放 home 时暴露。

**实现**：

1. `server/internal/fs/registry.go` —— 墓碑（tombstone）
   - 持久化结构从 `{dirs, order}` 扩为 `{dirs, order, removed}`，`removed` 是 `[]RemovedRoot{root_path, removed_at}`，按 `root_path` 排序落盘（否则 map 遍历顺序会让每次保存都重写文件）。
   - key 用 `comparableRegistryKey`（Clean → EvalSymlinks → Abs → Clean，Windows 再小写），**按路径而非 name**。EvalSymlinks 是必需的：发现路径本身被解析过，不解析就会「按一个名字拉黑、按另一个名字重新发现」。
   - `Remove()` 成功后写墓碑；`Upsert()`（用户显式添加）清墓碑；`Load()` 里 dirs 中已存在的 root 会**压制**自己的墓碑（同时出现在两处只可能是用户手动加回来了，managed 是更新的事实）。
   - 新增 `UpsertDiscovered(root, metaLocation)`：发现路径专用，带墓碑判断，返回 `added bool`。**策略放在 registry 里，调用方无法忘记加这道判断**——这是它和 `Upsert` 分开的唯一理由。
   - 另有 `RemovedRoots()` / `IsRemoved()` / `ForgetRemoved()`（计划里叫 `ClearRemoved`）。
2. 新增包 `server/internal/projectscan` —— 把 `autoAddExternalProjectRoots` / `hasMindFSMetadataDir` / 定时循环整体搬出 `server/app/server.go`（连同它的测试搬到 `projectscan_test.go`）。**理由**：HTTP 层要能按需触发扫描，而 `server/app` 反过来 import 了 api 包，直接调用会成 import 环。
   - 墓碑判断放在 `EnsureMetaDir` **之前**——否则每一轮都会往用户已经删掉的项目里重新写一个 `.mindfs`（有测试守着这一点）。
   - `Result{Added, SkippedRemoved}`：跳过数要报出来，否则「扫描了但什么都没发生」看起来像坏了。
3. 手动扫描 + 关掉自动扫描
   - `POST /api/dirs/scan` → `usecase.ScanManagedDirs` → `AppContext.ScanProjectRoots`，返回 `{added, skipped_removed, dirs}`，并对每个新增 root 广播 `root changed` + `Scheduled.ReloadRoot`。
   - usecase 侧用**可选接口** `rootScanner` 做类型断言（照 `rootMetaLocationUpserter` 的既有写法），避免给 `Registry` 接口加方法后被迫改动 4 个测试 fake。
   - 环境变量 `MINDFS_PROJECT_AUTO_SCAN=0|false|off|no` 关掉每分钟的自动扫描，**不是**计划里的 `-no-auto-scan` 启动参数：有两个入口点（`cli/cmd/mindfs.go`、`server/cmd/mindfs-server/main.go`）各带一套 flag 和一个 `startupConfig` JSON，穿一个 flag 的成本远高于读一个环境变量；也不用 preferences，因为循环启动时 HTTP 层还没起来。
   - 前端：文件树「⋯」菜单里「添加项目」下方加「扫描项目」，结果用 info toast 报「新增 N 个 / 未发现新项目」，有跳过时补一句「已跳过 N 个此前删除的项目，可用『添加项目』重新加入」。

**测试**：`registry_test.go` 6 例（跳过已删除 / 重启后仍生效 / 路径写法等价匹配 / 显式添加清墓碑 / ForgetRemoved / Load 压制 managed root 的墓碑），`projectscan_test.go` 4 例（正常新增 / 跳过已删除且不留 `.mindfs` / 环境变量开关 / 原有 worktree 用例），`project_scan_test.go` 3 例（usecase 输出与错误传播）。墓碑持久化那条做过变异验证：去掉 `removed` 的落盘，精确挂在「重启后仍生效」这一条上。

---

### 2.2　#4 显示项目所在目录【P0 · 纯前端】

**现状**：后端**早已在返回**。`server/internal/fs/fs.go:50` —— `RootPath string \`json:"root_path"\``，且前端类型 `ManagedRootPayload`（`web/src/App.tsx:624`）也已声明 `root_path?: string`。数据一路到了组件层，**只是从未渲染**（`grep -rn root_path web/src` 只命中 `App.tsx`，UI 组件里零使用）。

**改动**：纯展示。项目列表 / 项目选择器里，在项目名下方以次级字号显示 `root_path`。移动端空间紧张，建议只在 `title` / 长按气泡里给全路径，列表内显示尾部两级（如 `…/workspace/mindfs`）。

> 这是全部需求里最便宜的一项——建议第一个做，用来验证 fork + CI 链路是否通畅。

---

### 2.3　#3 批量删除会话【P1】　✅ 已实现（2026-08-21）

**实现**（按计划走了真批量端点，没有前端循环）：

1. 后端 `server/internal/api/usecase/session.go`：
   - `sessionCascadeClosure(ctx, manager, keys)`——**一次 `ListMetas`** 算出所有请求 key 的级联闭包并集，共享 `seen` 去重（父子同选不会重复删同一 key），顺序保证**子在父前**（中途崩溃也不会留下指向已删父会话的孤儿）。
   - `DeleteSessions`——先对全部 key `cancelActiveSessionTurn`，再逐个 `manager.Delete`；**部分失败不回滚**，按 key 报 `Failed{key, error}`。删除成功后的清理步骤（`RemoveSessionFileMeta` / `commandexec.CloseSession` / `ReleaseFileWatcher`）失败只记日志不进 Failed——会话已经没了，报 failed 只会诱导一次注定 404 的重试。
   - **返回 `Deleted` 含级联删掉的所有 key**（包括客户端从没加载过的子会话）——前端清缓存直接用它，不再自己递归算闭包。
   - 单条 `DeleteSession` 改为委托批量路径（一套代码），对外错误语义不变（"session not found" → HTTP 404，有测试钉住）。
2. 路由 `POST /api/sessions/batch-delete`，body `{root, keys[]}`，响应 `{deleted[], failed[{key,error}]}`。
3. 前端 `services/session.ts` 加 `deleteSessions(rootId, keys)`；App 的 `handleDeleteSession` 清理逻辑抽成 `cleanupDeletedSessions(rootID, deletedKeys)`，单删/批删共用，且**单删也走批量端点**——服务端返回的真实删除清单比客户端在部分加载的列表上递归靠谱。
4. `SessionList.tsx` 多选：头部"批量选择"按钮进入选择模式 → 行首 checkbox（点卡片任意处即切换选中，`onClickCapture` 拦截打开/菜单）→ 底部操作条（全选/取消全选 + 已选计数 + 删除 + 取消）。
   - **二次确认**：`window.confirm`（与项目删除同款），选中项有子会话时文案明确写"其下的 N 个子会话会一并删除"。
   - 全选只选**当前可见**的行（折叠的 Agent 分组里的不算），否则确认框里的数字解释不通。
   - 会话在选择期间被别处删掉/切根时自动剔除失效 key；进入搜索结果模式自动退出选择模式。
   - 删除失败（`onDeleteMany` 返回 false）保留选择状态，用户可直接重试。

**测试**：`session_batch_delete_test.go` 5 例——并集去重（父+子+独立会话同删，key 不重复）、子先于父的删除顺序、missing key 按条报告且不影响其余、空 key 列表拒绝、单删路径 404 语义不变。原有 `TestDeleteSessionDeletesSubSessionTree` 经新路径继续通过。

---

### 2.4　#5 会话列表按 Agent 分类折叠【P1】　✅ 已实现（2026-08-21）

**实现**（收敛在 `SessionList.tsx` 内部 + 一个纯逻辑模块，`MultiProjectSessionList` 不动）：

1. 新模块 `web/src/services/sessionAgentGroups.ts`（无 import 纯模块，沿用 vm 沙箱测试方式）：
   - `buildAgentGroups`——**在已构建的行列表上分组**，不是在会话数组上。会话列表不是平的：子会话和"展开更多"行跟在顶层父会话后面，按行走、把每一行归到最近一个顶层会话的 agent 名下，子会话永远跟着父会话走（即使子会话是别的 agent 产的）。
   - 分组顺序按首次出现——列表本身已是 pinned 优先 + 最近优先，最近活跃的 agent 自然排前，不需要二次排序。
   - 无 agent 的会话（plugin shell / command）归入 `""` 组，显示为"其他"，**不会消失**。
   - `agentGroupLabel`——已知 agent 用正式大小写（和 `AgentIcon.tsx` 的 alt 一致：Claude / Codex / DeepSeek Harness / CodeBuddy…），未知的首字母大写。
2. `SessionList.tsx`：
   - 头部搜索按钮旁加分组切换按钮（`aria-pressed`，激活时 accent 色），开关持久化到 `mindfs-session-group-by-agent`。
   - 分组头：分割线 + **AgentIcon 图标 + Agent 名称文字 + 顶层会话数 + 折叠箭头**（用户原话"只用图标下标区分不明显"，所以头部必须带文字）。整行可点折叠，各组折叠状态持久化到 `mindfs-session-agent-groups-collapsed`（按 agent 名全局记，不分项目）。
   - 行渲染抽成 `renderRow`，平铺/分组两条路径共用，没有复制 JSX。
   - **搜索结果模式不分组**（搜索结果天然是横切的）。
   - 与项目分组天然二选一：Agent 分组只在单项目 `SessionList`，多项目视图保持按项目分组，无嵌套。

**测试**：`web/tests/session-agent-groups.test.mjs`——同 agent 交错合并、子行跟随父会话、无 agent 归"其他"、大小写/空白归一、孤儿结构行不丢、topLevelCount 只数会话不数行、标签映射。

---

### 2.5　#2 Windows Agent CLI 自动加载【P2 · 有上游约束】　✅ 已实现·方案 A（2026-08-22）

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

**实现（方案 A，2026-08-22）**：

1. `shouldRunBackgroundRuntimeProbe(goos string)` → `shouldRunBackgroundRuntimeProbe(goos string, protocol Protocol) bool`：非 Windows 一律 true；Windows 下只有 `ProtocolACP` 为 true。
2. 新增 `filterBackgroundProbeDefs(goos string, defs []Definition) []Definition`：非 Windows 原样返回；Windows 只保留 ACP 协议（`protocol` 为空时经 `agentDefinitionProtocol` 回退默认协议，所以 `claude`/`codex` 空协议也正确归入 SDK、被过滤掉，而 `qwen` 等空协议回退到 ACP、保留）。
3. 三个接线点：
   - `Start()`：去掉原来「非 Windows 才起后台探测」的整段拦截，改为始终 `go p.safeProbeAll(ctx)`；
   - `safeProbeAll()`：过滤后再 `probeConfiguredAgents`（`ProbeAll` 保持不过滤，作为将来手动「全部探测」的语义）；
   - `UpdateConfig()`：`installed` 先过滤再决定是否起后台探测。
4. 结果：Windows 上 ACP 协议的 agent（Gemini / Qwen / CodeBuddy…绝大多数）现在启动时**自动后台探测**，不再需要手动刷新；只有 `claude` / `codex`（SDK 内部 spawn、无法注入 `CREATE_NO_WINDOW`）仍保持手动，避免黑窗。品种本机 go1.26.5 windows/amd64 全套件零失败。

**测试**：`probe_platform_test.go` 重写 `TestBackgroundRuntimeProbeDisabledOnWindows`（windows×ACP/ClaudeSDK/CodexSDK + darwin/linux），新增 `TestFilterBackgroundProbeDefsOnWindows`（显式 ACP 保留、显式 SDK 过滤、空协议回退）与 `TestFilterBackgroundProbeDefsOnUnixKeepsAll`。

---

### 2.6　输入与补全手感【P1 · 便宜】　✅ 已实现（2026-08-21）

**三个成因，全部处理**：

1. **512ms 防抖 → 130ms**——常量收敛到 `web/src/services/candidateCache.ts` 单一来源（`CANDIDATE_FETCH_DEBOUNCE_MS`），`ActionBar.tsx` 与 `App.tsx` 两处本地副本删除，改为 import。
2. **客户端缓存**——新模块 `candidateCache.ts`（无 import 的纯模块，方便沿用 `projectPath.ts` 的 vm 沙箱测试方式）：
   - **精确命中**：同 `(rootId, type, agent, query)`、15 秒内 → 直接用缓存，**不发请求**。退格重打、重开 `@` 面板即时出结果。
   - **前缀派生**：缓存里有当前 query 的前缀（取最长的）→ 立即本地过滤显示，但**必须照发请求校正**。这不是可选的：服务端把结果截断到 10 条（`maxCandidateItems`），前缀的 top-10 里可能根本不含长 query 的真实匹配，本地过滤只能当乐观预览。原计划里"取过 `fo` 就不再请求 `foo`"的写法是**不可靠的**，已按截断语义修正。
   - 过滤复刻服务端匹配（大小写不敏感 substring）；file/skill 复刻服务端排序（前缀优先→短名→字典序），command/prompt 保持缓存顺序（服务端按最近使用排序，客户端排序会打乱）。
   - 派生过滤结果为空时**不显示**——真实匹配可能在被截断的部分里，自信地显示"无结果"比多等 130ms 更糟。
   - LRU 上限 80 条；`#` 提示词增删（`services/prompts.ts`）主动失效 prompt 桶，否则会在 15 秒窗口内读到旧列表。
3. **请求量**：降防抖本会放大请求 3-4 倍，但精确命中直接吃掉重复请求，派生预览不产生额外请求（校正请求本来就要发）。

**没动的部分**：`AbortController` 逻辑原样保留；**键盘/IME 处理一行未碰**（讯飞/搜狗回车提交的历史雷区在 `IME_ENTER_GUARD_MS` 一带，本次只改了取数 effect）。

**测试**：`web/tests/candidate-cache.test.mjs`——精确命中/过期降级、前缀派生+最长前缀优先、substring 语义、空结果抑制、服务端排序复刻、command 保序、桶隔离（含带空格 rootId 不串桶、skill 按 agent 分桶）、类型化失效、LRU 淘汰。

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

fork 已完成，origin 指向 `github.com/karlxu9/mindfs`，upstream 的 push URL 已设为 `DISABLED_use_origin` 防误推。

### 阶段 0.5 —— 让 Windows 变绿　✅ 已完成（2026-08-17）

Windows runner 一上线就红了 **67 个测试**（本机 go1.26.5 windows/amd64 复现，与 CI 一致）。分类与处置：

| 类别 | 数量 | 处置 |
|---|---|---|
| 测试没关 sqlite 句柄（Windows 拒绝删除被占用文件） | 49（usecase 17 / kanban 18 / session 14） | 抽 `newTestManager` / `newTestService` 辅助函数，统一 `t.Cleanup` 关闭 |
| `HOME` / `USERPROFILE`、`APPDATA`、`TMP`/`TEMP` 环境变量差异 | 9 | 见下方「Windows 环境变量」 |
| commandexec 里的 POSIX shell / 路径假设 | 5 | 按 `runtime.GOOS` 分流或 skip |
| POSIX 权限位断言（0600） | 2 | Windows 下跳过，见下方安全说明 |
| `filepath.IsAbs` 平台差异（update 包） | 1 | 按平台取 `/tmp/escape` 或 `C:\escape` |
| **生产代码死锁**（`fs`） | 1 | 见下方 |

**Windows 环境变量对照**（踩过的坑，后续写测试直接查表）：

| Go API | POSIX 读 | Windows 读 |
|---|---|---|
| `os.UserHomeDir()` | `HOME` | `USERPROFILE` |
| `os.UserConfigDir()` | `XDG_CONFIG_HOME` → `HOME` | `AppData`（大小写不敏感） |
| `os.TempDir()` | `TMPDIR` | `TMP` → `TEMP`（**不看 `TMPDIR`**） |

最后一条最阴：`autoAddExternalProjectRoots` 会过滤掉 `os.TempDir()` 下的路径，而测试用的 workspace 来自 `t.TempDir()`——在 Windows 上只设 `TMPDIR` 不起作用，被测根目录会被自己的过滤器吃掉。

#### 顺带挖出 3 个真实生产 Bug（都已修）

1. **`SharedFileWatcher` 在 Windows 上死锁**（`fs/shared_watcher.go`，提交 `bd99aea` + `bcc2345`）。
   `run()` 直接内联调用 `watcher.Add()`。fsnotify 的 `readDirChangesW` 后端**把 `Add` 和事件投递串行化**：`Add` 投递请求后等后端应答，而后端只有把手上那条事件交出去之后才会应答——接收方正是 `run()`。二者互等，永久挂死。
   inotify / kqueue 的 `Add` 是普通 syscall，所以**只有 Windows 会中招**。
   **用户可见症状**：在被监视的项目里新建一个目录，该项目的实时文件更新就此失效，直到重启。**已存在 18 个版本无人发现。**
   修法分两步，第一步不够：先把 `sw.mu` 移出 `Add` 的等待环（`bd99aea`），CI 侥幸绿过一次；但只要 `run()` 还自己调 `Add`，两方循环等待依旧存在，下一轮 CI 就挂了 10 分钟以上。最终把 `Add` 挪到独立的 `watchRegistrar` goroutine，`run()` 只往有界 channel 投递（满则丢弃并打日志，绝不阻塞事件循环）。本机 40 连跑约 1.0s 通过。
2. **`kanban.Service` 完全没有关闭路径**。sqlite 句柄和后台 goroutine 一直活到进程退出。补了 `Close()` + goroutine 生命周期（`closed` 标志 + WaitGroup + service ctx），关闭后 `taskStore` 拒绝新请求。
   ⚠️ **注意**：`server/app` 里**至今没有任何地方调 `Close()`**——那里根本没有优雅退出路径。目前唯一调用方是测试辅助函数。
3. **`Close()` 里 cancel 的顺序会漏掉一个打开的 sqlite 文件句柄**。先 cancel 再 wait 看着更整齐，实际会坑调用方：查询被中途取消时 `database/sql` **会在自己的 goroutine 上拆连接**，于是 `Close()` 已经返回、sqlite 却还占着文件，调用方紧接着删目录就在 Windows 上失败。改为**先 drain（5s 上限）、cancel 只作兜底**。

#### 安全说明（已记录，未修）

Windows 上本地 CLI token 文件（`server/app/local_cli_token.go`）与 relay 凭据文件**没有权限保护**：`os.Chmod` 在 Windows 只切换只读属性，`Perm()` 实测报 0666 而非预期的 0600。要真正限制需要设 ACL。两处测试断言已按平台跳过并在注释里标注为独立的加固事项。

#### 已知的上游测试隔离缺陷（非本次引入）

`relay` 包的 `TestManagerPollTerminalBindStatusStopsPolling` 在**全新进程中单独 `-run` 必挂**（5.85s 超时），但在包内跑第 2 轮起就过，跑完整 `go test ./...` 也过。已用 `git checkout` 取上游原版 `service_test.go` 复现 3/3——**证实是上游遗留的隔离问题**（依赖同包更早测试留下的进程内状态），不是本次改动的回归。CI 双平台均绿，故未动。

**本机验收（Windows, go1.26.5）**：`go build ./...` / `go vet ./...` 干净；`go test ./... -count=1` **全部包零失败**；kanban `-count=20` 绿；`fs` 并发测试 40 连跑绿。
`-race` 本机不可用（需 `CGO_ENABLED=1` + C 工具链），仅靠 CI。
`gofmt -l` 会列出全仓库约 130 个文件，**是 `core.autocrlf=true` 的 CRLF 产物，不是格式漂移**（对未改动文件 `gofmt -d` 的 diff 只有 `^M`）；CI 也不跑 gofmt，只跑 `go vet` + `go test`。

### 阶段 1 —— 当天见效（低风险）

4. **#4 项目目录显示**（§2.2）——纯展示，用来验证链路。
5. **#1 项目删除不复活**（§2.1）——P0 bug，含 tombstone + 手动扫描。✅ 已完成（2026-08-21）
6. **输入补全手感**（§2.6）——常量收敛 + 降防抖 + 前缀缓存。✅ 已完成（2026-08-21）

### 阶段 2 —— 会话管理（中等）

7. **#5 Agent 分组折叠**（§2.4）——照抄现有项目分组基建。✅ 已完成（2026-08-21）
8. **#3 批量删除会话**（§2.3）——**务必走批量端点，不要前端循环**。✅ 已完成（2026-08-21）

### 阶段 3 —— 有技术风险　✅ 已完成（2026-08-22）

9. **#2 Windows Agent 探测**（§2.5）——方案 A：Windows 上仅 ACP 协议后台自动探测，claude/codex 保持手动。✅ 已完成（2026-08-22）

> 阶段 3 收尾时顺带发现并修复一个**上游合并带入的 Windows 测试失败**：`TestSendCommandMessagePersistsFinalToolCallAndSuggestion` 用 `printf` 作为命令，而 Windows 的 `userShell` 是 PowerShell（`printf` 不是 cmdlet）→ 命令以 exitCode 1 失败。改为一律用 `echo`（bash / PowerShell 语义一致），保持跨平台覆盖。该测试由上游 `ba59b44 feat: add command run mode` 引入、经 `5790d82` 合并进入本仓。

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
| #2 Windows 探测 | `agent/probe.go` `shouldRunBackgroundRuntimeProbe`/`filterBackgroundProbeDefs` · `Start`/`UpdateConfig`/`safeProbeAll` | — |
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
| Windows 放开探测弹出黑窗 | 只对 `acp` 协议放开（§2.5 方案 A，已实施）；claude-sdk / codex-sdk 由 SDK 内部 spawn、无法注入窗口隐藏，仍保持手动 |
| 改 ActionBar 破坏国产输入法 | 讯飞 / 搜狗 IME 回车回归测试（§2.6） |
| fork 与上游分叉 | 阶段 0 建 upstream remote；改动尽量收敛在少数文件，避开 `App.tsx` 高频冲突区 |
| 在事件循环里调 `watcher.Add()`（Windows 死锁） | 任何新增的 fsnotify 注册都必须走 `queueWatchDir`，不得在 `run()` 里内联 `Add`（阶段 0.5） |
| `kanban.Service` 至今无人调 `Close()` | `server/app` 缺优雅退出路径。若后续要落 worktree/看板改动，先补一条 shutdown 链（阶段 0.5 已备好 `Close()`） |
| 取消 context 后立刻删目录（Windows 失败） | `database/sql` 会异步拆连接。先 drain 再 cancel，`Close()` 返回后文件句柄才真正释放（阶段 0.5） |
| Windows 上 token / 凭据文件权限形同虚设 | `os.Chmod` 只切只读属性。要真正限制需设 ACL——独立加固事项，未做（阶段 0.5） |
