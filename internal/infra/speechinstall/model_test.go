package speechinstall

import (
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

func TestDownloadVerifiedPreservesExistingOnMismatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "model.bin")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("broken")), Header: make(http.Header)}, nil
	})}
	sum := sha256.Sum256([]byte("expected"))
	if err := DownloadVerified(context.Background(), client, "https://example.invalid/model", fmt.Sprintf("%x", sum), target, 100); err == nil {
		t.Fatal("expected checksum failure")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old" {
		t.Fatalf("existing file changed: %q, %v", data, err)
	}
}

func TestDownloadVerifiedCancellationCanRetry(t *testing.T) {
	target := filepath.Join(t.TempDir(), "model.bin")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failing := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}
	sum := sha256.Sum256([]byte("model"))
	digest := fmt.Sprintf("%x", sum)
	if err := DownloadVerified(ctx, failing, "https://example.invalid/model", digest, target, 100); err == nil {
		t.Fatal("cancelled request succeeded")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("cancelled request left a model: %v", err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("model")), Header: make(http.Header)}, nil
	})}
	if err := DownloadVerified(context.Background(), client, "https://example.invalid/model", digest, target, 100); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(target, digest); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyModelRejectsChangedFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "model.bin")
	sum := sha256.Sum256([]byte("model"))
	ModelSHA256["test-model"] = fmt.Sprintf("%x", sum)
	t.Cleanup(func() { delete(ModelSHA256, "test-model") })
	if err := os.WriteFile(target, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModel(target, "test-model"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("broken model"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModel(target, "test-model"); err == nil {
		t.Fatal("corrupt model accepted")
	}
}
