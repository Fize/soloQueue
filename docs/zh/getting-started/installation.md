# 安装

[English: Installation](../../getting-started/installation.md)

我目前以源码形式分发 SoloQueue。通过本地构建，我可以得到 Go 服务端，
并在需要时构建 Electron 桌面客户端。

## 前置条件

- Go 1.25.8 或兼容的 1.25 版本。
- Node.js 和 pnpm。
- Git。
- 至少一个已启用 LLM Provider 的 API Key。

我使用 Makefile 为 Portal 和桌面包运行 pnpm approve-builds 以及依赖安装。

## 构建浏览器 Portal 和服务端

~~~bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue
make build
~~~

我运行这个命令来构建 portal/，把产物复制到 internal/server/dist/，复制内置
Skills，并在仓库根目录生成 soloqueue 二进制文件。

我启动服务：

~~~bash
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve
~~~

我默认使用 127.0.0.1:57647 监听。接下来我阅读
[第一次运行](first-run.md)。

## 运行桌面开发客户端

我会在两个终端中分别运行服务端和桌面客户端：

~~~bash
# 终端 1
go run ./cmd/soloqueue serve --port 8765 --verbose

# 终端 2
cd desktop
pnpm approve-builds
pnpm install
pnpm dev
~~~

我使用 Vite 开发服务器把 /api 和 /ws 转发到 8765 端口的后端。
我使用 make build-desktop 生成桌面 Web 资源，使用 make package-desktop
PLATFORM=mac（也可以选择 win/linux）调用 Electron Builder。

## 构建变体

| 命令 | 结果 |
| --- | --- |
| make build-web | 构建并复制 Portal 资源 |
| make build-go | 构建 Go 二进制，要求 Portal 资源已经存在 |
| make build | 构建 Portal 和 Go 二进制 |
| make build-desktop | 构建桌面渲染资源 |
| make build-all | 构建 Portal、Go 二进制和桌面渲染资源 |
| make package-desktop PLATFORM=mac | 构建 macOS 桌面包 |

macOS 签名属于维护者流程，见[macOS 签名](../macos-signing.md)。它不提供
Apple notarization。

## 源码开发提示

我可以直接运行 go run ./cmd/soloqueue serve 启动后端，但如果
internal/server/dist/ 不存在，我看到的嵌入式浏览器界面会是空白。需要 Portal 时，
我先运行 make build-web。
