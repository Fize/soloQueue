package agent

import (
	"context"
	"testing"
	"time"
)

func TestPriorityMailbox_BasicOperations(t *testing.T) {
	pm := NewPriorityMailbox()

	var normalCalled, highCalled bool
	normalJob := func(ctx context.Context) { normalCalled = true }
	highJob := func(ctx context.Context) { highCalled = true }

	pm.SubmitNormal(normalJob)
	pm.SubmitHigh(highJob)

	// High priority should be available first
	select {
	case pj := <-pm.HighCh():
		if pj.priority != PriorityHigh {
			t.Errorf("priority = %d, want High", pj.priority)
		}
		pj.job(context.Background())
		if !highCalled {
			t.Error("high job was not called")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for high priority job")
	}

	// Normal priority should be available next
	select {
	case pj := <-pm.NormalCh():
		if pj.priority != PriorityNormal {
			t.Errorf("priority = %d, want Normal", pj.priority)
		}
		pj.job(context.Background())
		if !normalCalled {
			t.Error("normal job was not called")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for normal priority job")
	}
}

func TestPriorityMailbox_Channels(t *testing.T) {
	pm := NewPriorityMailbox()

	if pm.HighCh() == nil {
		t.Error("HighCh() is nil")
	}
	if pm.NormalCh() == nil {
		t.Error("NormalCh() is nil")
	}

	// Should not block for first few items (buffer cap=4)
	pm.SubmitHigh(func(ctx context.Context) {})
}

func TestPriorityMailbox_Len_Empty(t *testing.T) {
	pm := NewPriorityMailbox()
	high, normal := pm.Len()
	if high != 0 || normal != 0 {
		t.Errorf("Len() = (%d, %d), want (0, 0)", high, normal)
	}
}

func TestPriorityMailbox_Len_WithItems(t *testing.T) {
	pm := NewPriorityMailbox()
	pm.SubmitHigh(func(ctx context.Context) {})
	pm.SubmitHigh(func(ctx context.Context) {})
	pm.SubmitNormal(func(ctx context.Context) {})

	high, normal := pm.Len()
	if high != 2 {
		t.Errorf("high = %d, want 2", high)
	}
	if normal != 1 {
		t.Errorf("normal = %d, want 1", normal)
	}

	// Drain one high, check again
	<-pm.HighCh()
	high2, _ := pm.Len()
	if high2 != 1 {
		t.Errorf("after drain high = %d, want 1", high2)
	}
}
