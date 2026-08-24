# SoloQueue PWA 安装与离线生命周期

## 目标与边界

SoloQueue 的 Web Console 使用浏览器原生 PWA 能力，让桌面和手机用户可以把当前 Web Console 安装到主屏幕或应用列表，并在网络短暂不可用时继续打开已缓存的应用壳。手机可以安装和使用 SoloQueue；Design Mode 仍只由 Pad 能力判断开放。本阶段不改后端、API、数据库或业务页面的全面响应式布局。

## 生命周期

`main.tsx` 仅在 Vite 生产构建中调用 `registerServiceWorker()`，注册失败不会阻塞 React 启动。`usePWAInstall` 在组件挂载时读取 standalone 状态和本地 dismissal 标记，并监听两个浏览器事件：

- `beforeinstallprompt`：保存浏览器提供的一次性事件，状态变为 `available`，用户点击安装时调用 `prompt()`，随后读取 `userChoice`；用户拒绝后不自动再次弹出。
- `appinstalled`：状态变为 `installed`，安装提示永久隐藏。

已经以 standalone/display-mode 启动的窗口（包括 iOS 的 `navigator.standalone`）直接视为 `installed`。没有原生安装提示但支持 Service Worker 的浏览器进入 `manual`，安装按钮展示通用桌面/移动端浏览器指引；不支持 Service Worker 且没有原生提示的浏览器进入 `unsupported`。用户关闭提示会把 dismissal 标记写入 localStorage，避免每次启动重复打扰。存储不可用时只影响持久化，不影响主应用启动。

## 缓存边界

根路径的 `sw.js` 使用带版本号的 cache 名称。安装阶段只预缓存 `/` 与 `/index.html`；激活阶段删除旧版本 cache 并接管页面。导航请求采用 network-first，网络失败时回退到缓存的 `/index.html`。同源 GET 的构建静态资源采用 cache-first，并把成功响应写入当前 cache。

Service Worker 明确绕过非 GET、`/api/`、`/ws` 以及所有跨源请求，因此不会缓存 API 结果、WebSocket 握手、用户写操作或第三方资源。离线能力仅覆盖已经成功加载过的应用壳和静态资源；聊天数据、认证状态、实时连接和写操作仍要求后端与网络，不提供离线写队列或冲突解决。

## 安装入口差异

Chromium 等支持 `beforeinstallprompt` 的浏览器显示原生安装按钮。Safari/iOS 通常没有该事件，使用“安装指南”说明分享菜单中的“添加到主屏幕”；桌面 Safari 或其他支持 manifest/Service Worker 但没有原生事件的浏览器使用其浏览器菜单中的“添加到 Dock/主屏幕/安装”入口。`manifest.webmanifest`、theme metadata、Apple meta 标签和 `apple-touch-icon` 同时覆盖 Chromium 与 iOS 入口。

## 更新行为

每次发布递增 Service Worker cache 版本。新 Worker 安装后预缓存应用壳，激活时清理旧版本；页面下次导航或刷新即可使用新资源。缓存更新不触碰 API 或 WebSocket，也不改变后端会话。

## 非目标

- 不打包 APK、DMG、App Store 或其他原生安装包。
- 不引入 PWA 插件或额外运行时依赖。
- 不把业务数据、API 响应、WebSocket 或非 GET 请求写入 Cache Storage。
- 不实现离线业务写入队列、重放、冲突合并或离线认证。
- 不把手机开放到 Design Mode，也不做所有业务页面的全面响应式迁移。
