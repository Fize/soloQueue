# SoloQueue Backend — End-to-End Demo

> 本文演示 `soloqueue serve` 的完整交互：启动 → 创建 session → WebSocket
> 订阅流式事件 → 查历史 → 删除 session。

## 前置

- Go 1.25+（与 `go.mod` 对齐）
- `curl` + `jq`（查 session id）
- `websocat`（可选；用于 WS 手动验证）
- `DEEPSEEK_API_KEY` 环境变量（否则 agent 可以 Start，但任何 Ask 会失败）
- 可选：`TAVILY_API_KEY`（启用 `web_search` 工具；缺省时 `Build()` 跳过它）

## 1. 启动服务

```bash
# 终端 A
export DEEPSEEK_API_KEY=sk-xxx
export TAVILY_API_KEY=tvly-xxx  # 可选
go run ./cmd/soloqueue serve --port 8765
```

**日志示例**：

```
time=2026-04-22T17:00:00+08:00 level=INFO msg="soloqueue serve starting" category=app host=127.0.0.1 port=8765 version=0.1.0
time=2026-04-22T17:00:00+08:00 level=INFO msg="server listening" category=app addr=127.0.0.1:8765 tools=10
soloqueue serve listening on 127.0.0.1:8765
```

`tools=10` 表明 10 个内置工具都注册成功；若未设 `TAVILY_API_KEY`，会是 `tools=9`。

## 2. REST 入口

### 2.1 健康检查

```bash
curl -s http://127.0.0.1:8765/healthz
# {"status":"ok"}
```

### 2.2 创建 session

```bash
SID=$(curl -s -XPOST http://127.0.0.1:8765/v1/sessions \
  -d '{"team_id":"demo"}' | jq -r .session_id)
echo $SID
# A1B2c3D4... (32 hex chars)
```

### 2.3 查历史（最初为空）

```bash
curl -s http://127.0.0.1:8765/v1/sessions/$SID/history
# {"messages":[]}
```

### 2.4 删除 session

```bash
curl -s -w "HTTP %{http_code}\n" -XDELETE http://127.0.0.1:8765/v1/sessions/$SID
# HTTP 204
```

## 3. WebSocket 订阅流式事件

连上 WebSocket：

```bash
# 终端 B
websocat ws://127.0.0.1:8765/v1/sessions/$SID/stream
```

### 3.1 一次 `ask` 的帧序列

**client → server**

```json
{"type":"ask","prompt":"Read README.md and summarize in one sentence."}
```

**server → client**（示意顺序；具体会因 LLM 决策而异）

```json
{"type":"content_delta","iter":0,"delta":"I'll start by reading the README."}
{"type":"tool_call_delta","iter":0,"call_id":"c1","name":"file_read","args_delta":"{\"path"}
{"type":"tool_call_delta","iter":0,"call_id":"c1","name":"","args_delta":"\":\"README.md\"}"}
{"type":"iteration_done","iter":0,"finish_reason":"tool_calls","usage":{"prompt_tokens":...}}
{"type":"tool_exec_start","iter":0,"call_id":"c1","name":"file_read","args":"{\"path\":\"README.md\"}"}
{"type":"tool_exec_done","iter":0,"call_id":"c1","name":"file_read","result":"{\"path\":\"...\",\"content\":\"...\"}","err":"","duration_ms":3}
{"type":"content_delta","iter":1,"delta":"The README describes"}
{"type":"content_delta","iter":1,"delta":" SoloQueue as a ..."}
{"type":"iteration_done","iter":1,"finish_reason":"stop","usage":{...}}
{"type":"done","content":"The README describes SoloQueue as a ... tool."}
```

### 3.2 取消流式 ask

```json
{"type":"cancel"}
```

随后服务器会返回：

```json
{"type":"error","err":"context canceled"}
```

Session 仍然活着；你可以再发 `{"type":"ask", ...}` 继续对话。

### 3.3 心跳 / 探活

```json
{"type":"ping"}
```

服务器回：

```json
{"type":"pong"}
```

## 4. 工具并发演示（推荐）

在 `~/.soloqueue/settings.json` 里打开 `shell_exec` 白名单：

```json
{
  "tools": {
    "shellAllowRegexes": ["^echo\\s", "^sleep\\s+[0-9]+$"],
    "httpAllowedHosts": ["example.com"]
  }
}
```

然后提问：

> "Run these three commands in parallel and tell me how long each took: `sleep 1`, `sleep 2`, `sleep 3`."

日志里可以看到 **`tool_exec_start` 的时间戳高度重叠**（并发生效）：

```
time=... msg="tool exec start" category=tool tool_name=shell_exec tool_call_id=c1 ...
time=... msg="tool exec start" category=tool tool_name=shell_exec tool_call_id=c2 ...  (30ms 后)
time=... msg="tool exec start" category=tool tool_name=shell_exec tool_call_id=c3 ...  (60ms 后)
```

而总耗时 ≈ `max(1,2,3) s`，不是 6s。

## 5. 观察日志

每个 session 在 `~/.soloqueue/logs/sessions/<teamID>/<sessionID>/` 下有：

```
actor.jsonl     # agent 生命周期 + job 事件
llm.jsonl       # LLM 请求 / 响应（含 token 统计）
tool.jsonl      # 工具调用（含 trace_id 串联）
messages.jsonl  # （预留）
```

同一次 Ask 的所有日志都带 **同一 `trace_id`**，可用 `jq` 聚合：

```bash
jq 'select(.trace_id=="<paste-trace-id>")' \
   ~/.soloqueue/logs/sessions/demo/*/tool.jsonl
```

## 6. 验收清单

- [ ] `curl /healthz` 返回 `{"status":"ok"}`
- [ ] `POST /v1/sessions` 返回合法 32 字节 hex `session_id`
- [ ] `DELETE /v1/sessions/{id}` 返回 204；`Count()` 下降到 0
- [ ] WebSocket 订阅后 `ask` 能收到 `content_delta` → `done` 完整序列
- [ ] `cancel` 能在 <1s 打断慢 tool（配合白名单的 `sleep 10`）
- [ ] 并发场景下 `tool_exec_start` 时间戳重叠、总耗时 ≈ max
- [ ] 所有日志一致带 `trace_id`

## 7. 常见问题

| 现象 | 原因 | 解决 |
|---|---|---|
| 启动时 `LLM API key not set` | `DEEPSEEK_API_KEY` 未导出 | `export DEEPSEEK_API_KEY=sk-xxx` |
| `tools=9` 而非 10 | `TAVILY_API_KEY` 未设 | 设环境变量或忽略（web_search 可选） |
| Shell 命令总被拒 | 白名单为空 | settings.json 里加 `shellAllowRegexes` |
| `http_fetch` 返回 `private address` | URL 指向环回 / 内网 | 把 `httpBlockPrivate` 关掉（仅本地调试）或换公网 URL |
| WS 连接后立刻断 | session 不存在 | 检查 `session_id` 是否已被 delete |
