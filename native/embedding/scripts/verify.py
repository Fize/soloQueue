#!/usr/bin/env python3
"""Compare real worker embeddings against the pinned official CPU reference."""
import argparse
import json
from pathlib import Path
import subprocess

BASE_TEXTS = ["中文记忆：下周三讨论项目。", "The meeting is on Wednesday.", "中 English 混合🙂🚀 café e\u0301", "", " \t\n  ", "query: literal text", "记忆测试 " * 600]


def boundary_texts(tokenizer, prefix):
    # Discover exact boundaries through the official tokenizer; prefix lengths differ.
    required = (510, 511, 512, 513)
    found = {}
    for count in range(1, 514):
        text = "a " * count
        raw_count = len(tokenizer(prefix + text, add_special_tokens=True, verbose=False)["input_ids"])
        if raw_count in required:
            found[raw_count] = text
        if len(found) == len(required):
            break
    assert set(found) == set(required), "could not construct exact token boundaries"
    return [found[count] for count in required]


def frame(process, request):
    process.stdin.write(json.dumps(request, ensure_ascii=False) + "\n")
    process.stdin.flush()
    line = process.stdout.readline()
    if not line:
        raise AssertionError("worker exited without response")
    return json.loads(line)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker", type=Path, required=True)
    parser.add_argument("--model-dir", type=Path, required=True)
    parser.add_argument("--reference-dir", type=Path)
    parser.add_argument("--runtime", type=Path, required=True)
    parser.add_argument("--fixture-output", type=Path)
    parser.add_argument("--device", choices=["cpu", "auto"], default="cpu")
    args = parser.parse_args()
    import numpy as np
    import torch
    from transformers import AutoModel, AutoTokenizer
    torch.set_num_threads(2)
    reference = args.reference_dir or args.model_dir
    tokenizer = AutoTokenizer.from_pretrained(reference, local_files_only=True)
    model = AutoModel.from_pretrained(reference, local_files_only=True).eval().cpu()
    command = [str(args.worker.resolve()), "--model-dir", str(args.model_dir.resolve()), "--runtime", str(args.runtime.resolve()), "--device", args.device]
    with subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, encoding="utf-8") as process:
        try:
            ready = json.loads(process.stdout.readline())
            assert ready["version"] == 1 and ready["id"] == 0 and ready["error"] is None
            assert ready["data"]["model"] == "intfloat/multilingual-e5-small"
            assert ready["data"]["dimension"] == 384
            assert ready["data"]["backend"] in ("cpu", "coreml", "cuda", "directml")
            if args.device == "cpu":
                assert ready["data"]["backend"] == "cpu"
            # Recoverable schema/JSON/frame errors must leave the worker usable.
            process.stdin.write("{invalid\n"); process.stdin.flush()
            assert json.loads(process.stdout.readline())["error"]["code"] == "invalid_request"
            assert frame(process, {"version": 1, "id": 12, "op": "embed", "purpose": "other", "texts": ["x"]})["error"]["code"] == "invalid_request"
            worst = 1.0
            fixtures = []
            for request_id, purpose in enumerate(("query", "document"), start=1):
                prefix = "query: " if purpose == "query" else "passage: "
                texts = BASE_TEXTS + boundary_texts(tokenizer, prefix)
                response = frame(process, {"version": 1, "id": request_id, "op": "embed", "purpose": purpose, "texts": texts})
                assert response["id"] == request_id and response["error"] is None, response.get("error")
                results = response["data"]["results"]
                assert len(results) == len(texts)
                for text, result in zip(texts, results):
                    prefixed = ("query: " if purpose == "query" else "passage: ") + text
                    raw_count = len(tokenizer(prefixed, add_special_tokens=True, verbose=False)["input_ids"])
                    encoded = tokenizer(prefixed, max_length=512, padding="max_length", truncation=True, return_tensors="pt")
                    fixtures.append({"text": text, "purpose": purpose, "input_ids": encoded["input_ids"][0].tolist(), "attention_mask": encoded["attention_mask"][0].tolist(), "token_type_ids": encoded.get("token_type_ids", torch.zeros_like(encoded["input_ids"]))[0].tolist(), "truncated": raw_count > 512})
                    with torch.no_grad():
                        hidden = model(**encoded).last_hidden_state
                        mask = encoded["attention_mask"].unsqueeze(-1)
                        vector = (hidden * mask).sum(dim=1) / mask.sum(dim=1)
                        expected = torch.nn.functional.normalize(vector, p=2, dim=1).numpy()[0]
                    actual = np.asarray(result["embedding"], dtype=np.float32)
                    assert actual.shape == (384,) and np.isfinite(actual).all()
                    assert abs(float(np.linalg.norm(actual)) - 1) < 1e-5
                    assert result["tokens"] == int(encoded["attention_mask"].sum())
                    assert result["truncated"] == (raw_count > 512)
                    cosine = float(np.dot(actual, expected) / (np.linalg.norm(actual) * np.linalg.norm(expected)))
                    assert cosine >= 0.9999, (purpose, cosine)
                    worst = min(worst, cosine)
                print(json.dumps({"purpose": purpose, "samples": len(results), "backend": response["data"]["backend"], "fallbackReason": response["data"].get("fallbackReason")}), flush=True)
            if args.fixture_output:
                args.fixture_output.write_text(json.dumps(fixtures))
            shutdown = frame(process, {"version": 1, "id": 99, "op": "shutdown"})
            assert shutdown["id"] == 99 and shutdown["error"] is None
            process.stdin.close()
            assert process.wait(timeout=30) == 0
            print(json.dumps({"status": "pass", "device": args.device, "minCosine": worst}), flush=True)
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=30)

if __name__ == "__main__":
    main()
