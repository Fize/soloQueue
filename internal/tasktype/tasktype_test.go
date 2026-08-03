package tasktype

import (
	"testing"
)

func TestTaskType_Valid(t *testing.T) {
	tests := []struct {
		tt   TaskType
		want bool
	}{
		{General, true},
		{Engineering, true},
		{Research, true},
		{Unknown, false},
		{TaskType("invalid"), false},
	}

	for _, tt := range tests {
		if got := tt.tt.Valid(); got != tt.want {
			t.Errorf("TaskType(%q).Valid() = %v, want %v", tt.tt, got, tt.want)
		}
	}
}

func TestTaskType_String(t *testing.T) {
	if got := Engineering.String(); got != "engineering" {
		t.Errorf("Engineering.String() = %q, want %q", got, "engineering")
	}
}

func TestParse(t *testing.T) {
	got, err := Parse("general")
	if err != nil || got != General {
		t.Fatalf("Parse(\"general\") = %v, %v; want %v, nil", got, err, General)
	}

	_, err = Parse("invalid")
	if err == nil {
		t.Fatal("Parse(\"invalid\") expected error, got nil")
	}
}
