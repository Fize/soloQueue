package local

import "github.com/xiaobaitu/soloqueue/internal/testfixtures"
import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	if mode := os.Getenv("SOLOQUEUE_LOCAL_WORKER_TEST"); mode != "" {
		helper(mode)
		os.Exit(0)
	}
}
func helper(mode string) {
	if mode == "startup_wait" {
		time.Sleep(30 * time.Second)
		return
	}
	ready := Status{Model: "intfloat/multilingual-e5-small", Dimension: 384, Backend: "cpu"}
	if mode == "bad_ready" {
		ready.Dimension = 1024
	}
	emit := func(id uint64, data any) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"version": 1, "id": id, "data": data, "error": nil})
	}
	emit(0, ready)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	for scanner.Scan() {
		var request struct {
			ID    uint64   `json:"id"`
			Texts []string `json:"texts"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return
		}
		if mode == "wait" {
			time.Sleep(30 * time.Second)
			return
		}
		if mode == "bad_id" {
			request.ID++
		}
		results := make([]Result, len(request.Texts))
		for i := range results {
			vector := make([]float32, 384)
			vector[0] = 1
			results[i] = Result{Embedding: vector, Tokens: 4}
			if mode == "bad_norm" {
				vector[0] = 2
			}
		}
		emit(request.ID, Batch{Backend: "cpu", Results: results})
	}
}
func startHelper(t *testing.T, mode string) *Client {
	t.Helper()
	t.Setenv("SOLOQUEUE_LOCAL_WORKER_TEST", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Start(ctx, Config{Worker: exe, ModelDir: filepath.Dir(exe), Runtime: exe, Device: "cpu"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func TestEmbedContract(t *testing.T) {
	c := startHelper(t, "ok")
	batch, err := c.Embed(context.Background(), []string{testfixtures.Chinese("client_test_001"), "  "}, "query")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Results) != 2 || len(batch.Results[0].Embedding) != 384 || c.Status().Backend != "cpu" {
		t.Fatal("bad batch")
	}
}
func TestRejectMalformedOutput(t *testing.T) {
	for _, mode := range []string{"bad_id", "bad_norm"} {
		t.Run(mode, func(t *testing.T) {
			c := startHelper(t, mode)
			if _, err := c.Embed(context.Background(), []string{"secret"}, "document"); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("expected sanitized failure")
			}
			if !c.Closed() {
				t.Fatal("malformed response left the transport open")
			}
		})
	}
}
func TestReadyValidation(t *testing.T) {
	t.Setenv("SOLOQUEUE_LOCAL_WORKER_TEST", "bad_ready")
	exe, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Start(ctx, Config{Worker: exe, ModelDir: filepath.Dir(exe), Runtime: exe}); err == nil {
		t.Fatal("accepted wrong dimension")
	}
}
func TestCancellationKillsAndReaps(t *testing.T) {
	c := startHelper(t, "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.Embed(ctx, []string{"private"}, "query"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error: %v", err)
	}
	if !c.Closed() {
		t.Fatal("active cancellation left the transport open")
	}
	if c.cmd.ProcessState == nil || !c.cmd.ProcessState.Exited() && c.cmd.ProcessState.Success() {
		t.Fatal("process not reaped")
	}
}
func TestQueuedCancellationDoesNotKillWorker(t *testing.T) {
	c := startHelper(t, "ok")
	<-c.gate
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Embed(ctx, []string{"x"}, "query"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	c.gate <- struct{}{}
	if c.Closed() {
		t.Fatal("queued cancellation closed the transport")
	}
	if _, err := c.Embed(context.Background(), []string{"x"}, "query"); err != nil {
		t.Fatal(err)
	}
}
func TestStartupCancellation(t *testing.T) {
	t.Setenv("SOLOQUEUE_LOCAL_WORKER_TEST", "startup_wait")
	exe, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Start(ctx, Config{Worker: exe, ModelDir: filepath.Dir(exe), Runtime: exe})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestInputBounds(t *testing.T) {
	c := startHelper(t, "ok")
	oversizedFrame := make([]string, 32)
	for i := range oversizedFrame {
		oversizedFrame[i] = strings.Repeat("x", 32768)
	}
	for _, texts := range [][]string{nil, make([]string, 33), {strings.Repeat("x", 65537)}, {string([]byte{0xff})}, oversizedFrame} {
		if _, err := c.Embed(context.Background(), texts, "query"); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(fmt.Sprint("expected typed input error: ", err))
		}
		if c.Closed() {
			t.Fatal("invalid request closed the transport")
		}
	}
	if _, err := c.Embed(context.Background(), []string{"x"}, "invalid"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := c.Embed(context.Background(), []string{"valid"}, "query"); err != nil {
		t.Fatal(err)
	}
}
