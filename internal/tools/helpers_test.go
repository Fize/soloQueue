package tools

import (
	"testing"
)

// ─── looksBinary ───────────────────────────────────────────────────────

func TestLooksBinary(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"empty", []byte(""), false},
		{"utf8", []byte("hello world 中文"), false},
		{"nul", []byte("before\x00after"), true},
		{"nul_late_within_512", append(make([]byte, 500), 0), true},
		{"nul_after_512", append(bytes1k(), 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := looksBinary(c.in); got != c.want {
				t.Errorf("looksBinary = %v, want %v", got, c.want)
			}
		})
	}
}

func bytes1k() []byte {
	b := make([]byte, 1024)
	for i := range b {
		b[i] = 'a'
	}
	return b
}

// ─── ctxErrOrNil ───────────────────────────────────────────────────────

func TestCtxErrOrNil(t *testing.T) {
	if err := ctxErrOrNil(nil); err != nil {
		t.Errorf("ctxErrOrNil(nil) = %v, want nil", err)
	}
}
