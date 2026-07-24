package server

import (
	"testing"
)

func TestActiveRequestRegistry_ReserveAndGet(t *testing.T) {
	reg := NewActiveRequestRegistry()

	req, err := reg.Reserve("l2:s1", "req-1", "client-1")
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	if req.SessionID != "l2:s1" || req.RequestID != "req-1" || req.OwnerClientID != "client-1" {
		t.Errorf("unexpected snapshot: %#v", req)
	}

	snap, ok := reg.GetBySession("l2:s1")
	if !ok {
		t.Fatal("GetBySession returned false")
	}
	if snap.RequestID != "req-1" {
		t.Errorf("RequestID = %q, want req-1", snap.RequestID)
	}

	// Reserving second request for same session must fail with ErrSessionBusy
	_, err = reg.Reserve("l2:s1", "req-2", "client-2")
	if err != ErrSessionBusy {
		t.Errorf("err = %v, want ErrSessionBusy", err)
	}

	// Finalizing session frees it
	if !reg.Finalize("l2:s1", "req-1") {
		t.Error("Finalize returned false")
	}

	// Now session can reserve again
	_, err = reg.Reserve("l2:s1", "req-3", "client-1")
	if err != nil {
		t.Errorf("Reserve after Finalize failed: %v", err)
	}
}

func TestActiveRequestRegistry_ValidateAndPendingCall(t *testing.T) {
	reg := NewActiveRequestRegistry()
	_, _ = reg.Reserve("l2:s1", "req-1", "client-1")

	// Validate valid session & request
	_, err := reg.Validate("l2:s1", "req-1")
	if err != nil {
		t.Errorf("Validate valid pair failed: %v", err)
	}

	// Validate mismatched session & request
	_, err = reg.Validate("l2:s2", "req-1")
	if err != ErrRequestSessionMismatch {
		t.Errorf("err = %v, want ErrRequestSessionMismatch", err)
	}

	// Tool call pending registration & resolution
	if err := reg.RegisterPendingCall("req-1", "call-1"); err != nil {
		t.Fatalf("RegisterPendingCall failed: %v", err)
	}

	// Resolving wrong call ID fails
	if err := reg.ResolvePendingCall("req-1", "call-wrong"); err != ErrToolConfirmationMismatch {
		t.Errorf("err = %v, want ErrToolConfirmationMismatch", err)
	}

	// Resolving correct call ID succeeds
	if err := reg.ResolvePendingCall("req-1", "call-1"); err != nil {
		t.Errorf("ResolvePendingCall failed: %v", err)
	}
}
