# soloqueue-decide protocol, version 1

NDJSON over stdin/stdout. stdout carries protocol frames only; diagnostics go to stderr. The worker
never touches the network and never logs input text.

## Startup

```
soloqueue-decide --model-dir ABS --runtime ABS [--device cpu] [--variant NAME]
```

`--model-dir` and `--runtime` must be absolute existing paths. `--device` accepts `cpu` only.
The first frame on stdout is the ready frame:

```json
{"v":1,"id":0,"data":{"variant":"laya-multilingual-fp32","maxLen":1024,"headMaxLen":256,"backend":"cpu"},"error":null}
```

`maxLen` and `headMaxLen` are read from the checkpoint's `rl_agent_config.json` (defaults 512 / 192
when absent). Startup failures exit non-zero with a message on stderr and no frame.

## Request

```json
{"v":1,"id":7,"op":"decide","text":"帮我把这个接口的错误处理重构一下"}
```

| field | rule |
| --- | --- |
| `v` | must be `1` |
| `id` | non-zero `u64`, echoed in the response; `0` is reserved for the ready frame and unsolicited errors |
| `op` | `decide` or `shutdown` |
| `text` | required for `decide`, non-empty, ≤ 64 KiB |

Unknown fields are rejected. A frame longer than 1 MiB is rejected without being parsed.

## Response

```json
{"v":1,"id":7,"data":{"choice":"engineering","probabilities":{"general":0.01,"engineering":0.97,"research":0.02}},"error":null}
```

`shutdown` answers `{"v":1,"id":N,"data":{"stopped":true},"error":null}` and exits cleanly; EOF on
stdin also exits cleanly.

## Errors

```json
{"v":1,"id":7,"data":null,"error":{"code":"truncated","message":"..."}}
```

| code | meaning |
| --- | --- |
| `bad_json` | malformed request line (recoverable) |
| `bad_version` | `v` is not 1 |
| `bad_request` | unknown `op`, empty `text`, or an unreadable question set |
| `too_large` | frame over 1 MiB, or `text` over 64 KiB |
| `truncated` | the options no longer fit the token budget, so no marker survives |
| `model_error` | ONNX Runtime or the engine failed |

An error never terminates the process except at startup.

## Options

The three labels and their descriptions are compiled into the worker and must stay identical to the
Go-side definition in `internal/router/laya`; the question is not sent per request, so a mismatch is
a silent accuracy loss rather than a protocol error. Changing them means rebuilding the worker.
