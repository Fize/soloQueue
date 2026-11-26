package logger

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// exportedError mimics provider/API errors that carry exported fields, which
// encoding/json would otherwise serialize while dropping the Error() text.
type exportedError struct {
	StatusCode int
	Code       string
}

func (e *exportedError) Error() string {
	return fmt.Sprintf("api rejected: HTTP %d (%s)", e.StatusCode, e.Code)
}

func TestFileHandler_ErrorValuesKeepText(t *testing.T) {
	base := errors.New("boom")
	tests := []struct {
		name      string
		key       string
		value     any
		want      any
		wantInCtx bool
	}{
		{"errors.New", "err", errors.New("boom"), "boom", false},
		{"wrapped error", "err", fmt.Errorf("outer: %w", base), "outer: boom", false},
		{"exported fields", "err", &exportedError{StatusCode: 402, Code: "payment_required"},
			"api rejected: HTTP 402 (payment_required)", false},
		{"typed nil", "err", (*exportedError)(nil), nil, false},
		{"non-reserved key", "cause", errors.New("boom"), "boom", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			log, err := System(dir, WithConsole(false))
			if err != nil {
				t.Fatalf("System: %v", err)
			}
			log.Warn(CatApp, "error serialization", tt.key, tt.value)
			if err := log.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			entry := readFirstEntry(t, filepath.Join(dir, "logs", "system",
				"app-"+time.Now().Format("2006-01-02")+".jsonl"))

			got := entry[tt.key]
			if tt.wantInCtx {
				ctx, ok := entry["ctx"].(map[string]any)
				if !ok {
					t.Fatalf("ctx missing in entry: %#v", entry)
				}
				got = ctx[tt.key]
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("%s = %#v, want %#v", tt.key, got, tt.want)
			}
		})
	}
}
