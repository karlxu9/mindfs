# 石墨主题（ui-theme-refresh）测试报告

日期：2026-08-24
设计文档：`docs/ui-theme-refresh.md`
实现提交：`dbac173`（UI-T1）/ `af21466`（UI-T2）/ `fe7a014`（UI-T3）
方法：静态测试 + CDP 浏览器实测（Chrome 151，http://127.0.0.1:7331/?root=stock-verge）

## 结论

**自动化可覆盖项全部通过，0 项不通过**；1 条既有对比度问题降级为 P2 仅报告；UI-T4/UI-T5 两个 P2 项待决策未执行。

## 静态基线（前轮已跑，本轮不重跑）

- web typecheck 绿
- web tests 16/16 绿
- build 绿

## 六种外观模式实测

| 模式 | 关键 token（内容底/侧栏底/accent/正文） | 结果 |
|---|---|---|
| 石墨 graphite | #212121 / #171717 / #66a3e0 / #ececec | 通过 |
| 深色 dark | #0f172a / #020617 / #3b82f6 / #f8fafc | 通过 |
| 浅色 light | #f3f4f6 / #ffffffd9 / #2563eb / #0f172a | 通过 |
| 翠谷金光 meadow | #f4efe6 / #fffaf1b8 / #2d5a32 / #193033 | 通过 |
| 苔痕绿影 moss | #dfe5d4 / #eef1e78f / #415b3f / #1d261f | 通过 |
| 跟随系统 system | 本机系统为深色，正确回退 dark 全量 token | 通过 |

## 石墨 token 核对（设计文档 §3.1）

| 项 | 设计值 | 实测值 | 结果 |
|---|---|---|---|
| data-theme | graphite | graphite | 通过 |
| meta theme-color | #171717 | #171717 | 通过 |
| --content-bg | #212121 | #212121 | 通过 |
| --sidebar-bg | #171717 | #171717 | 通过 |
| --panel-bg | #2a2a2a | #2a2a2a | 通过 |
| --menu-bg | #2f2f2f | #2f2f2f | 通过 |
| --accent-color | #66a3e0 | #66a3e0 | 通过 |
| --text-primary | #ececec | #ececec | 通过 |
| --text-secondary | #b4b4b4 | #b4b4b4 | 通过 |

## 持久化与稳定性

- 切换后 `localStorage["mindfs-appearance-mode"]=graphite`：通过
- 整页 reload 后全部 token 不变：通过
- 控制台错误 0 条：通过
- 残色审计：约 4000 元素全量扫描，蓝黑残色零命中

## 对比度

- 正文 15.18:1；次级文字 6.5–8.65:1；徽章 9.14:1（均过 AA）
- **既有问题（P2，仅报告）**：侧栏选中页签"白字 + accent 底"2.66:1，低于 AA；属既有模式，非本次引入，建议后续轮次统一处理

## 豁免记录

- `--danger-color` 实测 #ec5d5e，偏离规范 #e5484d：`docs/ui-theme-refresh-implementation.md` L84 已授权，L82 有 AA 抽查记录

## 截图归档

`verify-ui/00–18` 共 19 张：桌面/移动、外观菜单、会话流、工具调用、git 面板与 diff、诊断、对话框、toast 三级、总览。

## 遗留

- UI-T4（paper 主题）、UI-T5（系统映射细化）为 P2，待决策
- 2.66:1 选中页签对比度待后续统一处理
