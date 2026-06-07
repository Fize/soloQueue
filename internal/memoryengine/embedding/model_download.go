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

// EnsureModel returns the path to the ONNX model, downloading if necessary.
// If modelName is empty, the default model is used.
// Models are cached under cacheDir (typically ~/.soloqueue/models/).
func EnsureModel(modelName string) (string, error) {
	if modelName == "" {
		modelName = defaultModelName
	}

	// Resolve cache dir: use SOLOQUEUE_MODELS or ~/.soloqueue/models
	cacheDir := os.Getenv("SOLOQUEUE_MODELS")
	if cacheDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("model download: home dir: %w", err)
		}
		cacheDir = filepath.Join(homeDir, ".soloqueue", defaultModelDir)
	}

	modelDir := filepath.Join(cacheDir, modelName)
	modelFile := filepath.Join(modelDir, "model.onnx")

	if _, err := os.Stat(modelFile); err == nil {
		return modelDir, nil
	}

	// Model not found — download it
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return "", fmt.Errorf("model download: mkdir %s: %w", modelDir, err)
	}

	if err := downloadModel(modelName, modelDir); err != nil {
		return "", fmt.Errorf("model download %s: %w", modelName, err)
	}

	return modelDir, nil
}

// downloadModel downloads the ONNX model from HuggingFace CDN.
//
// The E5 model ONNX files are available at:
//
//	https://huggingface.co/intfloat/multilingual-e5-large/resolve/main/onnx/model.onnx
//	https://huggingface.co/intfloat/multilingual-e5-large/resolve/main/onnx/model.onnx_data (if >2GB)
//	https://huggingface.co/intfloat/multilingual-e5-large/resolve/main/vocab.txt
//	https://huggingface.co/intfloat/multilingual-e5-large/resolve/main/tokenizer.json
//
// For now this is a placeholder — users should download the model manually or
// use the HuggingFace CLI. Automatic download will be implemented in a follow-up.
func downloadModel(modelName, targetDir string) error {
	return fmt.Errorf(
		"automatic model download not yet implemented. Please download %s manually:\n"+
			"  1. Visit https://huggingface.co/%s\n"+
			"  2. Download model.onnx, vocab.txt\n"+
			"  3. Place in %s\n"+
			"  Or use: huggingface-cli download %s --local-dir %s",
		modelName, modelName, targetDir, modelName, targetDir,
	)
}
