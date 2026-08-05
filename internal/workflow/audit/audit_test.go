package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogIsAppendOnlyHashChainedAndRedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir, "wf_test")
	if err != nil {
		t.Fatal(err)
	}
	one, err := log.Append("wf_test", "node_1", "tool_result", map[string]string{"api_key": "secret-value", "message": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	two, err := log.Append("wf_test", "", "state", map[string]string{"status": "running"})
	if err != nil {
		t.Fatal(err)
	}
	if two.Sequence != 2 || two.PrevHash != one.Hash || log.Head() != two.Hash {
		t.Fatalf("unexpected chain: one=%+v two=%+v head=%s", one, two, log.Head())
	}
	path := filepath.Join(dir, "wf_test.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-value") || !strings.Contains(string(raw), "REDACTED") {
		t.Fatalf("audit redaction failed: %s", raw)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir, "wf_test")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	three, err := reopened.Append("wf_test", "", "finished", json.RawMessage(`{"status":"completed"}`))
	if err != nil || three.Sequence != 3 || three.PrevHash != two.Hash {
		t.Fatalf("reopen chain = %+v err=%v", three, err)
	}
}

func TestOpenRestoresLargeTailEntry(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir, "wf_large")
	if err != nil {
		t.Fatal(err)
	}
	last, err := log.Append("wf_large", "node_1", "large", map[string]string{"content": strings.Repeat("x", 128<<10)})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir, "wf_large")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	next, err := reopened.Append("wf_large", "", "finished", map[string]string{"status": "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Sequence != last.Sequence+1 || next.PrevHash != last.Hash {
		t.Fatalf("reopened chain = %+v, previous = %+v", next, last)
	}
}
