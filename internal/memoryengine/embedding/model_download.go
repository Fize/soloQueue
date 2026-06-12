package embedding

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultModelName = "intfloat/multilingual-e5-large"
	defaultModelDir  = "models"
)

// EnsureModel returns the path to the ONNX model directory.
// If modelName is empty, the default model is used.
// Models are cached under cacheDir (typically ~/.soloqueue/models/).
// Returns an error if the model is not found — user must download manually.
func EnsureModel(modelName string) (string, error) {
	if modelName == "" {
		modelName = defaultModelName
	}

	cacheDir := os.Getenv("SOLOQUEUE_MODELS")
	if cacheDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("model download: home dir: %w", err)
		}
		cacheDir = filepath.Join(homeDir, ".soloqueue", defaultModelDir)
	}

	modelDir := filepath.Join(cacheDir, modelName)

	// Check root of model dir first
	if _, err := os.Stat(filepath.Join(modelDir, "model.onnx")); err == nil {
		return modelDir, nil
	}
	// HF download puts files in onnx/ subdirectory
	if _, err := os.Stat(filepath.Join(modelDir, "onnx", "model.onnx")); err == nil {
		return filepath.Join(modelDir, "onnx"), nil
	}

	return "", fmt.Errorf(
		"ONNX model not found at %s. Please download it:\n\n"+
			"  pip install huggingface-hub\n"+
			"  hf download %s --local-dir %s\n\n"+
			"Requires:\n"+
			"  - model.onnx (ONNX graph + weights)\n"+
			"  - tokenizer.json (tokenizer config)",
		modelDir, modelName, modelDir,
	)
}
