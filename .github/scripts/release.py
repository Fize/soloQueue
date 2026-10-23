"""Small, stdlib-only helpers for the tag release workflow."""

import base64
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import zipfile
import shutil


def metadata(tag, repository):
    # Build metadata (+...) is excluded because '+' is not a Docker tag character.
    number = r"(?:0|[1-9][0-9]*)"
    identifier = r"(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
    if not re.fullmatch(rf"v{number}\.{number}\.{number}(?:-{identifier}(?:\.{identifier})*)?", tag):
        raise ValueError("Expected vMAJOR.MINOR.PATCH[-PRERELEASE], without build metadata")
    if len(tag) > 128:
        raise ValueError("Version exceeds Docker's 128-character tag limit")
    return {"version": tag[1:], "prerelease": str("-" in tag).lower(),
            "image": "ghcr.io/" + repository.lower()}


def fingerprint():
    # Exact tracked trees copied by Dockerfile, plus the recipe and CI tooling.
    # Root docs, deploy docs, and skills are not Docker build inputs.
    paths = ["cmd", "internal", "web", "status-ui", "go.mod", "go.sum", "LICENSE",
             "native/embedding/Cargo.toml", "native/embedding/Cargo.lock", "native/embedding/src",
             "native/decide/Cargo.toml", "native/decide/Cargo.lock", "native/decide/src", "native/decide/vendor",
             "deploy/docker/Dockerfile", ".dockerignore", ".github"]
    tree = subprocess.check_output(["git", "ls-tree", "-r", "-z", "HEAD", "--", *paths])
    return hashlib.sha256(tree).hexdigest()


def native_licenses(metadata_paths, output_path):
    """Preserve the license/notice files shipped by every pinned Rust crate."""
    if isinstance(metadata_paths, (str, Path)):
        metadata_paths = [metadata_paths]
    packages = [package for metadata_path in metadata_paths
                for package in json.loads(Path(metadata_path).read_text(encoding="utf-8"))["packages"]]
    sections = []
    seen = set()
    for package in sorted(packages, key=lambda p: (p["name"], p["version"])):
        identity = (package["name"], package["version"])
        if identity in seen:
            continue
        seen.add(identity)
        root = Path(package["manifest_path"]).parent
        # Vendored native code can carry additional notices (e.g. Oniguruma's
        # COPYING). Registry packages have no build output; avoid recursively
        # walking the workspace package's target cache.
        candidates = root.rglob("*") if package.get("source") else root.iterdir()
        files = {path for path in candidates if path.is_file() and any(
            part.lower().startswith(("license", "licence", "copying", "notice"))
            for part in path.relative_to(root).parts)}
        if package.get("license_file"):
            files.add(root / package["license_file"])
        heading = f'{package["name"]} {package["version"]}: {package.get("license") or "see license files"}'
        sections.append(heading + "\n" + "=" * len(heading))
        for path in sorted(files):
            sections.append(str(path.relative_to(root)) + "\n" + path.read_text(encoding="utf-8"))
    Path(output_path).write_text("\n\n".join(sections) + "\n", encoding="utf-8")


def archive(version, system, arch, binary, worker, license_metadata, output_dir, decide=None):
    """Package only the app, its optional sibling worker, and required notices."""
    metadata("v" + version, "unused/unused")
    supported = {("darwin", "arm64"), ("linux", "amd64"), ("windows", "amd64")}
    legacy = (system, arch) == ("darwin", "amd64")
    if (system, arch) not in supported and not legacy:
        raise ValueError("unsupported release platform")
    if not legacy and (not worker or not decide or not license_metadata):
        raise ValueError("local ONNX releases require both workers and license metadata")
    if legacy and (worker or decide or license_metadata):
        raise ValueError("macOS Intel release does not support the pinned local ONNX runtime")
    suffix = ".exe" if system == "windows" else ""
    output = Path(output_dir)
    output.mkdir(parents=True, exist_ok=True)
    name = f"soloqueue_{version}_{system}_{arch}"
    with tempfile.TemporaryDirectory() as temporary:
        stage = Path(temporary)
        shutil.copyfile(binary, stage / ("soloqueue" + suffix))
        shutil.copyfile(Path(__file__).resolve().parents[2] / "LICENSE", stage / "LICENSE")
        if worker:
            shutil.copyfile(worker, stage / ("soloqueue-embedding" + suffix))
            native_licenses(license_metadata, stage / "THIRD_PARTY_LICENSES.txt")
        if decide:
            shutil.copyfile(decide, stage / ("soloqueue-decide" + suffix))
        else:
            (stage / "LOCAL_ONNX_UNSUPPORTED.txt").write_text(
                "Local ONNX embedding is unavailable on macOS Intel: no official ONNX Runtime 1.24.2 package.\n"
                "Other SoloQueue features and remote embedding providers remain available.\n", encoding="utf-8")
        if system == "windows":
            destination = output / (name + ".zip")
            with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED) as bundle:
                for path in sorted(stage.iterdir()):
                    bundle.write(path, path.name)
        else:
            destination = output / (name + ".tar.gz")
            with tarfile.open(destination, "w:gz") as bundle:
                for path in sorted(stage.iterdir()):
                    info = bundle.gettarinfo(str(path), arcname=path.name)
                    info.mode = 0o755 if path.name in {"soloqueue", "soloqueue-embedding", "soloqueue-decide"} else 0o644
                    with path.open("rb") as content:
                        bundle.addfile(info, content)
    return destination


def checksums(directory):
    root = Path(directory)
    archives = sorted([*root.glob("*.tar.gz"), *root.glob("*.zip")])
    if not archives:
        raise ValueError("no release archives")
    lines = []
    for path in archives:
        digest = hashlib.sha256()
        with path.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
        lines.append(f"{digest.hexdigest()}  {path.name}\n")
    (root / "SHA256SUMS").write_text("".join(lines), encoding="utf-8")


def manifest_digest(image, tag, actor, password, opener=urllib.request.urlopen):
    repository = image.removeprefix("ghcr.io/")
    # Push scope permits GHCR to authorize creation of a not-yet-existing package.
    # A pull-only token can be denied before we can observe its manifest 404.
    query = urllib.parse.urlencode({"service": "ghcr.io", "scope": f"repository:{repository}:pull,push"})
    credentials = base64.b64encode(f"{actor}:{password}".encode()).decode()
    request = urllib.request.Request("https://ghcr.io/token?" + query,
                                     headers={"Authorization": "Basic " + credentials})
    # Authentication and network errors must fail, never masquerade as a miss.
    with opener(request, timeout=30) as response:
        token = json.load(response)["token"]
    request = urllib.request.Request(f"https://ghcr.io/v2/{repository}/manifests/{tag}",
        method="HEAD", headers={"Authorization": "Bearer " + token,
        "Accept": ", ".join(["application/vnd.oci.image.index.v1+json",
            "application/vnd.oci.image.manifest.v1+json",
            "application/vnd.docker.distribution.manifest.list.v2+json",
            "application/vnd.docker.distribution.manifest.v2+json"])})
    try:
        with opener(request, timeout=30) as response:
            digest = response.headers.get("Docker-Content-Digest", "")
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return ""
        raise
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise ValueError("Registry returned no valid manifest digest")
    return digest


def output(values):
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as stream:
        for key, value in values.items():
            stream.write(f"{key}={value}\n")


if __name__ == "__main__":
    if sys.argv[1] == "metadata":
        output(metadata(os.environ["GITHUB_REF_NAME"], os.environ["GITHUB_REPOSITORY"]))
    elif sys.argv[1] == "image":
        cache_tag = "build-" + fingerprint()
        digest = manifest_digest(os.environ["IMAGE"], cache_tag,
                                 os.environ["GITHUB_ACTOR"], os.environ["GH_TOKEN"])
        output({"cache_tag": cache_tag, "digest": digest,
                "build": str(not digest).lower()})
    elif sys.argv[1] == "licenses":
        native_licenses(sys.argv[2:-1], sys.argv[-1])
    elif sys.argv[1] == "checksums":
        checksums(sys.argv[2])
    elif sys.argv[1] == "archive":
        parser = argparse.ArgumentParser()
        parser.add_argument("--version", required=True)
        parser.add_argument("--os", dest="system", required=True)
        parser.add_argument("--arch", required=True)
        parser.add_argument("--binary", type=Path, required=True)
        parser.add_argument("--worker", type=Path)
        parser.add_argument("--decide", type=Path, help="local task classifier worker")
        parser.add_argument("--license-metadata", type=Path, nargs="+")
        parser.add_argument("--output-dir", type=Path, required=True)
        print(archive(**vars(parser.parse_args(sys.argv[2:]))))
    else:
        raise ValueError("Unknown release command")
