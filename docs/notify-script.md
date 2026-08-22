# notify-script 契约

启动 MindFS 时通过 `-notify-script <path>` 指定一个可执行脚本后，每当产生
通知事件，MindFS 会**执行该脚本一次**，并把事件 payload 以 **JSON** 写入
脚本的**标准输入**。脚本自行决定如何处理（转发到桌面通知、写日志、调用
第三方推送等）。

- Windows 上 `.ps1` 用 `powershell -NoProfile -ExecutionPolicy Bypass -File`
  运行，`.bat`/`.cmd` 用 `cmd /C`，其余按可执行文件直接运行；Unix 直接执行
  （需可执行位）。
- 单次执行超时 10 秒；最多 4 个并发；同一事件（按 `data.eventId`）30 分钟
  内只投递一次。
- 脚本以非 0 退出或超时只会记入服务日志，不影响 MindFS 本身。
- 通知文案语言跟随 Web 界面语言（`zh-CN` / `en-US`）。

## payload 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `type` | string | 事件类型，见下表 |
| `title` | string | 通知标题，形如 `<项目名> · <会话名> · <状态>` |
| `body` | string | 正文（摘要/错误信息，最长 600 字符，超长保留尾部） |
| `tag` | string | 通知去重/替换标签（同 tag 的系统通知会互相覆盖） |
| `url` | string | 点击跳转的相对地址，如 `./?root=<id>&session=<key>` |
| `icon` / `badge` | string | 图标相对路径（Web Push 用，脚本一般忽略） |
| `renotify` | bool | 同 tag 再次通知时是否仍提醒 |
| `requireInteraction` | bool | 是否要求用户手动关闭（`session.ask_user` 为 true） |
| `data.type` | string | 同 `type` |
| `data.rootId` | string | 项目（托管目录）ID |
| `data.sessionKey` | string | 会话 key（可能为空） |
| `data.taskId` | string | 定时任务 ID（仅 scheduled.* 事件） |
| `data.eventId` | string | 事件唯一 ID（30 分钟去重的 key） |

## 事件类型（4 + 1 种）

| type | 触发时机 |
|---|---|
| `session.done` | Agent 会话一轮回复完成（定时任务触发的轮次除外） |
| `session.ask_user` | Agent 等待用户输入（AskUser 工具） |
| `session.error` | Agent 会话进入错误态（定时任务触发的轮次除外，见 scheduled.failed） |
| `scheduled.done` | 定时任务执行成功 |
| `scheduled.failed` | 定时任务执行失败（含超时） |

## 示例脚本

仓库 `scripts/` 下有两个可直接使用的示例，把通知转发为桌面通知：

- Windows：`scripts/notify-example.ps1`
  ```
  mindfs -notify-script C:\path\to\scripts\notify-example.ps1
  ```
- Linux / macOS：`scripts/notify-example.sh`（Linux 需要 `notify-send`，
  macOS 用系统自带 `osascript`；记得 `chmod +x`）
  ```
  mindfs -notify-script /path/to/scripts/notify-example.sh
  ```

手工测试脚本本身（不经 MindFS）：

```powershell
'{"type":"session.done","title":"demo · 会话 · 完成","body":"hello"}' | powershell -NoProfile -ExecutionPolicy Bypass -File scripts\notify-example.ps1
```

```sh
echo '{"type":"session.done","title":"demo · Session · Done","body":"hello"}' | scripts/notify-example.sh
```
