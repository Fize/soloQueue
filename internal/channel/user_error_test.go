package channel

import (
	"errors"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

func TestUserFacingErrorHidesInternalOperationID(t *testing.T) {
	tests := []struct {
		name string
		code runwatch.Code
		want string
	}{
		{name: "model transport", code: runwatch.CodeModelTransportStalled, want: "model"},
		{name: "model first progress", code: runwatch.CodeModelFirstProgressStalled, want: "model"},
		{name: "model semantic", code: runwatch.CodeModelSemanticStalled, want: "model"},
		{name: "tool", code: runwatch.CodeToolStalled, want: "tool"},
		{name: "delegation", code: runwatch.CodeDelegationOrphaned, want: "delegated"},
		{name: "root", code: runwatch.CodeRootOrphaned, want: "task"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const operationID = "dlg_internal_secret"
			got := UserFacingError(&runwatch.Cause{Code: tt.code, OperationID: operationID})
			if strings.Contains(got, operationID) || strings.Contains(got, string(tt.code)) {
				t.Fatalf("user-facing error leaked internal details: %q", got)
			}
			if !strings.Contains(strings.ToLower(got), tt.want) {
				t.Fatalf("user-facing error = %q, want it to mention %q", got, tt.want)
			}
		})
	}
}

func TestUserFacingErrorHidesUnknownRawError(t *testing.T) {
	got := UserFacingError(errors.New("request failed: dlg_internal_secret at /private/path"))
	if strings.Contains(got, "dlg_internal_secret") || strings.Contains(got, "/private/path") {
		t.Fatalf("user-facing error leaked raw error: %q", got)
	}
}
