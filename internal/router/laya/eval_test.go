//go:build laya_eval

// Accuracy check against the labelled set. It needs the built worker and a model directory, so it
// is behind a build tag:
//
//	SOLOQUEUE_DECIDE_WORKER=$PWD/soloqueue-decide \
//	SOLOQUEUE_LAYA_MODEL_DIR=$HOME/.soloqueue/models/laya/laya-multilingual-fp32 \
//	SOLOQUEUE_LAYA_WORKDIR=$HOME/.soloqueue \
//	go test -tags laya_eval -run TestLayaAccuracy -v ./internal/router/laya/
package laya_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/router/laya"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

type item struct {
	Text string `json:"text"`
	Gold string `json:"gold"`
}

const accuracyFloor = 0.80

func TestLayaAccuracy(t *testing.T) {
	workDir := os.Getenv("SOLOQUEUE_LAYA_WORKDIR")
	if workDir == "" {
		workDir = t.TempDir()
	}
	modelDir := os.Getenv("SOLOQUEUE_LAYA_MODEL_DIR")
	if modelDir == "" {
		t.Skip("SOLOQUEUE_LAYA_MODEL_DIR is not set")
	}
	if os.Getenv("SOLOQUEUE_DECIDE_WORKER") == "" && !workerBesideTestBinary(t) {
		t.Skip("SOLOQUEUE_DECIDE_WORKER is not set")
	}

	manager := laya.New(workDir, nil)
	defer manager.Close()
	manager.Apply(laya.Config{Enabled: true, ModelDir: modelDir})
	waitReady(t, manager)

	file, err := os.Open(filepath.Join("testdata", "labels50.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var items []item
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var it item
		if err := json.Unmarshal(scanner.Bytes(), &it); err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}

	type tally struct{ correct, total int }
	perClass := map[string]*tally{}
	correct := 0
	var latencies []time.Duration
	for _, it := range items {
		started := time.Now()
		got, err := manager.Decide(context.Background(), it.Text)
		latencies = append(latencies, time.Since(started))
		if err != nil {
			t.Fatalf("decide %q: %v", it.Text, err)
		}
		if perClass[it.Gold] == nil {
			perClass[it.Gold] = &tally{}
		}
		perClass[it.Gold].total++
		if string(got) == it.Gold {
			correct++
			perClass[it.Gold].correct++
		} else {
			t.Logf("miss gold=%s got=%s %.32s", it.Gold, got, it.Text)
		}
		if !tasktype.TaskType(it.Gold).Valid() {
			t.Fatalf("bad gold label %q", it.Gold)
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	accuracy := float64(correct) / float64(len(items))
	t.Logf("accuracy %d/%d = %.3f | p50 %v | p95 %v", correct, len(items), accuracy,
		latencies[len(latencies)/2], latencies[len(latencies)*95/100])
	for label, t2 := range perClass {
		t.Logf("  %-12s recall %d/%d", label, t2.correct, t2.total)
	}
	if accuracy < accuracyFloor {
		t.Fatalf("accuracy %.3f below floor %.2f", accuracy, accuracyFloor)
	}
}

func waitReady(t *testing.T, manager *laya.Manager) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		switch status := manager.Status(); status.State {
		case "ready":
			return
		case "error":
			t.Fatalf("classifier failed: %s", status.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("classifier never became ready: %+v", manager.Status())
}

func workerBesideTestBinary(t *testing.T) bool {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	stat, err := os.Stat(filepath.Join(filepath.Dir(exe), "soloqueue-decide"))
	return err == nil && !stat.IsDir()
}
