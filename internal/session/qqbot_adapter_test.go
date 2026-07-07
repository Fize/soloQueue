package session

import (
	"context"
	"testing"
)

func TestErrorQQBotAdapter(t *testing.T) {
	errMsg := "Test error message"
	adapter := NewErrorQQBotAdapter(errMsg)

	// Test AskStream
	res, err := adapter.AskStream(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error in AskStream: %v", err)
	}
	if res.Content != errMsg {
		t.Errorf("expected Content to be %q, got %q", errMsg, res.Content)
	}

	// Test CancelCurrent
	if err := adapter.CancelCurrent("test"); err != nil {
		t.Errorf("unexpected error in CancelCurrent: %v", err)
	}

	// Test Clear
	if err := adapter.Clear(context.Background()); err != nil {
		t.Errorf("unexpected error in Clear: %v", err)
	}

	// Test SaveUploadedFile
	_, err = adapter.SaveUploadedFile(context.Background(), "test.txt", []byte("hello"))
	if err == nil {
		t.Fatal("expected error in SaveUploadedFile, got nil")
	}
	if err.Error() != errMsg {
		t.Errorf("expected error %q, got %q", errMsg, err.Error())
	}
}
