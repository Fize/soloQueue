package speechinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

const ModelRevision = "5359861c739e955e79d9a303bcbc70fb988958b1"

var ModelSHA256 = map[string]string{
	"tiny":   "be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21",
	"base":   "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe",
	"small":  "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b",
	"medium": "6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208",
}

func VerifyFile(path, sha string) error {
	expected, err := hex.DecodeString(sha)
	if err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("invalid checksum")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if !bytesEqual(hash.Sum(nil), expected) {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}

// DownloadVerified writes a temporary sibling and only publishes verified bytes.
func DownloadVerified(ctx context.Context, client *http.Client, url, sha, target string, maxBytes int64) error {
	expected, err := hex.DecodeString(sha)
	if err != nil || len(expected) != sha256.Size || maxBytes <= 0 {
		return fmt.Errorf("invalid download constraints")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".speech-model-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > maxBytes {
		return fmt.Errorf("model exceeds size limit")
	}
	if !bytesEqual(hash.Sum(nil), expected) {
		return fmt.Errorf("model checksum mismatch")
	}
	return os.Rename(temp, target)
}

type verifiedModel struct {
	info   os.FileInfo
	digest string
}

var verifiedModels sync.Map // map[string]verifiedModel

// VerifyModel caches successful checks only while the file identity, size and
// modification time remain unchanged.
func VerifyModel(path, model string) error {
	digest, ok := ModelSHA256[model]
	if !ok {
		return fmt.Errorf("unsupported speech model %q", model)
	}
	before, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("model is not a regular file")
	}
	if value, ok := verifiedModels.Load(path); ok {
		cached := value.(verifiedModel)
		if cached.digest == digest && os.SameFile(cached.info, before) && cached.info.Size() == before.Size() && cached.info.ModTime().Equal(before.ModTime()) {
			return nil
		}
	}
	if err := VerifyFile(path, digest); err != nil {
		return err
	}
	after, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("model changed during verification")
	}
	verifiedModels.Store(path, verifiedModel{info: after, digest: digest})
	return nil
}
