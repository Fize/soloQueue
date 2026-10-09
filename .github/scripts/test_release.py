import io
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import urllib.error
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
                             "deploy/docker/Dockerfile", ".dockerignore",
                             ".github/workflows/release.yml"]:
                    before = release.fingerprint()
                    commit(path, "changed")
                    self.assertNotEqual(before, release.fingerprint(), path)
            finally:
                os.chdir(previous)


if __name__ == "__main__":
    unittest.main()
