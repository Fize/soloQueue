package local

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/buildidentity"
)

func TestInternalMainBinaryWithRealModel(t *testing.T) {
	binary, model, runtime := os.Getenv("SOLOQUEUE_TEST_MAIN_BINARY"), os.Getenv("SOLOQUEUE_TEST_EMBEDDING_MODEL"), os.Getenv("SOLOQUEUE_TEST_EMBEDDING_RUNTIME")
	if binary == "" || model == "" || runtime == "" {
		t.Skip("main binary, model and runtime not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	buildID, err := buildidentity.File(binary)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Start(ctx, Config{Worker: binary, ModelDir: model, Runtime: runtime, Device: "cpu", Internal: true, BuildID: buildID})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	batch, err := c.Embed(ctx, []string{"Hello world"}, "query")
	if err != nil {
		t.Fatal(err)
	}
	if batch.Backend != "cpu" || len(batch.Results) != 1 || len(batch.Results[0].Embedding) != Dimension {
		t.Fatal("invalid embedding result")
	}
	var sum float64
	for _, v := range batch.Results[0].Embedding {
		sum += float64(v) * float64(v)
	}
	if math.Abs(sum-1) > 1e-4 {
		t.Fatalf("embedding norm %g", sum)
	}
	dir := c.workDir
	if dir == "" {
		t.Fatal("missing isolated worker directory")
	}
	_ = c.Close()
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("worker directory not removed: %v", err)
	}
}

func TestInternalWorkerRejectsDifferentBuild(t *testing.T) {
	binary := os.Getenv("SOLOQUEUE_TEST_MAIN_BINARY")
	if binary == "" {
		t.Skip("main binary not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := Start(ctx, Config{Worker: binary, ModelDir: t.TempDir(), Runtime: filepath.Join(t.TempDir(), "runtime"), Device: "cpu", Internal: true, BuildID: "different-build"})
	if err == nil {
		_ = client.Close()
		t.Fatal("different build accepted")
	}
	if !strings.Contains(err.Error(), "incompatible internal embedding build") {
		t.Fatalf("unexpected error: %v", err)
	}
}
