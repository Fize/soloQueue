package config

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestStorageLegacySizePrecedence(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int
	}{{"session:\n  timeline_max_file_mb: 7\n", 7}, {"session:\n  timeline_max_file_mb: 7\nstorage:\n  session_history:\n    max_file_mb: 50\n", 50}} {
		s := DefaultSettings()
		if err := yaml.Unmarshal([]byte(tc.input), &s); err != nil {
			t.Fatal(err)
		}
		if got := s.EffectiveStorage().SessionHistory.MaxFileMB; got != tc.want {
			t.Fatalf("got %d want %d", got, tc.want)
		}
	}
}
func TestStorageValidation(t *testing.T) {
	p := DefaultStorageConfig()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.RuntimeLogs.MaxFiles = -1
	if p.Validate() == nil {
		t.Fatal("negative accepted")
	}
	p = DefaultStorageConfig()
	p.SessionHistory.MaxTotalMB = 1
	if p.Validate() == nil {
		t.Fatal("unsupported history deletion accepted")
	}
}
