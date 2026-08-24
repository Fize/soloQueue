# Web Console E2E 回归测试

`e2e/test_web_console.py` 是一个不依赖 pytest 的 Python Playwright 浏览器回归套件。它用真实 Chromium 访问 Vite Web Console，并检查页面导航、响应式能力边界、PWA 安装入口和关键输入交互。

## 运行

先安装 Python Playwright 与 Chromium，然后在仓库根目录执行：

```bash
python3 -m pip install playwright
python3 -m playwright install chromium
python3 /Users/xiaobaitu/.agents/skills/webapp-testing/scripts/with_server.py \
  --server "go run ./cmd/soloqueue serve --port 8765 --bypass" --port 8765 \
  --server "cd web && pnpm dev --host 127.0.0.1" --port 5173 \
  -- python3 e2e/test_web_console.py
```

脚本会等待双服务就绪，默认以 headless Chromium 运行，并输出每个场景的 `PASS`、`FAIL` 或 `BLOCKED`。失败时会将截图写入 `e2e-artifacts/`；可用 `E2E_ARTIFACT_DIR=/tmp/soloqueue-e2e` 改变目录。`E2E_BASE_URL` 或 `--base-url` 可覆盖前端地址。

## 覆盖矩阵

| 区域 | 场景 |
| --- | --- |
| Shell/PWA | 根路由、标题、manifest、`sw.js`、无控制台错误/请求失败/HTTP 5xx |
| 导航 | Assistant、Simulations、Workflows、Cron Jobs、Usage Statistics、Settings |
| Chat | 输入框可见、文本输入、键盘操作不触发发送 |
| Phone | 390×844 隐藏 Design Mode，输入仍可用 |
| Pad | 999×800 为 single；1000×800 为 split |
| Desktop | 1280×800 为 split |
| 安装 | 合成 `beforeinstallprompt` 验证原生安装调用；手动安装指引验证桌面/移动说明、可访问对话框；`Not now` 在 reload 后保持隐藏 |
| L1 | 当前没有可复现的 delegated-session fixture，明确报告 `BLOCKED`，不伪造通过 |
| Service Worker | 开发服务器不注册 worker，生产静态服务器注册/离线验证明确报告 `BLOCKED`，不伪造通过 |

## 结果解释与限制

- `FAIL` 表示浏览器断言、控制台错误、请求失败或 5xx；进程以非零状态退出。
- `BLOCKED` 表示环境或 fixture 缺失，不计入失败，但必须在 CI 报告中保留。
- 该套件使用 Chromium；Safari/iOS 的安装提示、真实离线缓存和系统级“添加到主屏幕”仍需真机/设备矩阵补测。
- 开发服务器不会注册生产 Service Worker；本套件验证 `manifest.webmanifest`/`sw.js` 可访问及安装生命周期，生产离线缓存应在 `pnpm build` 后通过静态服务器再做一次设备验证。
