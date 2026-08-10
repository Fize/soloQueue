# CLI 参考

[English: CLI reference](../../reference/cli.md)

我使用 soloqueue 作为主命令。运行 soloqueue --help 可以查看当前检出版本的
命令树。

## 服务端

~~~bash
soloqueue serve [flags]
~~~

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| --port, -p | 57647 | HTTP 端口；0 表示随机端口 |
| --host | 127.0.0.1 | 绑定地址 |
| --verbose, -v | false | 把日志写到 stderr |
| --bypass | false | 绕过所有工具确认 |

## Version

~~~bash
soloqueue version
~~~

打印应用版本。

## Skills

~~~bash
soloqueue skills report [--work-dir path] [--db path] [--days 30]
~~~

以 JSON 输出安装 Skill 的治理信息，包括调用时间窗口和描述质量。

## Memory

~~~bash
soloqueue memory audit [--db path]
soloqueue memory cleanup [--db path] [--manifest path] [--project-root path] [--apply]
~~~

Audit 是只读操作。Cleanup 会写入 Manifest；--apply 会先备份数据库，再应用计划。

## 微信

~~~bash
soloqueue wechat login [--id default] [--name WeChat] [--bind-type l1|l2] [--bind-agent id]
~~~

我用这个命令打印二维码 URL、轮询 iLink、按需读取验证码，并把确认后的账号保存到
settings.yaml。
