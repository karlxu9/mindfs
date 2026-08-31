# 「新建会话入口重做」测试报告

> 被测提交：`038d6d6`（删除会话环左滑手势，T1+T4-del）、`a8e55cc`（环旁＋会话列表头部「＋」按钮，T2+T3+T4-add）
> 设计文档：`docs/ui-new-session-entry.md`；实施计划：`docs/ui-new-session-entry-devplan.md`
> 测试环境：本地生产实例 `127.0.0.1:7331`，CDP 直连 Chrome（移动端 500×844 / 桌面端 929×917 双标签）
> 结论：**可以提交推送**。自动化可覆盖项全部通过；剩余 4 项需真机/手工抽查，均不阻塞合并。

## 一、结论先行

- 静态门禁：typecheck、web 测试 16/16、生产构建全绿（前轮已跑，本轮不重复）。
- 活体矩阵：C-1a、C-2、C-3、C-4、C-7、C-8、C-9、C-10、C-11、C-13、C-15 通过；C-1b/c 以「静态回归 + 分支(a)活体 + 一次意外但无害的发送流活体」覆盖。
- 真机遗留：C-5（并发）、C-6（command/shell 会话）、C-12（IME）、C-14（触摸滚动）。
- 无阻塞缺陷。发现 6 条行为事实/注意事项，见第四节。

## 二、用例结果

| 用例 | 验证方式 | 结果 | 证据 |
|---|---|---|---|
| C-1a 绑定点环无副作用 | 活体 | ✅ | 绑定→点环：sheetCount=0、URL 不变、activeElement=BODY；`verify-ns/ns-04-c1a-bound-ring-noop.jpeg` |
| C-1b/c 新会话态与回绑 | 静态+活体(分支a)+发送流活体 | ✅（降级说明见下） | `git diff HEAD~2 HEAD`：App.tsx 仅 +4 行接线；环三分支 `onSessionClick` 未动；本轮 Enter 误发两条消息至 fast-echo 会话，agent 数秒内回执、`pending=false`，发送流活体无副作用 |
| C-2 解绑态点环回主视图 | 活体 | ✅ | 前轮 |
| C-3 ＋点击等价原左滑 | 活体 | ✅ | 解绑、model/effort 回默认、原会话不自动回绑 |
| C-4 ActionBar 拖拽机制移除 | 静态 | ✅ | 拖拽代码净删除，环点击简化为 `onClick={() => onSessionClick?.()}` |
| C-5 并发会话 | 真机遗留 | ⏸ | 需双端同时操作 |
| C-6 command 模式 shell 会话 | 真机遗留 | ⏸ | 需绑定 shell 会话 |
| C-7 多行输入控件布局 | 活体 | ✅ | 3 行文本：编辑器 44→112px 向上扩展、底锚定；控件行固定 (484,866,180×32) 在视口内；末行文本底 859 < 控件顶 866，无重叠无溢出；`verify-ns/ns-05-c7-desktop-multiline.jpeg` |
| C-8 占位文案无「左滑蓝环」 | 活体 | ✅ | 前轮 |
| C-9 桌面会话列表头部＋ | 活体 | ✅ | 点击后主视图新会话态；按钮 (692,1,34×34) 存在；hover accent |
| C-10 移动端侧栏头部＋ | 活体 | ✅ | 侧栏收起+主视图新会话态，键盘不弹出；`verify-ns/ns-03-c10-drawer-plus.jpeg` |
| C-11 onboarding 锚点与文案 | 活体(DOM)+源码 | ✅ | `[data-onboarding="session-ring"]` 包裹容器 64×32 精确覆盖＋(293,793,32×32)+环(325,793,32×32)；zh 文案恰为「点击圆环可以展开或收起当前绑定会话；点击旁边的＋按钮可以开始一个新会话。」；en 对应一致；两 locale 无「拖动/左滑」 |
| C-12 IME 组合输入 | 真机遗留 | ⏸ | 需真实输入法 |
| C-13 六主题可见/对比 | 活体 | ✅ | dark/light/system/meadow/moss/graphite 下＋均可见；idle 色逐主题等于 `--text-secondary`；hover 等于 `--accent-color`（graphite #66a3e0、light #2563eb）；`verify-ns/ns-06-c13-light-plus.jpeg` |
| C-14 触摸滚动 | 真机遗留 | ⏸ | 需触摸设备 |
| C-15 en 文案与死键清理 | 源码 | ✅ | en 有 `"session.new": "New session"`、`"action.newSession": "New session"`；`swipeNewSession|releaseNewSession|newSessionSwipe` 全源码零命中 |

截图归档：`verify-ns/`（ns-01 解绑态、ns-02 绑定态、ns-03 抽屉＋、ns-04 绑定点环无副作用、ns-05 桌面多行、ns-06 light 主题＋）。

## 三、C-1b/c 降级说明

看板当前无任务卡片（完成/已取消仅计数，DOM 含隐藏节点均无卡片；API 证实已取消任务无会话；其余 root 零任务），任务会话路径不可活体到达；普通会话路径的发送会触发真实 agent。本轮 C-7 输入时 Enter 误发两条测试消息至桌面标签所绑会话，该会话为 `fast_service` 回执型 agent，秒级「收到 ✅」回执后 `pending=false`，无副作用——反而补上了发送消息流的活体证据。任务会话分支维持静态覆盖。

## 四、发现与行为事实（不阻塞，供知悉）

1. **Enter=发送、Shift+Enter=换行**。自动化/手工多行输入必须用 Shift+Enter；误用 Enter 会直接发送（本轮即因此产生两条无害回执消息）。
2. **编辑器右留白随形态切换**：单行 `padding-right:180px` 为悬浮控件让位；多行时降为 14px、控件沉底（`bottom:6px`），文本与控件垂直分离，实测无重叠。
3. **URL `session` 参数与 UI 解绑态可并存**（前轮发现，仍成立）。
4. **面包屑返回项目视图会清除绑定**（圆环变灰）。
5. **模型/effort 重置规则**：新会话态回默认值，agent 保留（设计如此）。
6. **安装横幅间歇遮挡移动端 footer 按钮**；桌面标签存在历史遗留绑定态。自动化点击前需做遮挡检测。
7. 主题切换经 `data-theme` + CSS 变量生效，按钮颜色带 0.15s 过渡；自动化读色需跨任务或等待过渡结束。
8. **a11y 缺陷建议（低优先级，转研发）**：会话环原为 div onClick，无 aria-label、不在键盘 tab 序，读屏与键盘用户无法操作；同容器内＋按钮的无障碍属性完整，可参照。建议研发补 role="button" + tabIndex={0} + aria-label + 回车/空格键处理。**已实施**（`a8c0c00`，2026-08-29）：环补 `role="button"`、`tabIndex={0}`、`aria-label`（新 key `action.sessionRing`，zh「当前会话」/ en "Current session"）、Enter/空格激活（preventDefault 防滚动），并在抽屉指示可见时暴露 `aria-expanded`。**活体验证通过**（2026-08-29，headless Chrome + CDP 真键事件）：① 桌面/移动双标签 role/tabindex/aria-label 均正确；② Tab 序可达（桌面 25 次、移动 22 次 Tab 后焦点落在环，紧随＋按钮之后）；③ affordance 态（bound≠main）下 `aria-expanded` 出现且与抽屉开合同步；④ 聚焦环后真实 Enter、Space 键均可开/关抽屉（expanded true↔false 翻转），鼠标点击三分支回归不变、环内箭头方向正确翻转；⑤ 未绑定与 bound==main 态不渲染 `aria-expanded`，键盘激活走同一 onSessionClick 无副作用。typecheck + web build 绿。截图归档 `verify-ns/ns-07-a11y-drawer-open.jpeg`、`ns-07-a11y-drawer-closed.jpeg`。

## 五、真机遗留清单（合并后抽查）

- C-5 双端并发新建/绑定竞争；C-6 shell(command) 会话下＋与右缘控件；C-12 中文 IME 组合期 Enter 不发送；C-14 会话查看器触摸滚动不受环手势移除影响。
