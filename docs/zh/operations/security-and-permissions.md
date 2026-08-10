# 安全与权限

[English: Security and permissions](../../operations/security-and-permissions.md)

我把 SoloQueue 作为个人自托管软件使用。它的控制可以减少意外操作，但我不把
这些控制当作完整的安全边界。

## 工具确认

工具执行前可能暂停并请求确认。我会检查命令、路径、网络目标和副作用，再允许
执行。确认是 Agent Loop 中的互锁，不等同于操作系统权限、容器隔离或代码审查。

下面的参数会关闭所有工具确认：

~~~bash
./soloqueue serve --bypass
~~~

我只在受控本地实验中使用它，不会把它和公网监听或未经审查的 Agent 模板一起使用。

## 文件系统和项目范围

我用明确的绝对路径注册项目。项目路径决定工具和委派任务的工作范围，但底层进程
仍然拥有服务端操作系统用户的权限。高风险实验时，我会使用独立操作系统账号。

## 网络和子进程

HTTP Fetch、Web Search、MCP、Language Server 和 Shell 命令可以访问服务端进程
能够访问的资源。我会审查 Shell 策略、HTTP Host 策略、MCP 命令和环境变量。
我把默认的私有网络 HTTP 阻断当作策略设置，不把它当作每个工具或子进程都已完全隔离的证明。

## Secrets

Provider 和渠道凭据可能保存在 settings.yaml 或本地数据库中。我会优先使用环境
变量引用、保护工作目录，并避免提交它。日志和 Memory 也可能包含 Prompt、路径、
工具参数和响应。

## 远程使用

默认回环监听是我最安全的运行模式。远程访问需要明确认证和可信网络边界，详见
[远程访问](remote-access.md)。
