package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/pkg/archive"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

const (
	defaultRemoteImage  = "malzaharguo/soloqueue-sandbox:latest"
	defaultLocalImage   = "soloqueue-sandbox:latest"
	legacyContainerName = "soloqueue-sandbox"
	sandboxImageSchema  = "2"
)

type metaConfig struct {
	Image string `json:"image"`
}

// resolveImageAndBuild resolves meta.json & Dockerfile matrix in ~/.soloqueue/sandbox/.
func (d *DockerRunner) resolveImageAndBuild(ctx context.Context) (string, error) {
	if imageName := strings.TrimSpace(os.Getenv("SOLOQUEUE_SANDBOX_IMAGE")); imageName != "" {
		if d.log != nil {
			d.log.InfoContext(ctx, logger.CatApp, "sandbox: using image from SOLOQUEUE_SANDBOX_IMAGE", "image", imageName)
		}
		return imageName, nil
	}
	home, _ := os.UserHomeDir()
	sandboxDir := filepath.Join(home, ".soloqueue", "sandbox")
	return d.resolveImageAndBuildFromDir(ctx, sandboxDir)
}

func (d *DockerRunner) resolveImageAndBuildFromDir(ctx context.Context, sandboxDir string) (string, error) {
	metaPath := filepath.Join(sandboxDir, "meta.json")
	dockerfilePath := filepath.Join(sandboxDir, "Dockerfile")

	var metaImage string
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta metaConfig
		if err := json.Unmarshal(data, &meta); err == nil && strings.TrimSpace(meta.Image) != "" {
			metaImage = strings.TrimSpace(meta.Image)
		} else if d.log != nil {
			d.log.WarnContext(ctx, logger.CatApp, "sandbox: invalid meta.json format, falling back to default resolution", "err", err)
		}
	}

	_, hasDockerfile := os.Stat(dockerfilePath)
	dockerfileExist := hasDockerfile == nil

	// Matrix Resolution
	if metaImage != "" && dockerfileExist {
		if d.log != nil {
			d.log.InfoContext(ctx, logger.CatApp, "sandbox: building local Dockerfile with meta.json image name", "image", metaImage, "dir", sandboxDir)
		}
		if err := d.buildImage(ctx, sandboxDir, metaImage); err != nil {
			return "", err
		}
		return metaImage, nil
	}

	if metaImage != "" && !dockerfileExist {
		if d.log != nil {
			d.log.InfoContext(ctx, logger.CatApp, "sandbox: meta.json present without Dockerfile, pulling remote image", "image", metaImage)
		}
		return metaImage, nil
	}

	if metaImage == "" && dockerfileExist {
		imgName := defaultLocalImage
		if d.log != nil {
			d.log.InfoContext(ctx, logger.CatApp, "sandbox: Dockerfile present without valid meta.json, building with default local name", "image", imgName, "dir", sandboxDir)
		}
		if err := d.buildImage(ctx, sandboxDir, imgName); err != nil {
			return "", err
		}
		return imgName, nil
	}

	if d.log != nil {
		d.log.InfoContext(ctx, logger.CatApp, "sandbox: using default remote sandbox image", "image", defaultRemoteImage)
	}
	return defaultRemoteImage, nil
}

func (d *DockerRunner) buildImage(ctx context.Context, contextDir, imageName string) error {
	tar, err := archive.TarWithOptions(contextDir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("sandbox: archive Dockerfile context: %w", err)
	}
	defer tar.Close()

	opts := types.ImageBuildOptions{
		Dockerfile: "Dockerfile",
		Tags:       []string{imageName},
		Remove:     true,
	}
	res, err := d.cli.ImageBuild(ctx, tar, opts)
	if err != nil {
		return fmt.Errorf("sandbox: build image %s: %w", imageName, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

func (d *DockerRunner) ensureImage(ctx context.Context) error {
	resolvedImage, err := d.resolveImageAndBuild(ctx)
	if err != nil {
		return err
	}
	d.imageName = resolvedImage

	inspect, _, inspectErr := d.cli.ImageInspectWithRaw(ctx, d.imageName)
	if inspectErr == nil {
		if inspect.Config == nil {
			return fmt.Errorf("sandbox: image contract missing config metadata")
		}
		return validateSandboxImage(inspect.Config.Labels)
	}

	if d.log != nil {
		d.log.InfoContext(ctx, logger.CatApp, "sandbox: pulling image", "image", d.imageName)
	}
	reader, err := d.cli.ImagePull(ctx, d.imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("sandbox: pull image %s: %w", d.imageName, err)
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	inspect, _, err = d.cli.ImageInspectWithRaw(ctx, d.imageName)
	if err != nil {
		return fmt.Errorf("sandbox: inspect pulled image %s: %w", d.imageName, err)
	}
	if inspect.Config == nil {
		return fmt.Errorf("sandbox: image contract missing config metadata")
	}
	return validateSandboxImage(inspect.Config.Labels)
}

func validateSandboxImage(labels map[string]string) error {
	if labels["org.soloqueue.sandbox.schema"] != sandboxImageSchema {
		return fmt.Errorf(
			"sandbox: image contract mismatch: require org.soloqueue.sandbox.schema=%s",
			sandboxImageSchema,
		)
	}
	return nil
}
