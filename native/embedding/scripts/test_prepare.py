import hashlib
import io
import shutil
import tarfile
from unittest.mock import patch
import prepare
from pathlib import Path
import tempfile
import unittest
import zipfile
from prepare import digest, download, unpack

class PreparationTests(unittest.TestCase):
    def test_verified_cache(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "cached"
            path.write_bytes(b"verified")
            expected = hashlib.sha256(b"verified").hexdigest()
            download("invalid://must-not-be-requested", path, expected)
            self.assertEqual(digest(path), expected)

    def test_runtime_ignores_dsym_duplicate_basename(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            archive = root / "fixture.tgz"
            stem = "onnxruntime-osx-arm64-1.24.2"
            library = "libonnxruntime.1.24.2.dylib"
            with tarfile.open(archive, "w:gz") as output:
                for name in [f"{stem}/lib/{library}", f"{stem}/lib/{library}.dSYM/Contents/Resources/DWARF/{library}"]:
                    data = b"native runtime" if ".dSYM" not in name else b"debug symbols"
                    info = tarfile.TarInfo(name)
                    info.size = len(data)
                    output.addfile(info, io.BytesIO(data))
            def cached_download(url, destination, sha):
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(archive, destination)
            with patch.object(prepare.platform, "system", return_value="Darwin"), patch.object(prepare.platform, "machine", return_value="arm64"), patch.object(prepare, "download", side_effect=cached_download):
                actual = prepare.prepare_runtime(root / "runtime", "cpu")
            self.assertEqual(actual, (root / "runtime" / stem / stem / "lib" / library).resolve())
            self.assertEqual(actual.read_bytes(), b"native runtime")

    def test_zip_traversal_rejected(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            archive = root / "unsafe.zip"
            with zipfile.ZipFile(archive, "w") as output:
                output.writestr("../escape", "bad")
            with self.assertRaises(ValueError):
                unpack(archive, root / "output")
            self.assertFalse((root / "escape").exists())

if __name__ == "__main__":
    unittest.main()
