{
  "version": 1,
  "transport": "UTF-8 NDJSON on stdin/stdout; logs stderr only; maximum input frame 1MiB; explicit request IDs; no network in worker",
  "startup": {
    "args": [
      "--model-dir PATH",
      "--runtime PATH",
      "--device auto|cpu (default auto)"
    ],
    "ready": {
      "version": 1,
      "id": 0,
      "data": {
        "model": "intfloat/multilingual-e5-small",
        "dimension": 384,
        "backend": "cpu|coreml|cuda|directml",
        "fallbackReason": "optional string"
      },
      "error": null
    }
  },
  "request": {
    "version": 1,
    "id": "nonzero u64",
    "op": "embed|shutdown",
    "purpose": "query|document (embed required)",
    "texts": "1..32 strings for embed, each <=65536 UTF-8 bytes"
  },
  "response": {
    "version": 1,
    "id": "matching request id",
    "data": {
      "backend": "actual execution provider",
      "fallbackReason": "optional string",
      "results": [
        {
          "embedding": "384 finite float32 values; unit L2 norm",
          "tokens": "actual effective mask count",
          "truncated": "boolean"
        }
      ]
    },
    "error": "null or {code:snake_case string,message:string}"
  },
  "model": {
    "repo": "intfloat/multilingual-e5-small",
    "revision": "614241f622f53c4eeff9890bdc4f31cfecc418b3",
    "file": "onnx/model.onnx",
    "tokenizer": "onnx/tokenizer.json",
    "maxTokens": 512,
    "pooling": "attention-mask mean pooling last_hidden_state",
    "normalization": "L2",
    "prefixes": {
      "query": "query: ",
      "document": "passage: "
    }
  },
  "backendPolicy": "auto attempts CoreML(macOS), CUDA(Linux/Windows), DirectML(Windows), then CPU. Require provider available, GPU-capable device path, successful warmup. On accelerated Run failure discard partial batch and retry entire request on CPU. Never fall back remote. Report actual selected provider; registration alone cannot claim GPU execution."
}
