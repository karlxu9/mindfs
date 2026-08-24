# 二开第二轮 测试报告

> 测试日期：2026-08-22　|　被测版本：main @ b6653be（T1–T23 全部实施完成）　|　测试环境：Windows 11 Home（win32/amd64）、Go 1.26.5、Node 24.14
>
> 依据：[round2-prd.md](./round2-prd.md) 验收标准、[round2-devplan.md](./round2-devplan.md) 测试要点、[round2-implementation-notes.md](./round2-implementation-notes.md) 实现说明

## 0. 结论速览

- **代码层面：P0（R-1 ～ R-3）无不通过项**；P1/P2 已验证范围内也无不通过项。
- **CI 已闭环（2026-08-22 推送后更新）**：23 个提交推送 origin 后 CI run #12（head `b6653be`）**双平台全绿**（go test ubuntu-latest ✔ / go test windows-latest ✔ / web typecheck + tests ✔）。初版报告中的唯一硬阻塞（提交未推送、CI 未跑）已解除，PRD N-2 与 §5"CI 双平台全绿"达成。
- R-5.2（备份可恢复，发布硬门槛）已由测试方**独立重做并通过**，且额外覆盖了研发未测的".link 保持兜底布局"恢复分支。
- **发布判定：代码 + CI 关卡全部通过，仅剩 §5 的 8 条真机手工回归**（Web 更新、真实 agent 残留进程、手机推送/导出、macOS launchd 等），过完即可打 fork 版本。

**结论统计**：通过 26 项 ✔ ｜ 无法验证（需真机/真实环境）4 项 ｜ 不通过 0 项 ｜ 观察项 7 条（无阻塞级缺陷）

## 1. 测试方法与证据

1. **基线回归**（全部真实执行，非缓存）：`go test -count=1 ./...` 全仓 24 包绿；web 14 个 vm 沙箱测试绿；`tsc --noEmit` 绿；`scheduled` 包 `-count=5` 连跑稳定（研发在 T21 修过的偶发竞态未复现）。
2. **沙箱 E2E**（`verify-round2/`，隔离 APPDATA/USERPROFILE，独立构建二进制，端口 7551/7552，与用户常驻实例完全隔离）：真实守护启动/停止/重启、三种元数据布局造数、curl 直打全部新端点、备份导出/解包断言/全清恢复。
3. **造数工具**：新增守护型种子测试 `server/internal/session/e2e_seed_test.go`（仅环境变量触发，常规 `go test` 自动 SKIP，不影响 CI），向三种布局各种入 3 个带唯一标记的会话。
4. **证据留存**：沙箱与导出物在 `verify-round2/`（backup zip ×3、导出 .md、诊断 JSON、restart20 脚本与输出、RESTORE-extracted.md）；研发截图（`verify-t1/` 5 张、`verify-t15/` 4 张）已逐张复核，内容与验收描述相符。

## 2. 逐条验收结论

### R-1 优雅退出与进程治理【P0】

| 编号 | 验收点 | 结论 | 依据 |
|---|---|---|---|
| R-1.1① | 停止后 10s 内退出 + 完整关闭序列日志 | **通过（Windows 侧）** / Unix 无法验证（本机） | 沙箱累计 20+ 次停止（-stop / API / -restart），每次日志均出现完整 9 步序列 `http-server→ws-clients→relay→kanban→scheduled→agent-prober→agent-pool→command-shells→app-context→done`，全部 <1s（预算 10s）。Unix 侧：编排器/关闭链全部单测已随 CI 在 ubuntu 真实执行通过；真实 `kill -TERM` 活体实例验证仍属实机手工项（CI 不跑守护进程 E2E） |
| R-1.1② | 退出后无 mindfs 派生残留进程 | **通过（沙箱范围）** | 每次停止后按命令行过滤进程计数 = 0。真实 claude CLI 子进程场景沙箱无法构造（无真实 agent 会话），由已执行的 Runtime/Pool CloseAll 单测 + 研发手工清单兜底 |
| R-1.1③ | 无 `*-journal` 残留 | **通过** | 停止后 `find` 三个 root 与 home 元数据目录 = 0 |
| R-1.1④ | 无停留 `running` 的 stage run | **通过（单测依据）** | T7 单测（预置 running 行→重开 store→变 cancelled 且补 finished_at）已真实执行；E2E 需强杀执行中 agent，未模拟 |
| R-1.1⑤ | 反复 `-restart` 20 次后配置完整 | **通过（实测）** | 20/20 循环每次均等到健康新 PID；结束后 `registry.json`（3 root 全在、order 正确）与 `preferences.json` 解析无损；日志中 20 次 begin/20 次 done 全为优雅序列，无 taskkill/看门狗痕迹；会话数据完好 |
| R-1.2① | `-stop` 默认走 API 且达成 R-1.1 效果 | **通过（实测）** | CLI 输出 `mindfs service stopped`，日志 `POST /api/shutdown status=202` + 完整序列，PID 文件清理 |
| R-1.2② | 服务挂死时超时回退 taskkill | **通过（实测）** | 用永不响应的 node 假服务 + 伪造 PID/token 现场：`-stop` 耗时 5.17s（恰为 5s API 超时）后 taskkill 成功，进程消失、PID 文件清理、exit 0 |
| R-1.2③ | 关机 API 鉴权 | **通过（实测+单测）** | 实测矩阵：无 token 403 / 错 token 403（服务存活）/ 正确 token 202（优雅退出）。非 loopback 拒绝由已执行的单测覆盖（沙箱监听 loopback，外源请求无法构造） |
| R-1.3 | 自更新走退出链 | **无法验证（本机）** | 需真实安装环境 + 真实版本更新（dev 构建 `auto_update_supported=false`）。时序单测（finalAction 先 close 后 spawn、run 返回前完成）已真实执行通过。列入发布前手工清单（与研发口径一致） |
| R-1.4 | 9 处原子落盘 | **通过** | 静态核查：9 处全部改为 `config.WriteFileAtomic`，5 个目标文件生产代码零裸 `os.WriteFile` 残留；helper 4 例单测（含"rename 永久失败保旧内容+tmp 保留新数据"的注入失败路径）与 `fs/registry_test.go` 既有 6 例均已真实执行 |

### R-2 前端崩溃与错误可见性【P0】

| 编号 | 结论 | 依据 |
|---|---|---|
| R-2.1 | **通过** | `toast-fatal.test.mjs`（含 App.tsx 两处包裹的源码断言）真实执行绿；研发 3 张截图复核相符：主视图崩溃显示降级 UI 且文件树/会话列表存活、重试后看板恢复、抽屉崩溃限于抽屉容器。说明：崩溃注入需临时修改生产代码，测试角色不可为，故 UI 部分以"已执行源码断言测试 + 截图复核"为据 |
| R-2.2 | **通过** | fatal 持久/非 fatal 过期的纯逻辑断言真实执行；截图复核：fatal 深红持久条目 9s 不消失、✕ 可关 |
| R-2.3 | **通过（自动化范围）** | notify builder 字段快照 + `TestBroadcastSessionErrorNotifiesAndExemptsScheduled`（触发×2/豁免×1）真实执行绿。真机手机 PWA 收推送并点击跳转→手工清单 |

### R-3 数据安全小修包【P0】

| 编号 | 结论 | 依据 |
|---|---|---|
| R-3.1 | **通过（CI 闭环）** | 权限断言测试按约定在 Windows SKIP（本机确认确实 SKIP）；代码核查 0600 已收编原子 helper。Unix 侧权限断言已随 CI run #12 在 ubuntu runner 真实执行通过 |
| R-3.2 | **通过** | `TestDeleteSessionRemovesAllSessionFiles`（三文件删净+无文件不报错）与 `TestDeleteSessionsCascadeRemovesDebugLogs`（级联）真实执行绿 |

### R-4 远程可观测【P1】

| 编号 | 结论 | 依据 |
|---|---|---|
| R-4.1① | **通过** | 轮转 writer 三例单测（缩小阈值滚动、`.1/.2/.3` 链、8×50 并发写）真实执行；实测确认运行期日志由服务进程自写（守护模式下持续追加自己的按址日志文件） |
| R-4.1② | **通过（实测）** | 双实例（7551/7552）同时运行：各写 `mindfs-127_0_0_1_<port>.log`，交叉检索零污染；`-status` 输出的日志路径真实存在 |
| R-4.1③ | **无法验证** | 无 macOS 环境。列入手工清单 |
| R-4.2 | **通过** | `/api/logs` 实测：默认 200（文件不足时全量+`truncated:false`）、`lines=5` 精确、`lines=99999` 钳制生效；响应含 path/size_bytes/lines/truncated。鉴权与其他 `/api/*` 完全同一封装（`protectedEndpoint`）✔。等宽渲染与移动端截图复核相符。真机手机/Relay 远程访问→手工清单 |
| R-4.3 | **通过** | 实测字段与设计 §5.2 逐项一致（含 roots[].path/meta_location/session_count，.link 兜底 root 的 session_count=3 正确）；延迟 1.9ms（预算 500ms）；面板截图复核相符 |

### R-5 数据备份与导出【P1】

| 编号 | 结论 | 依据 |
|---|---|---|
| R-5.1 | **通过（实测）** | 三布局造数各 3 会话后导出：包内 9 会话数据齐全；`.link` 兜底 db 正确进 `fallback-db/<id>/` 且指针保留、项目侧无 db；单 root scope 只含该 root；非法 scope/root 均 400。导出时服务在运行，包内 db 快照用独立 sqlite 打开行数与名称全对（+T16 并发竞态单测已执行）。manifest 字段与设计 §4.3 一致（format_version=1、includes_credentials、roots[] 五字段、has_fallback_db 正确标记） |
| R-5.2（硬门槛） | **通过（独立重做）** | 停服→全清现场（appdata、双侧 .mindfs、home 元数据、兜底 db）→严格按包内 RESTORE.md 恢复三布局（link-c 走"**保持兜底布局**"分支，为研发未测路径）→重启：3 root 全在、9/9 会话列表+正文（逐 root 抽查唯一标记命中）、看板任务、`@daily` 定时任务定义全部可见。RESTORE.md 内容完整覆盖三布局+兜底二选一 |
| R-5.3 | **通过** | 报表与磁盘逐字节对账（exchange 843B/debug 18B/会话数 3 全对）；孤儿 debug 正确检出、清理后磁盘真实删除、活跃文件无损、db 完好会话仍可见。journal 机制说明见观察项 5 |
| R-5.4 | **通过（实测）** | 解包断言：`include_credentials=0` 包内凭据类文件为 0；`=1` 多出 `e2ee.json`、`local-cli-tokens.json`（沙箱仅有这两类凭据）；日志/PID/.stderr 永不入包。提醒文案与排除选项截图复核相符。手机端完整导出→手工清单 |

### R-6 定时任务可靠性

| 编号 | 结论 | 依据 |
|---|---|---|
| R-6.1 | **通过（单测）** | 挂起 SendMessage 超时后锁释放、LastError 含 "timed out"、下一周期正常触发——直接模拟验收场景的测试已真实执行 |
| R-6.2 | **通过（单测）** | 真实失败后跳过：LastError 保留原因、LastSkippedAt 独立写入 |
| R-6.3 | **通过（单测）** | 环形第 21 条挤最旧 + 成功/失败落盘断言 |
| R-6.4 | **通过（实测+单测）** | `@daily` 经 API 保存成功且 `next_run_at` 为 UTC（实测）；`@every` 真实 cron 触发 + 装饰后 UTC 口径由已执行单测覆盖 |

### R-7 / R-8【P2】

| 编号 | 结论 | 依据 |
|---|---|---|
| R-7.1 | **通过（自动化范围）** | builder 双语快照测试真实执行；`/api/preferences/ui-language` GET/PUT 实测持久化。真机英文推送→手工清单 |
| R-7.2 | **通过** | 契约文档 payload 字段表与 `notify.go` 结构逐字段一致、4+1 事件齐全；`.ps1` 本机真实运行 exit 0（Windows 为"对应平台"）；`.sh` 语法检查 + stderr 兜底分支实测输出正确（Linux/macOS 桌面通知分支无法本机验证） |
| R-8.1 | **通过** | E2E 实测导出：ATX 标题/出处引用/`---` 轮次分隔/用户与 Agent 标题带本地时间/UTF-8 文件名头，纯标准 Markdown；toolcall 折叠、图片"标注缺失"、空会话、文件名净化由已执行的 4 例转换器单测覆盖。Typora/Obsidian 抽验→手工清单 |

### 非功能需求

| 编号 | 结论 |
|---|---|
| N-1 上游友好 | 通过（抽查：新逻辑均在新文件/新包；App.tsx 等仅接线级 diff） |
| N-2 平台矩阵 | **满足（推送后闭环）**：CI run #12（`b6653be`）ubuntu + windows 双矩阵全绿 |
| N-3 安全边界 | 通过（shutdown 仅 loopback+token 实测；备份凭据明示提醒+排除选项实测）。另见观察项 2 |
| N-4 性能 | 通过（诊断 1.9ms；体检遍历在独立请求中执行，Go handler 天然并发不阻塞其他请求；轮转写路径开销为每次写一次原子 size 检查） |
| N-5 可回滚 | 通过（23 个提交与 T1–T23 一一对应，一任务一提交） |

## 3. 缺陷与观察项（无阻塞级缺陷）

1. **【流程·已解决】CI 未运行**（初版报告的唯一阻塞项）。2026-08-22 已推送 23 个提交（`01d0472..b6653be`），CI run #12 双平台全绿，R-3.1、R-1.4 Unix 重试路径等实证闭环。保留此条作记录。
2. **【低】备份包中兜底 db 出现非一致重复副本**。`scope=all` 导出时，`.link` 兜底 db 因物理位于用户配置目录而被 `userconfig/<rootId>/session-list.db` 原样收录（运行中 db 的裸拷贝，可能撕裂），与 `fallback-db/<rootId>/`（VACUUM INTO 一致性快照）重复。恢复文档只使用后者，不影响正确性，但建议 userconfig 遍历排除已作为兜底 db 归档的目录。复现：对含 .link 兜底 root 执行 `POST /api/backup/export?scope=all`，解包见两份同名 db。
3. **【观察·待产品复核】`relay-services.json` 权限**：设计 §3 表格写 0644，实现保持现状 0600（不放宽原则）。研发已在实现说明中声明，疑为设计表笔误。
4. **【观察】diagnostics 的 `scheduled.task_count` 口径为"已启用任务数"**（禁用任务不计，`next_run_at` 无值时省略）。与设计字段表兼容，实现说明已声明；前端展示"无已启用任务"语义一致。
5. **【观察】存储体检的 `journal_files` 在实际使用中几乎恒空**（自愈机制）：停服期间产生的 journal 会在下次打开 db 时被 sqlite 吸收（实测确认）；服务运行中注入的无效 journal 会被活跃连接的任意查询即时吸收（实测确认）。"报表列出→安全回收"路径仅在 db 未被任何连接打开时出现，该路径由已执行的 backup 包单测覆盖。非缺陷，帮助理解报表行为。
6. **【观察·建议手工确认】Windows 守护子进程与启动控制台的耦合度**。测试中观察到：强杀启动侧控制台进程树时，刚 spawn 的守护子进程会被株连（timeout 强杀 PowerShell 场景）；MSYS/PowerShell 等待行为也表明子进程与启动控制台存在句柄关联。日常"关闭启动它的终端窗口后服务是否存活"建议手工确认一次（用户常用开机自启路径无此问题）。
7. **【提示】两处验收依赖真机场景**，本轮以单测+研发清单兜底：R-1.1② 真实 claude CLI 子进程消失、R-1.1④ 强杀后 kanban running 收敛。

## 4. 测试执行记录摘要

| 项 | 数据 |
|---|---|
| Go 全量（无缓存） | 24 包全绿，其中新增测试所在包：app/api/usecase/backup/commandexec/config/notify/scheduled/session/relay/agent/claude/kanban/cli |
| web 测试 | 14/14 绿（含新增 diagnostics、toast-fatal）；typecheck 绿 |
| 沙箱优雅停止次数 | 23 次（-stop×4、API×1、-restart×19 内含），全部完整 9 步序列，0 次强杀路径 |
| R-5.2 恢复演练 | 1 次全流程（3 布局×3 会话 + 看板 + 定时任务，100% 可见） |
| 备份导出 | 4 次（all±凭据、单 root、非法参数） |

## 5. 发布前手工回归清单（合并研发清单，均为本轮无法自动化项）

1. R-1.3：Windows + Unix 各真实走一次 Web 触发更新（验证会话数据与 registry.json 无损）；
2. R-1.2/R-1.1②：真实 claude 会话下四条停止路径（-stop / -restart / 前台 Ctrl-C / Web 更新）后确认无 claude/node/shell 残留进程；
3. 手机 PWA：session.error 推送并点击跳转；手机端完整备份导出一次；
4. macOS launchd：自启动实例日志轮转生效；
5. Linux/macOS：`notify-example.sh` 真机桌面通知；
6. 导出 Markdown 在 Typora/Obsidian 抽验一篇；
7. relay 面板：服务退出时中继端立即感知下线；
8. （观察项 6）关闭启动终端窗口后服务存活确认。

## 6. 测试资产

- `server/internal/session/e2e_seed_test.go`：守护型造数测试（默认 SKIP，可复用于下轮恢复演练）；
- `verify-round2/`：沙箱现场与证据（backup zip、导出 md、diag json、restart20.sh 及输出、RESTORE-extracted.md），未入库；
- 本报告：`docs/round2-test-report.md`。
