package session

import (
	"testing"
)

// ─── ParseSessionID tests ──────────────────────────────────────────────────

func TestParseSessionID_L1(t *testing.T) {
	ref, err := ParseSessionID("l1")
	if err != nil {
		t.Fatalf("ParseSessionID(l1): %v", err)
	}
	if ref.Kind != KindL1 {
		t.Errorf("Kind = %d, want KindL1", ref.Kind)
	}
	if ref.L2ID != "" {
		t.Errorf("L2ID = %q, want empty", ref.L2ID)
	}
	if ref.String() != "l1" {
		t.Errorf("String() = %q, want l1", ref.String())
	}
}

func TestParseSessionID_L2(t *testing.T) {
	const id = "550e8400-e29b-41d4-a716-446655440000"
	ref, err := ParseSessionID("l2:" + id)
	if err != nil {
		t.Fatalf("ParseSessionID(l2:%s): %v", id, err)
	}
	if ref.Kind != KindL2 {
		t.Errorf("Kind = %d, want KindL2", ref.Kind)
	}
	if ref.L2ID != id {
		t.Errorf("L2ID = %q, want %s", ref.L2ID, id)
	}
	if ref.String() != "l2:"+id {
		t.Errorf("String() = %q, want l2:%s", ref.String(), id)
	}
}

func TestParseSessionID_Empty(t *testing.T) {
	_, err := ParseSessionID("")
	if err != ErrEmptySessionID {
		t.Errorf("err = %v, want ErrEmptySessionID", err)
	}
}

func TestParseSessionID_BareUUID(t *testing.T) {
	_, err := ParseSessionID("550e8400-e29b-41d4-a716-446655440000")
	if err != ErrMalformedSessionID {
		t.Errorf("err = %v, want ErrMalformedSessionID", err)
	}
}

func TestParseSessionID_EmptyL2UUID(t *testing.T) {
	_, err := ParseSessionID("l2:")
	if err != ErrEmptyL2UUID {
		t.Errorf("err = %v, want ErrEmptyL2UUID", err)
	}
}

func TestParseSessionID_MalformedPrefix(t *testing.T) {
	cases := []string{"l2:not-a-uuid", "l3:foo", "L1", "L2:bar", "session:1", "foo"}
	for _, c := range cases {
		_, err := ParseSessionID(c)
		if err != ErrMalformedSessionID {
			t.Errorf("ParseSessionID(%q): err = %v, want ErrMalformedSessionID", c, err)
		}
	}
}

// ─── NormalizeLegacy tests ──────────────────────────────────────────────────

func TestNormalizeLegacy(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"", "l1"},
		{"l1", "l1"},
		{"l2:abc", "l2:abc"},
		{"550e8400-e29b-41d4-a716-446655440000", "l2:550e8400-e29b-41d4-a716-446655440000"},
	}
	for _, c := range cases {
		got := NormalizeLegacy(c.input)
		if got != c.want {
			t.Errorf("NormalizeLegacy(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
