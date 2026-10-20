import io
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import tarfile
import unittest
import urllib.error
import zipfile
from unittest.mock import patch

import release


class Response(io.BytesIO):
    def __init__(self, body=b"", headers=None):
        super().__init__(body)
        self.headers = headers or {}


class ReleaseTests(unittest.TestCase):
    def test_versions_and_dynamic_owner(self):
        self.assertEqual(release.metadata("v1.2.3", "Fize/SoloQueue"), {
            "version": "1.2.3", "prerelease": "false", "image": "ghcr.io/fize/soloqueue"})
        self.assertEqual(release.metadata("v0.1.0-rc.1", "Org/Repo")["prerelease"], "true")
        for tag in ["v1", "1.2.3", "v01.2.3", "v1.2.3-01", "v1.2.3+build", "v1.2.3;echo bad"]:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.metadata(tag, "Org/Repo")

    def lookup(self, result):
        with patch("urllib.request.urlopen") as opener:
            opener.side_effect = [Response(b'{"token":"test"}'), result]
            result = release.manifest_digest("ghcr.io/org/repo", "build-abc", "actor", "secret", opener)
            self.assertIn("pull%2Cpush", opener.call_args_list[0].args[0].full_url)
            return result

    def test_existing_image_reuses_digest(self):
        digest = "sha256:" + "a" * 64
        self.assertEqual(self.lookup(Response(headers={"Docker-Content-Digest": digest})), digest)

    def test_missing_image_builds(self):
        self.assertEqual(self.lookup(urllib.error.HTTPError("url", 404, "missing", {}, None)), "")

    def test_registry_failures_are_not_misses(self):
        for code in [401, 403, 429, 500, 503]:
            with self.subTest(code=code), self.assertRaises(urllib.error.HTTPError):
                self.lookup(urllib.error.HTTPError("url", code, "failure", {}, None))
        with self.assertRaises(urllib.error.URLError):
            self.lookup(urllib.error.URLError("offline"))
        with self.assertRaises(ValueError):
            self.lookup(Response())

    def test_token_failure_is_not_a_miss(self):
        with self.assertRaises(urllib.error.HTTPError):
            release.manifest_digest("ghcr.io/org/repo", "tag", "actor", "secret",
                opener=lambda *a, **k: (_ for _ in ()).throw(
                    urllib.error.HTTPError("url", 403, "denied", {}, None)))

    def test_fingerprint_ignores_docs_but_covers_build_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            previous = os.getcwd()
            try:
                os.chdir(directory)
                def git(*args):
                    return subprocess.check_output(["git", *args], stderr=subprocess.DEVNULL)
                git("init")
                git("config", "user.email", "test@example.invalid")
                git("config", "user.name", "Test")
                def commit(path, content):
                    Path(path).parent.mkdir(parents=True, exist_ok=True)
                    Path(path).write_text(content)
                    git("add", ".")
                    git("commit", "-m", "test")
                commit("go.mod", "module example.invalid/test")
                initial = release.fingerprint()
                commit("docs/release.md", "docs only")
                self.assertEqual(initial, release.fingerprint())
                for path in ["cmd/main.go", "internal/app.go", "web/src/app.tsx",
                             "status-ui/src/app.tsx", "web/pnpm-lock.yaml",
                             "LICENSE", "native/embedding/Cargo.toml", "native/embedding/Cargo.lock",
                             "native/embedding/src/main.rs",
                             "deploy/docker/Dockerfile", ".dockerignore",
                             ".github/workflows/release.yml"]:
                    before = release.fingerprint()
                    commit(path, "changed")
                    self.assertNotEqual(before, release.fingerprint(), path)
                before = release.fingerprint()
                commit("native/embedding/README.md", "worker docs only")
                self.assertEqual(before, release.fingerprint())
            finally:
                os.chdir(previous)

    def test_native_archives_include_sibling_worker_and_notices(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary, worker = root / "app", root / "worker"
            binary.write_bytes(b"app bytes")
            worker.write_bytes(b"worker bytes")
            crate = root / "crate"
            crate.mkdir()
            (crate / "LICENSE-MIT").write_text("A third party copyright notice")
            (crate / "vendor").mkdir()
            (crate / "vendor" / "COPYING").write_text("Vendored native code notice")
            manifest = root / "metadata.json"
            manifest.write_text(json.dumps({"packages": [{"name": "crate", "version": "1.0",
                "license": "MIT", "source": "registry+test", "manifest_path": str(crate / "Cargo.toml")}]}))
            for system, arch in [("darwin", "arm64"), ("linux", "amd64"), ("windows", "amd64")]:
                with self.subTest(system=system):
                    # Windows chmod cannot set Unix executable bits. Archive
                    # metadata must remain correct when chmod has no effect.
                    with patch.object(Path, "chmod", return_value=None):
                        bundle = release.archive("1.2.3", system, arch, binary, worker, manifest, root / "out")
                    suffix = ".exe" if system == "windows" else ""
                    if system == "windows":
                        with zipfile.ZipFile(bundle) as source:
                            files = {name: source.read(name) for name in source.namelist()}
                    else:
                        with tarfile.open(bundle) as source:
                            files = {member.name: source.extractfile(member).read() for member in source.getmembers()}
                            for name, mode in {"soloqueue": 0o755, "soloqueue-embedding": 0o755,
                                               "LICENSE": 0o644, "THIRD_PARTY_LICENSES.txt": 0o644}.items():
                                self.assertEqual(source.getmember(name).mode, mode, name)
                    self.assertEqual(set(files), {"soloqueue" + suffix, "soloqueue-embedding" + suffix,
                                                  "LICENSE", "THIRD_PARTY_LICENSES.txt"})
                    self.assertEqual(files["soloqueue-embedding" + suffix], b"worker bytes")
                    self.assertEqual(files["soloqueue" + suffix], b"app bytes")
                    self.assertIn(b"A third party copyright notice", files["THIRD_PARTY_LICENSES.txt"])
                    self.assertIn(b"Vendored native code notice", files["THIRD_PARTY_LICENSES.txt"])
            release.checksums(root / "out")
            lines = (root / "out" / "SHA256SUMS").read_text().splitlines()
            self.assertEqual(len(lines), 3)
            for line in lines:
                digest, name = line.split("  ")
                self.assertEqual(digest, hashlib.sha256((root / "out" / name).read_bytes()).hexdigest())

    def test_legacy_intel_archive_and_required_worker(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "app"
            binary.write_bytes(b"legacy")
            with patch.object(Path, "chmod", return_value=None):
                destination = release.archive("1.2.3", "darwin", "amd64", binary, None, None, root / "out")
            with tarfile.open(destination) as bundle:
                self.assertEqual(set(bundle.getnames()), {"soloqueue", "LICENSE", "LOCAL_ONNX_UNSUPPORTED.txt"})
                for name, mode in {"soloqueue": 0o755, "LICENSE": 0o644,
                                   "LOCAL_ONNX_UNSUPPORTED.txt": 0o644}.items():
                    self.assertEqual(bundle.getmember(name).mode, mode, name)
                self.assertIn(b"macOS Intel", bundle.extractfile("LOCAL_ONNX_UNSUPPORTED.txt").read())
            for system, arch in [("windows", "amd64"), ("darwin", "arm64"), ("linux", "amd64")]:
                with self.subTest(system=system), self.assertRaises(ValueError):
                    release.archive("1.2.3", system, arch, binary, None, None, root / "out")
            with self.assertRaises(ValueError):
                release.archive("1.2.3", "linux", "other", binary, None, None, root / "out")


if __name__ == "__main__":
    unittest.main()
