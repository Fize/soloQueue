package tools

import (
	"testing"
	"time"
)

func TestDelegateTool_PreferredTimeout_Explicit(t *testing.T) {
	dt := NewDelegateTool("leader", "desc", 20*time.Minute, nil, nil)
	if got := dt.PreferredTimeout(); got != 20*time.Minute {
		t.Errorf("PreferredTimeout() = %v, want 20m", got)
	}
}

func TestDelegateTool_PreferredTimeout_Default(t *testing.T) {
	dt := NewDelegateTool("leader", "desc", 0, nil, nil)
	if got := dt.PreferredTimeout(); got != DelegateDefaultTimeout {
		t.Errorf("PreferredTimeout() = %v, want DelegateDefaultTimeout (%v)", got, DelegateDefaultTimeout)
	}
}

func TestDelegateTool_PreferredTimeout_Capped(t *testing.T) {
	// PreferredTimeout returns the raw dt.Timeout / DelegateDefaultTimeout;
	// the actual capping to DelegateMaxTimeout happens inside Execute/ExecuteAsync.
	dt := NewDelegateTool("leader", "desc", 99*time.Minute, nil, nil)
	if got := dt.PreferredTimeout(); got != 99*time.Minute {
		t.Errorf("PreferredTimeout() = %v, want 99m (uncapped)", got)
	}
}
