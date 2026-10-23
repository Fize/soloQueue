package onnxruntime

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDownloadHashOfflineAndCancellation(t *testing.T) {
	content := "verified artifact"
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:])
	calls := 0
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(content)), ContentLength: int64(len(content))}, nil
	})}
	path := filepath.Join(t.TempDir(), "model")
	url := "https://test.invalid/model"
	progress := func(string, int64, int64) {}
	if err := Download(context.Background(), client, path, "model", url, hash, progress); err != nil {
		t.Fatal(err)
	}
	client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		t.Error("verified offline cache requested network")
		return nil, errors.New("offline")
	})
	if err := Download(context.Background(), client, path, "model", url, hash, progress); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if err := os.WriteFile(path, []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("wrong"))}, nil
	})
	if err := Download(context.Background(), client, path, "model", url, hash, progress); err == nil {
		t.Fatal("bad hash accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "corrupted" {
		t.Fatal("unverified artifact replaced existing cache")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Download(ctx, client, path, "model", url, hash, progress); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	parts, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".download-*"))
	if len(parts) != 0 {
		t.Fatal("partial downloads left behind")
	}
}
func TestExtractRejectsTraversal(t *testing.T) {
	for _, name := range []string{"../escape", "/tmp/escape", "dir/../../escape", "C:\\escape"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "bad.tgz")
			file, _ := os.Create(archive)
			gz := gzip.NewWriter(file)
			out := tar.NewWriter(gz)
			data := []byte("bad")
			_ = out.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data))})
			_, _ = out.Write(data)
			_ = out.Close()
			_ = gz.Close()
			_ = file.Close()
			if err := extract(context.Background(), archive, filepath.Join(root, "output")); err == nil {
				t.Fatal("accepted traversal")
			}
		})
	}
}
func TestSupportedRuntimePolicy(t *testing.T) {
	embedding := []struct{ os, arch, contains string }{{"darwin", "arm64", "osx-arm64"}, {"linux", "amd64", "gpu"}, {"linux", "arm64", "aarch64"}, {"windows", "amd64", "directml"}}
	for _, test := range embedding {
		packages, library, err := artifacts("1.24.2", test.os, test.arch)
		if err != nil || len(packages) == 0 || library == "" || !strings.Contains(packages[0].name, test.contains) {
			t.Fatal(test, packages, library, err)
		}
	}
	decide := []struct{ os, arch, contains string }{{"darwin", "arm64", "osx-arm64-1.30.0"}, {"linux", "amd64", "linux-x64-1.30.0"}, {"linux", "arm64", "aarch64-1.30.0"}, {"windows", "amd64", "onnxruntime.1.30.0.nupkg"}}
	for _, test := range decide {
		packages, library, err := artifacts("1.30.0", test.os, test.arch)
		if err != nil || len(packages) == 0 || library == "" || !strings.Contains(packages[0].name, test.contains) {
			t.Fatal(test, packages, library, err)
		}
	}
	for _, test := range []struct{ version, os, arch string }{{"1.24.2", "darwin", "amd64"}, {"1.30.0", "darwin", "amd64"}, {"1.30.1", "linux", "amd64"}} {
		if _, _, err := artifacts(test.version, test.os, test.arch); err == nil {
			t.Fatal("unsupported runtime accepted", test)
		}
	}
}
