# 架构

[English: Architecture](../architecture.md)

我把当前架构概览放在[架构概览](architecture/overview.md)，并保留这个文件作为旧书签
的稳定入口。

我把产品边界定义为本地优先：Go 服务端拥有运行时状态，Electron 桌面客户端
或嵌入式 Portal 通过 HTTP 和 WebSocket 使用它。仓库根目录的
[AGENTS.md](../../AGENTS.md) 是维护者使用的构建和包路径说明。
