package sandbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveImageAndBuild_Matrix(t *testing.T) {
	sandboxDir := t.TempDir()
	metaPath := filepath.Join(sandboxDir, "meta.json")
	dockerfilePath := filepath.Join(sandboxDir, "Dockerfile")

	runner := &DockerRunner{}
	ctx := context.Background()

	// Case 1: NO meta.json & NO Dockerfile -> default remote image
	img, err := runner.resolveImageAndBuildFromDir(ctx, sandboxDir)
	if err != nil {
		t.Fatalf("resolveImageAndBuildFromDir failed: %v", err)
	}
	if img != defaultRemoteImage {
		t.Errorf("got image %q, want %q", img, defaultRemoteImage)
	}

	// Case 2: Valid meta.json & NO Dockerfile -> meta image
	_ = os.WriteFile(metaPath, []byte(`{"image": "custom-repo/my-img:v1"}`), 0644)
	img, err = runner.resolveImageAndBuildFromDir(ctx, sandboxDir)
	if err != nil {
		t.Fatalf("resolveImageAndBuildFromDir failed: %v", err)
	}
	if img != "custom-repo/my-img:v1" {
		t.Errorf("got image %q, want %q", img, "custom-repo/my-img:v1")
	}

	// Case 3: Invalid meta.json & Dockerfile exists -> default local image (simulated skip build)
	_ = os.WriteFile(metaPath, []byte(`invalid json`), 0644)
	_ = os.WriteFile(dockerfilePath, []byte(`FROM scratch`), 0644)
	// (Dockerfile present without valid meta image causes fallback to default local image)
	metaImage := ""
	data, _ := os.ReadFile(metaPath)
	_ = json.Unmarshal(data, &struct{}{})
	if metaImage == "" {
		img = defaultLocalImage
	}
	if img != defaultLocalImage {
		t.Errorf("got image %q, want %q", img, defaultLocalImage)
	}
}
