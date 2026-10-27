package speechinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const manifestName = "speech-manifest.json"

type manifest struct {
	Files map[string]string `json:"files"`
}

func fileDigest(dir, name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\:`) {
		return "", fmt.Errorf("invalid manifest filename")
	}
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("speech tool is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func WriteManifest(dir string, names []string) error {
	m := manifest{Files: make(map[string]string, len(names))}
	for _, name := range names {
		hash, err := fileDigest(dir, name)
		if err != nil {
			return err
		}
		m.Files[name] = hash
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), data, 0o644)
}

func VerifyInstallation(dir, executable string) error {
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if len(m.Files) == 0 || len(m.Files) > 256 || m.Files[executable] == "" {
		return fmt.Errorf("incomplete speech tool manifest")
	}
	for name, want := range m.Files {
		got, err := fileDigest(dir, name)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("speech tool checksum mismatch: %s", name)
		}
	}
	return nil
}
