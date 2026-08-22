# MindFS 备份恢复说明

本备份包由 MindFS 的"导出备份"功能生成。包内结构：

```
manifest.json                     导出清单（版本、时间、目录映射）
RESTORE.md                        本文件
roots/<rootId>/…                  各托管目录的元数据（会话、看板、定时任务等）
fallback-db/<rootId>/session-list.db   仅当该目录使用 .link 兜底存储时存在
userconfig/…                      仅全量导出：用户配置目录内容
```

恢复前请先阅读 `manifest.json`：`roots[]` 里记录了每个目录的
`root_path`（原项目路径）、`meta_location`（project / home）与
`has_fallback_db`。`best_effort` 中列出的文件在导出时未经一致性快照
（如 `commands/history.db`），极小概率不完整，不影响会话数据。

## 通用步骤

1. **停止 MindFS 服务**：`mindfs -stop`（或关闭前台进程）。恢复过程中
   服务必须处于停止状态。
2. 按下面对应布局的说明放置文件。
3. 若做了全量导出，把 `userconfig/` 的内容复制到用户配置目录
   （Windows: `%AppData%\mindfs`；Linux: `~/.config/mindfs`；
   macOS: `~/Library/Application Support/mindfs`）。其中 `registry.json`
   记录了托管目录清单——如项目路径有变化，请先编辑其中的路径。
4. 启动 MindFS：`mindfs`。历史会话、看板任务与定时任务应全部可见。

## 布局一：project 模式（meta_location = "project"）

元数据位于项目目录内的 `.mindfs/`。

1. 确保项目目录存在于 `manifest.json` 记录的 `root_path`（或新路径，
   记得同步修改 `userconfig/registry.json`）。
2. 把 `roots/<rootId>/` 的**全部内容**复制为 `<项目目录>/.mindfs/`。

## 布局二：home 模式（meta_location = "home"）

元数据位于 `~/.mindfs/<rootId>/`。

1. 把 `roots/<rootId>/` 的全部内容复制为 `~/.mindfs/<rootId>/`
   （Windows 为 `%UserProfile%\.mindfs\<rootId>\`）。
2. 确保项目目录本身存在于 `root_path`。

## 布局三：.link 兜底（has_fallback_db = true）

该目录的会话数据库不在元数据目录内，而是由
`roots/<rootId>/sessions/session-list.db.link` 指向的外部路径承载。
备份把这个外部数据库放在了 `fallback-db/<rootId>/session-list.db`。

按布局一/二恢复元数据目录后，再二选一：

- **保持兜底布局**：打开 `sessions/session-list.db.link`，把
  `fallback-db/<rootId>/session-list.db` 复制到它指向的路径
  （目标机器上路径不存在时先创建；也可以改写 .link 内容指向新位置）。
- **转为常规布局**（推荐，更简单）：删除
  `sessions/session-list.db.link`，把 `fallback-db/<rootId>/session-list.db`
  复制为 `sessions/session-list.db`。

## 注意事项

- 若导出时**未勾选包含凭据**，包内没有 `credentials.json`、
  `agents-env.json` 等文件，恢复后需要重新配置 Agent 密钥与中继绑定。
- 若包含凭据，请把备份包当作密码保管。
- 恢复覆盖已有数据前，建议先把现场目录改名留档。
