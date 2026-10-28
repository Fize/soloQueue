package cli

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
)

func TestConfiguredTranscriberRetainsUnavailableDependencies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOLOQUEUE_WORK_DIR", dir)
	t.Setenv("PATH", t.TempDir())
	if configuredTranscriber(config.SpeechConfig{Enabled: true, Model: "small"}, dir) == nil {
		t.Fatal("enabled speech must retain a transcriber before dependencies are installed")
	}
	if configuredTranscriber(config.SpeechConfig{}, dir) != nil {
		t.Fatal("disabled speech must not create a transcriber")
	}
}
