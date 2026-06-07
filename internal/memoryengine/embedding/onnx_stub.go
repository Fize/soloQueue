//go:build !onnx

package embedding

import "fmt"

func newONNXEmbedder(cfg Config) (Embedder, error) {
	return nil, fmt.Errorf("embedding: ONNX provider requires building with -tags onnx and installing onnxruntime (brew install onnxruntime)")
}
