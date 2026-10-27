package cli

import (
	"path/filepath"

	qqbot "github.com/xiaobaitu/soloqueue/internal/channel/qq"
	"github.com/xiaobaitu/soloqueue/internal/config"
)

func configuredTranscriber(settings config.SpeechConfig, workDir string) *qqbot.Transcriber {
	if !settings.Enabled {
		return nil
	}
	modelDir := settings.ModelDir
	if modelDir == "" {
		modelDir = filepath.Join(workDir, "models")
	}
	return qqbot.NewTranscriber(settings.Model, modelDir)
}
