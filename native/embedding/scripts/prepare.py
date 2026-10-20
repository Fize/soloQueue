#!/usr/bin/env python3
"""Prepare verified, immutable model and ONNX Runtime artifacts outside the repo."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import tarfile
import urllib.request
import zipfile

REVISION = "614241f622f53c4eeff9890bdc4f31cfecc418b3"
REPO = "intfloat/multilingual-e5-small"
RUNTIME_VERSION = "1.24.2"
FILES = {
    "onnx/model.onnx": "ca456c06b3a9505ddfd9131408916dd79290368331e7d76bb621f1cba6bc8665",
    "onnx/tokenizer.json": "0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39",
    "model.safetensors": "1a55775f53449dac10a2bcbc312469fac40b96d53198c407081a831f81c98477",
}
# Official GitHub release asset SHA-256 digests (v1.24.2).
RUNTIMES = {
    ("Darwin", "arm64", "cpu"): ("onnxruntime-osx-arm64-1.24.2.tgz", "0af4fa503e8ea285245b47ee42d0a7461b8156a81270857da0c1d4ecf858abde"),
    ("Darwin", "arm64", "auto"): ("onnxruntime-osx-arm64-1.24.2.tgz", "0af4fa503e8ea285245b47ee42d0a7461b8156a81270857da0c1d4ecf858abde"),
    ("Linux", "x86_64", "cpu"): ("onnxruntime-linux-x64-1.24.2.tgz", "43725474ba5663642e17684717946693850e2005efbd724ac72da278fead25e6"),
    ("Linux", "aarch64", "cpu"): ("onnxruntime-linux-aarch64-1.24.2.tgz", "6715b3d19965a2a6981e78ed4ba24f17a8c30d2d26420dbed10aac7ceca0085e"),
    ("Linux", "x86_64", "auto"): ("onnxruntime-linux-x64-gpu-1.24.2.tgz", "bcb42da041f42192e5579de175f7410313c114740a611e230afe9d79be65cc49"),
    ("Windows", "x86_64", "cpu"): ("onnxruntime-win-x64-1.24.2.zip", "8e3e9c826375352e29cb2614fe44f3d7a4b0ff7b8028ad7a456af9d949a7e8b0"),
}

def digest(path):
    value = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def download(url, destination, sha):
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.is_file() and digest(destination) == sha:
        return
    temp = destination.with_suffix(destination.suffix + ".part")
    try:
        with urllib.request.urlopen(url, timeout=120) as source, open(temp, "wb") as output:
            while chunk := source.read(1024 * 1024):
                output.write(chunk)
        if digest(temp) != sha:
            raise ValueError(f"checksum mismatch for {destination.name}")
        os.replace(temp, destination)
    finally:
        temp.unlink(missing_ok=True)


def prepare_model(root, reference):
    # Resolve metadata for immutable revision, verifying revision and small-file git blob IDs.
    with urllib.request.urlopen(f"https://huggingface.co/api/models/{REPO}/revision/{REVISION}?blobs=true", timeout=120) as stream:
        metadata = json.load(stream)
    if metadata["sha"] != REVISION:
        raise ValueError("unexpected model revision")
    entries = {entry["rfilename"]: entry for entry in metadata["siblings"]}
    files = ["onnx/model.onnx", "onnx/tokenizer.json"]
    if reference:
        files += ["model.safetensors", "config.json", "tokenizer_config.json", "special_tokens_map.json", "tokenizer.json", "sentencepiece.bpe.model"]
    for name in files:
        entry = entries[name]
        destination = root / name
        if name in FILES or "lfs" in entry:
            sha = FILES.get(name) or entry["lfs"]["sha256"]
            download(f"https://huggingface.co/{REPO}/resolve/{REVISION}/{name}", destination, sha)
            if destination.stat().st_size != entry["size"]:
                raise ValueError(f"size mismatch: {name}")
        else:
            # Git blob hashes identify non-LFS configuration files in the pinned tree.
            destination.parent.mkdir(parents=True, exist_ok=True)
            with urllib.request.urlopen(f"https://huggingface.co/{REPO}/resolve/{REVISION}/{name}", timeout=120) as stream:
                content = stream.read()
            blob = hashlib.sha1(f"blob {len(content)}\0".encode() + content).hexdigest()
            if blob != entry["blobId"]:
                raise ValueError(f"git checksum mismatch: {name}")
            destination.write_bytes(content)
    (root / "provenance.json").write_text(json.dumps({"repo": REPO, "revision": REVISION, "sha256": {name: digest(root / name) for name in files}}, indent=2))


def unpack(archive, target):
    # Verify all names before extracting; upstream symlinks remain inside archive root.
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as source:
            for name in source.namelist():
                if not (target / name).resolve().is_relative_to(target.resolve()):
                    raise ValueError("unsafe archive path")
            source.extractall(target)
    else:
        with tarfile.open(archive) as source:
            source.extractall(target, filter="data")


def prepare_directml(root):
    packages = [
        ("microsoft.ml.onnxruntime.directml", "1.24.2", "c9b8adb96dfb5578097bea42a7d9b7ff8f300fb3c3a6f3052fe5b702628ab681"),
        ("microsoft.ai.directml", "1.15.4", "4e7cb7ddce8cf837a7a75dc029209b520ca0101470fcdf275c1f49736a3615b9"),
    ]
    target = root / "directml-1.24.2-win-x64"
    target.mkdir(parents=True, exist_ok=True)
    for package, version, sha in packages:
        name = f"{package}.{version}.nupkg"
        archive = root / "archives" / name
        download(f"https://api.nuget.org/v3-flatcontainer/{package}/{version}/{name}", archive, sha)
        with zipfile.ZipFile(archive) as source:
            names = [name for name in source.namelist() if (name.startswith("runtimes/win-x64/native/") or name == "bin/x64-win/DirectML.dll") and name.lower().endswith(".dll")]
            for name in names:
                with source.open(name) as data, open(target / Path(name).name, "wb") as output:
                    shutil.copyfileobj(data, output)
    if not (target / "onnxruntime.dll").is_file() or not (target / "DirectML.dll").is_file():
        raise ValueError("missing DirectML runtime dependency")
    return (target / "onnxruntime.dll").resolve()


def prepare_runtime(root, flavor):
    machine = platform.machine().lower()
    machine = {"amd64": "x86_64", "arm64": "arm64" if platform.system() == "Darwin" else "aarch64"}.get(machine, machine)
    if platform.system() == "Windows" and machine == "x86_64" and flavor == "auto":
        return prepare_directml(root)
    key = (platform.system(), machine, flavor)
    if key not in RUNTIMES:
        raise ValueError(f"no pinned runtime for {key}; use explicit --runtime path with a compatible provider-capable Runtime 1.24+")
    name, sha = RUNTIMES[key]
    archive = root / "archives" / name
    download(f"https://github.com/microsoft/onnxruntime/releases/download/v{RUNTIME_VERSION}/{name}", archive, sha)
    target = root / name.removesuffix(".tgz").removesuffix(".zip")
    target.mkdir(parents=True, exist_ok=True)
    unpack(archive, target)
    filename = "onnxruntime.dll" if platform.system() == "Windows" else "libonnxruntime.1.24.2.dylib" if platform.system() == "Darwin" else "libonnxruntime.so.1.24.2"
    matches = list(target.rglob(filename))
    if len(matches) != 1:
        raise ValueError("runtime library missing or ambiguous")
    return matches[0].resolve()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--reference", action="store_true")
    parser.add_argument("--runtime-flavor", choices=["cpu", "auto"], default="auto")
    parser.add_argument("--model-only", action="store_true")
    args = parser.parse_args()
    root = args.output.resolve()
    repo_root = Path(__file__).resolve().parents[3]
    if root.is_relative_to(repo_root):
        parser.error("artifacts must be staged outside the repository")
    prepare_model(root / "model", args.reference)
    runtime = None if args.model_only else prepare_runtime(root / "runtime", args.runtime_flavor)
    print(json.dumps({"modelDir": str(root / "model"), "runtime": str(runtime) if runtime else None}))

if __name__ == "__main__":
    main()
