import hashlib
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
