# 第一次运行

[English: First run](../../getting-started/first-run.md)

## 1. 启动后端

我在仓库根目录运行：

~~~bash
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve --verbose
~~~

我读取服务端打印的监听地址。使用默认设置时，我打开
http://127.0.0.1:57647。第一次启动时，我让 SoloQueue 在 ~/.soloqueue/ 下按需
创建 settings.yaml、Agent/Team 存储、SQLite 和日志目录。

## 2. 检查 Provider

我打开 Settings → Models，确认至少有一个 Provider 和一个 Model 已启用。
我使用默认的 DeepSeek 配置，并从 DEEPSEEK_API_KEY 读取 Key；我也可以直接编辑
[settings.yaml 配置](../reference/configuration.md)。

如果模型列表为空，我会检查 Provider ID、Model ID 和 model_routes。
我使用 provider:model 作为路由值，例如
deepseek:deepseek-v4-flash-thinking。

## 3. 发送小请求

我打开 Chat，创建新会话并提出一个简短问题。第一次请求可能会初始化默认
Profile 和 Agent Catalog。测试时我会保留服务端终端，以便看到配置和工具错误。

## 4. 理解确认

当工具匹配确认策略时，我会看到运行暂停并在界面展示确认卡片。我会检查命令、路径和
作用范围后再允许执行。我把 --bypass 当作全局关闭工具确认的参数，只在受控实验中使用它。

## 5. 查找本地状态

我默认使用 ~/.soloqueue/ 作为工作目录，也可以由 SOLOQUEUE_WORK_DIR 指定。复制或删除
之前，我会先阅读[数据、日志与备份](../operations/data-and-backup.md)。

## 第一次运行失败时

- Portal 空白：我运行 make build-web，然后重启服务。
- 没有模型响应：我检查 Provider Key 和启用的 Model。
- 无法连接后端：我确认服务端终端打印的地址正在监听，并检查桌面代理端口。
- 远程返回 403：我先配置认证，再使用非回环地址，并阅读[远程访问](../operations/remote-access.md)。
