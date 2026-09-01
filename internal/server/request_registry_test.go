package server

import (
	"testing"
	"time"
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

func TestActiveRequestRegistry_WatchdogStateSurvivesSnapshot(t *testing.T) {
	reg := NewActiveRequestRegistry()
	_, err := reg.Reserve("l1", "req-watch", "client-1")
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	lastProgress := time.Unix(1_700_000_000, 0)
	due := lastProgress.Add(15 * time.Minute)
	changed, err := reg.SetWatchdog("req-watch", WatchdogState{
		RunID:          "req-watch",
		Phase:          "tool_confirmation",
		LastProgressAt: lastProgress,
		WatchdogDueAt:  due,
		PausedReason:   "tool_confirmation",
		TerminalCode:   "",
	})
	if err != nil {
		t.Fatalf("SetWatchdog() error = %v", err)
	}
	if !changed {
		t.Fatal("first watchdog projection did not report a change")
	}
	snapshot, err := reg.Validate("l1", "req-watch")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if snapshot.RunID != "req-watch" || snapshot.Phase != "tool_confirmation" || !snapshot.WatchdogDueAt.Equal(due) {
		t.Fatalf("watchdog snapshot = %+v", snapshot)
	}
}

func TestActiveRequestRegistry_FinalizeRetainsTerminalTombstone(t *testing.T) {
	reg := NewActiveRequestRegistry()
	if _, err := reg.Reserve("l1", "req-terminal", "client"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetWatchdog("req-terminal", WatchdogState{
		RunID: "req-terminal", Phase: "cancelled", TerminalCode: "cancelled_by_user",
	}); err != nil {
		t.Fatal(err)
	}
	if !reg.Finalize("l1", "req-terminal") {
		t.Fatal("Finalize returned false")
	}
	if _, err := reg.Validate("l1", "req-terminal"); err != ErrRequestNotFound {
		t.Fatalf("terminal tombstone remained cancellable: %v", err)
	}
	all := reg.GetBySessionAll("l1")
	if len(all) != 1 || all[0].RequestID != "req-terminal" || all[0].TerminalCode != "cancelled_by_user" {
		t.Fatalf("terminal tombstone not rebuildable: %+v", all)
	}
}

func TestActiveRequestRegistry_SingleFlightSuccessSupersedesOldTerminal(t *testing.T) {
	reg := NewActiveRequestRegistry()
	if _, err := reg.Reserve("l2:s1", "req-failed", "client"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetWatchdog("req-failed", WatchdogState{TerminalCode: "model_transport_stalled"}); err != nil {
		t.Fatal(err)
	}
	if !reg.Finalize("l2:s1", "req-failed") {
		t.Fatal("Finalize failed generation returned false")
	}

	if _, err := reg.Reserve("l2:s1", "req-success", "client"); err != nil {
		t.Fatal(err)
	}
	if !reg.Finalize("l2:s1", "req-success") {
		t.Fatal("Finalize successful generation returned false")
	}
	if got, ok := reg.GetBySession("l2:s1"); ok {
		t.Fatalf("old terminal remained current after a newer successful generation: %+v", got)
	}
}

func TestActiveRequestRegistry_TerminalTombstoneExpiresWithClock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reg := NewActiveRequestRegistry()
	reg.now = func() time.Time { return now }
	reg.terminalTTL = time.Minute
	if _, err := reg.Reserve("l1", "req-expiring", "client"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetWatchdog("req-expiring", WatchdogState{TerminalCode: "cancelled_by_user"}); err != nil {
		t.Fatal(err)
	}
	reg.Finalize("l1", "req-expiring")
	if got := reg.GetBySessionAll("l1"); len(got) != 1 {
		t.Fatalf("fresh terminal snapshots = %d, want 1", len(got))
	}

	now = now.Add(time.Minute + time.Nanosecond)
	if !reg.ExpireTerminals() {
		t.Fatal("terminal expiry did not report an observable runtime change")
	}
	if got := reg.GetBySessionAll("l1"); len(got) != 0 {
		t.Fatalf("expired terminal snapshots = %+v, want none", got)
	}
}

func TestActiveRequestRegistry_L1LatestIsDeterministicAndActivePrecedesHistory(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reg := NewActiveRequestRegistry()
	reg.now = func() time.Time { return now }
	if _, err := reg.Reserve("l1", "req-a", "client"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := reg.Reserve("l1", "req-b", "client"); err != nil {
		t.Fatal(err)
	}
	if got, ok := reg.GetBySession("l1"); !ok || got.RequestID != "req-b" {
		t.Fatalf("latest active request = %+v, %v; want req-b", got, ok)
	}
	if _, err := reg.SetWatchdog("req-b", WatchdogState{TerminalCode: "model_transport_stalled"}); err != nil {
		t.Fatal(err)
	}
	reg.Finalize("l1", "req-b")
	if got, ok := reg.GetBySession("l1"); !ok || got.RequestID != "req-a" || got.TerminalCode != "" {
		t.Fatalf("current L1 request = %+v, %v; active req-a must precede retained history", got, ok)
	}
	all := reg.GetBySessionAll("l1")
	if len(all) != 2 || all[0].RequestID != "req-a" || all[1].RequestID != "req-b" {
		t.Fatalf("deterministic L1 current/history order = %+v", all)
	}
}

func TestActiveRequestRegistry_L1ConcurrentRequests(t *testing.T) {
	reg := NewActiveRequestRegistry()

	// L1 allows concurrent requests
	req1, err := reg.Reserve("l1", "req-1", "client-1")
	if err != nil {
		t.Fatalf("Reserve req-1 failed: %v", err)
	}
	if req1.SessionID != "l1" {
		t.Errorf("SessionID = %q, want l1", req1.SessionID)
	}

	_, err = reg.Reserve("l1", "req-2", "client-2")
	if err != nil {
		t.Fatalf("Reserve req-2 failed: %v", err)
	}

	_, err = reg.Reserve("l1", "req-3", "client-1")
	if err != nil {
		t.Fatalf("Reserve req-3 failed: %v", err)
	}

	// GetBySession returns any one of the three
	snap, ok := reg.GetBySession("l1")
	if !ok {
		t.Fatal("GetBySession returned false")
	}
	if snap.SessionID != "l1" {
		t.Errorf("SessionID = %q, want l1", snap.SessionID)
	}

	// GetBySessionAll returns all 3
	all := reg.GetBySessionAll("l1")
	if len(all) != 3 {
		t.Errorf("GetBySessionAll count = %d, want 3", len(all))
	}

	// Finalize req-1
	if !reg.Finalize("l1", "req-1") {
		t.Error("Finalize req-1 returned false")
	}

	all = reg.GetBySessionAll("l1")
	if len(all) != 2 {
		t.Errorf("GetBySessionAll after one finalize = %d, want 2", len(all))
	}

	// Finalize remaining
	if !reg.Finalize("l1", "req-2") {
		t.Error("Finalize req-2 returned false")
	}
	if !reg.Finalize("l1", "req-3") {
		t.Error("Finalize req-3 returned false")
	}

	all = reg.GetBySessionAll("l1")
	if len(all) != 0 {
		t.Errorf("GetBySessionAll after all finalized = %d, want 0", len(all))
	}
	_, ok = reg.GetBySession("l1")
	if ok {
		t.Error("GetBySession should return false after all finalized")
	}
}

func TestActiveRequestRegistry_L2RejectsConcurrent(t *testing.T) {
	reg := NewActiveRequestRegistry()

	// First request for L2 session succeeds
	_, err := reg.Reserve("l2:s1", "req-1", "client-1")
	if err != nil {
		t.Fatalf("Reserve req-1 failed: %v", err)
	}

	// Second request for same L2 session must fail
	_, err = reg.Reserve("l2:s1", "req-2", "client-2")
	if err != ErrSessionBusy {
		t.Errorf("err = %v, want ErrSessionBusy", err)
	}

	// Finalize frees the session
	if !reg.Finalize("l2:s1", "req-1") {
		t.Error("Finalize req-1 returned false")
	}

	// Now a new request can be reserved
	_, err = reg.Reserve("l2:s1", "req-3", "client-1")
	if err != nil {
		t.Errorf("Reserve req-3 after Finalize failed: %v", err)
	}
}

func TestActiveRequestRegistry_L1FinalizeDoesNotAffectOtherL1Requests(t *testing.T) {
	reg := NewActiveRequestRegistry()

	_, _ = reg.Reserve("l1", "req-A", "client-1")
	_, _ = reg.Reserve("l1", "req-B", "client-2")

	// Finalize req-A
	reg.Finalize("l1", "req-A")

	// req-B still exists
	snap, ok := reg.GetBySession("l1")
	if !ok {
		t.Fatal("GetBySession returned false after finalizing req-A")
	}
	if snap.RequestID != "req-B" {
		t.Errorf("RequestID = %q, want req-B", snap.RequestID)
	}

	all := reg.GetBySessionAll("l1")
	if len(all) != 1 {
		t.Errorf("GetBySessionAll count = %d, want 1", len(all))
	}
	if all[0].RequestID != "req-B" {
		t.Errorf("remaining request = %q, want req-B", all[0].RequestID)
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
