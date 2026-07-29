package config

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

func TestSandboxConfigRuntimeMigration(t *testing.T) {
	tests := []struct {
		name string
		cfg  SandboxConfig
		want tools.RuntimeType
	}{
		{name: "new host", cfg: SandboxConfig{Runtime: "host", Enabled: true}, want: tools.RuntimeHost},
		{name: "new sandbox", cfg: SandboxConfig{Runtime: "sandbox"}, want: tools.RuntimeSandbox},
		{name: "legacy enabled", cfg: SandboxConfig{Enabled: true}, want: tools.RuntimeSandbox},
		{name: "legacy disabled", cfg: SandboxConfig{}, want: tools.RuntimeHost},
		{name: "invalid fails closed", cfg: SandboxConfig{Runtime: "container", Enabled: true}, want: tools.RuntimeHost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.RuntimeType(); got != tt.want {
				t.Fatalf("RuntimeType() = %q, want %q", got, tt.want)
			}
		})
	}
}
