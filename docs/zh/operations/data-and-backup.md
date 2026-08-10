# 数据、日志与备份

[English: Data, logs, and backup](../../operations/data-and-backup.md)

我把应用状态保存在工作目录中。我默认使用 ~/.soloqueue/，需要隔离环境时，
我通过 SOLOQUEUE_WORK_DIR 指定其他目录。

## 重要路径

| 路径 | 内容 |
| --- | --- |
| settings.yaml | Provider、Model、路由、工具策略、渠道和认证 |
| soloqueue.db | SQLite 状态、Memory、Team、Cron 和 Workflow 记录 |
| logs/ | 应用、HTTP、Timeline 和定时任务日志 |
| agents/ 与 groups/ | Agent 模板和 Team 定义 |
| skills/ | 用户安装的 Skills |
| plan/ 与 workspace/ | 计划和运行时工作内容 |
| artifacts/ | 生成的文件和媒体（如适用） |

我预计实际子目录会随功能使用变化，并把整个工作目录当作敏感数据。

## 安全备份

1. 我停止服务并等待进程退出。
2. 我把完整工作目录复制到加密且受控的备份位置。
3. 我记录 SoloQueue 版本和操作系统。
4. 我重启服务，并确认 UI 能够读取数据库和配置。

我先停止服务，让 SQLite 完成 WAL checkpoint，避免复制不完整的 Timeline 或
Workflow Run。

## 恢复

我停止 SoloQueue，先把当前目录保留为独立恢复副本，再把备份恢复到工作目录。
我启动同版本或兼容版本后，先检查迁移错误，再允许 Agent 修改项目。

## Memory Cleanup

我会先计划、再应用：

~~~bash
soloqueue memory cleanup --project-root /absolute/path/to/project
soloqueue memory cleanup --project-root /absolute/path/to/project --apply
~~~

我应用 Cleanup 时会创建数据库备份并写入 Cleanup Manifest。我会在 Audit 和抽样检查完成
前保留这两份证据。
