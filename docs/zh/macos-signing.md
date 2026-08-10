# macOS 固定自签名（维护者参考）

[English: macOS signing](../macos-signing.md)

我使用这篇文档打包本地 macOS 构建，不把它当作终端用户安装指南；它也不提供 Apple
Developer ID 签名或 notarization。

我使用固定的应用标识 com.soloqueue 和名为 SoloQueue Code Signing 的
固定自签名身份。首次设置、检查、迁移和打包命令与英文版本相同；我会把 PKCS#12
私钥备份放在受保护的位置，绝不会提交或随应用分发。
