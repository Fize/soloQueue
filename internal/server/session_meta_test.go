package server

import (
	"context"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// newTitleHub builds the minimum Hub needed to exercise session-title
// observation: an L2 store plus the work directory its metadata lives in.
func newTitleHub(t *testing.T, id string) (*Hub, *session.L2SessionStore) {
	t.Helper()
	workDir := t.TempDir()
	store := session.NewL2SessionStore(nil, workDir, nil)
	if _, err := store.Create(context.Background(), id, "engineering", "", ""); err != nil {
		t.Fatalf("Create(%s): %v", id, err)
	}
	h := &Hub{mux: &Mux{workDir: workDir, l2Store: store}}
	return h, store
}

func TestObserveSessionInputSetsTitleImmediately(t *testing.T) {
	h, store := newTitleHub(t, "s1")

	// A tool-using first turn can run for minutes before its DoneEvent. The
	// title must be available as soon as the prompt is accepted.
	h.observeSessionInput("l2:s1", "Fix the login bug\n\nIt only reproduces on Safari.")

	if got, want := store.GetName("s1"), "Fix the login bug"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
	if store.LastActive("s1").IsZero() {
		t.Fatal("input should mark the session as active")
	}
}

func TestObserveSessionInputSkipsBuiltinCommands(t *testing.T) {
	for _, prompt := range []string{"/clear", "/model provider:model", "   ", ""} {
		h, store := newTitleHub(t, "s1")
		h.observeSessionInput("l2:s1", prompt)
		if got := store.GetName("s1"); got != "" {
			t.Fatalf("prompt %q titled the session as %q", prompt, got)
		}
	}
}

func TestObserveSessionInputKeepsExistingTitle(t *testing.T) {
	h, store := newTitleHub(t, "s1")
	store.SetName("s1", "First title")

	h.observeSessionInput("l2:s1", "A completely different follow-up")

	if got := store.GetName("s1"); got != "First title" {
		t.Fatalf("title = %q, want the first accepted prompt to win", got)
	}
}

func TestObserveSessionMetaDoneEventKeepsInputTitle(t *testing.T) {
	h, store := newTitleHub(t, "s1")
	h.observeSessionInput("l2:s1", "Investigate the flaky test")

	h.observeSessionMeta("l2:s1", "req-1", agent.DoneEvent{Content: "done"})

	if got := store.GetName("s1"); got != "Investigate the flaky test" {
		t.Fatalf("DoneEvent overwrote the input-time title: %q", got)
	}
}

func TestObserveSessionInputPublishesSessionMetaFrame(t *testing.T) {
	h, _ := newTitleHub(t, "s1")
	h.mux.transcripts = transcript.NewRegistry(transcript.Config{})

	h.observeSessionInput("l2:s1", "Publish the new title")

	journal, ok := h.mux.transcripts.Lookup("l2:s1")
	if !ok {
		t.Fatal("expected a transcript journal for the session")
	}
	frames, err := journal.ReadAfter(journal.Epoch(), 0, 10)
	if err != nil {
		t.Fatalf("ReadAfter: %v", err)
	}
	for _, frame := range frames {
		if frame.Kind == transcript.KindSessionMeta && frame.SessionName == "Publish the new title" {
			return
		}
	}
	t.Fatalf("expected a session_meta frame carrying the title, got %+v", frames)
}
