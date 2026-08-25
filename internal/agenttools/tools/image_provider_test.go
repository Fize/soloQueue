package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTencentGenerateRequestContract(t *testing.T) {
	seed := int64(42)
	revise := int64(1)
	model := ImgModelCfg{SecretId: "id", SecretKey: "key", APIBaseHost: "aiart.tencentcloudapi.com", Region: "ap-guangzhou"}
	_, body, headers, err := (&tencentProvider{}).buildSubmitReq(model, submitReq{
		Prompt: "mountain", Resolution: "1024:1024", Seed: &seed, Revise: &revise, Images: []string{"reference"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if headers["X-TC-Action"] != "SubmitTextToImageJob" || headers["X-TC-Version"] != tencentAPIVersion {
		t.Fatalf("headers = %#v", headers)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"Prompt": "mountain", "LogoAdd": float64(0), "Resolution": "1024:1024",
		"Seed": float64(42), "Revise": float64(1), "Images": []any{"reference"},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = %#v, want %#v", payload, want)
	}
}

func TestParseEditResp(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		got, err := parseEditResp([]byte(`{"Response":{"ResultImage":"https://example.com/result.png"}}`))
		if err != nil || got != "https://example.com/result.png" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})
	t.Run("empty result", func(t *testing.T) {
		if _, err := parseEditResp([]byte(`{"Response":{"ResultImage":""}}`)); err == nil {
			t.Fatal("expected empty result error")
		}
	})
	t.Run("malformed", func(t *testing.T) {
		if _, err := parseEditResp([]byte(`not-json`)); err == nil {
			t.Fatal("expected malformed response error")
		}
	})
}

func TestSaveImageResults_URLMultiple(t *testing.T) {
	oldTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(filepath.Base(req.URL.Path))),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = oldTransport })

	oldImgDir := imgDir
	imgDir = t.TempDir()
	t.Cleanup(func() { imgDir = oldImgDir })

	paths := saveImageResults(context.Background(), NewExecutor(), []string{
		"https://example.com/first.png?token=one",
		"https://example.com/second.jpg?token=two",
	}, nil)
	if len(paths) != 2 {
		t.Fatalf("paths = %v", paths)
	}
	for i, want := range []string{"first.png", "second.jpg"} {
		got, err := os.ReadFile(paths[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("file %d = %q, want %q", i, got, want)
		}
	}
}

func TestSaveImageResults_UniqueAcrossCallsAndConcurrent(t *testing.T) {
	oldImgDir := imgDir
	imgDir = t.TempDir()
	t.Cleanup(func() { imgDir = oldImgDir })

	const encoded = "data:image/png;base64,aGVsbG8="
	first := saveImageResults(context.Background(), NewExecutor(), []string{encoded}, nil)
	second := saveImageResults(context.Background(), NewExecutor(), []string{encoded}, nil)
	if len(first) != 1 || len(second) != 1 || first[0] == second[0] {
		t.Fatalf("consecutive paths = %v, %v", first, second)
	}
	if filepath.Ext(first[0]) != ".png" || filepath.Ext(second[0]) != ".png" {
		t.Fatalf("extensions = %q, %q", filepath.Ext(first[0]), filepath.Ext(second[0]))
	}

	const workers = 16
	paths := make(chan string, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := saveImageResults(context.Background(), NewExecutor(), []string{encoded}, nil)
			if len(got) == 1 {
				paths <- got[0]
			}
		}()
	}
	wg.Wait()
	close(paths)
	seen := map[string]bool{first[0]: true, second[0]: true}
	for path := range paths {
		if seen[path] {
			t.Fatalf("duplicate artifact path %q", path)
		}
		seen[path] = true
		if filepath.Ext(path) != ".png" {
			t.Fatalf("extension = %q for %q", filepath.Ext(path), path)
		}
	}
	if len(seen) != workers+2 {
		t.Fatalf("saved %d unique artifacts, want %d", len(seen), workers+2)
	}
	for path := range seen {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "hello" {
			t.Fatalf("artifact %q = %q, %v", path, data, err)
		}
	}
}

func TestDownloadTo_RejectsOversizeWithoutWritingTruncatedFile(t *testing.T) {
	oldTransport := http.DefaultClient.Transport
	t.Cleanup(func() { http.DefaultClient.Transport = oldTransport })

	t.Run("boundary", func(t *testing.T) {
		http.DefaultClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(io.LimitReader(zeroReader{}, maxImageBytes)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})
		path := filepath.Join(t.TempDir(), "boundary.png")
		if err := downloadTo(context.Background(), NewExecutor(), "https://example.com/boundary.png", path); err != nil {
			t.Fatalf("downloadTo: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() != maxImageBytes {
			t.Fatalf("size = %v, err = %v", info, err)
		}
	})

	t.Run("over limit", func(t *testing.T) {
		http.DefaultClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(io.LimitReader(zeroReader{}, maxImageBytes+1)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})
		path := filepath.Join(t.TempDir(), "oversize.png")
		err := downloadTo(context.Background(), NewExecutor(), "https://example.com/oversize.png", path)
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("error = %v", err)
		}
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("oversize artifact was written: %v", statErr)
		}
	})
}

func TestDoPost_AppliesTimeoutAndPreservesContextCancellation(t *testing.T) {
	exec := NewExecutor()
	var got HTTPOptions
	var hadDeadline bool
	exec.HTTPPostFn = func(ctx context.Context, _ string, _ string, opts HTTPOptions) (HTTPResponse, error) {
		got = opts
		_, hadDeadline = ctx.Deadline()
		<-ctx.Done()
		return HTTPResponse{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := doPost(ctx, exec, "https://example.com", "{}", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if got.Timeout != httpTimeout {
		t.Fatalf("timeout = %s, want %s", got.Timeout, httpTimeout)
	}
	if !hadDeadline {
		t.Fatal("request context has no deadline")
	}
	if got.MaxBody != 64<<10 {
		t.Fatalf("max body = %d", got.MaxBody)
	}
	if got.Timeout <= 0 || got.Timeout > time.Minute {
		t.Fatalf("unexpected timeout %s", got.Timeout)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
