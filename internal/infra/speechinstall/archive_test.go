package speechinstall

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestInstallArchiveVerifiesAndExtractsRequiredExecutable(t *testing.T) {
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	f, err := z.Create("Release/whisper-cli.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("executable"))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data.Bytes())
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data.Bytes())), Header: make(http.Header)}, nil
	})}
	root := t.TempDir()
	path, err := InstallArchive(context.Background(), client, Archive{URL: "https://example.invalid/archive.zip", SHA256: fmt.Sprintf("%x", sum), Directory: "whisper-test", Prefix: "Release/", Executable: "whisper-cli.exe"}, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "executable" {
		t.Fatalf("installed %q: %v", got, err)
	}
	if path != filepath.Join(root, "whisper-test", "whisper-cli.exe") {
		t.Fatalf("path %q", path)
	}
}

func TestInstallArchiveRejectsChecksumMismatchWithoutReplacingExistingFiles(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("not the archive")), Header: make(http.Header)}, nil
	})}
	root := t.TempDir()
	existing := filepath.Join(root, "whisper-test")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "whisper-cli.exe"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := InstallArchive(context.Background(), client, Archive{URL: "https://example.invalid/archive.zip", SHA256: strings.Repeat("0", 64), Directory: "whisper-test", Prefix: "Release/", Executable: "whisper-cli.exe"}, root)
	if err == nil {
		t.Fatal("checksum mismatch succeeded")
	}
	got, err := os.ReadFile(filepath.Join(existing, "whisper-cli.exe"))
	if err != nil || string(got) != "old" {
		t.Fatalf("existing installation changed: %q, %v", got, err)
	}
}

func TestInstallArchiveRepairsIncompleteVersionDirectory(t *testing.T) {
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	f, _ := z.Create("Release/whisper-cli.exe")
	_, _ = f.Write([]byte("replacement"))
	_ = z.Close()
	sum := sha256.Sum256(data.Bytes())
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data.Bytes())), Header: make(http.Header)}, nil
	})}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken", "old.dll"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := InstallArchive(context.Background(), client, Archive{URL: "https://example.invalid/archive.zip", SHA256: fmt.Sprintf("%x", sum), Directory: "broken", Prefix: "Release/", Executable: "whisper-cli.exe"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "replacement" {
		t.Fatalf("%q: %v", got, err)
	}
}

func TestVerifyInstallationDetectsCorruptedExecutable(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "whisper-cli.exe")
	if err := os.WriteFile(exe, []byte("correct"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(root, []string{"whisper-cli.exe"}); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstallation(root, "whisper-cli.exe"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("corrupt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInstallation(root, "whisper-cli.exe"); err == nil {
		t.Fatal("corrupt executable was accepted")
	}
}
