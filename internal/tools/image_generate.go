package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Tool ──────────────────────────────────────────────────────────────

type imageGenTool struct {
	cfg    Config
	logger *logger.Logger
}

func newImageGenTool(cfg Config) *imageGenTool {
	return &imageGenTool{cfg: cfg, logger: cfg.Logger}
}

func (imageGenTool) Name() string { return "ImageGenerate" }

func (imageGenTool) Description() string {
	return "Generate images from text descriptions using configured AI image models " +
		"(Tencent Hunyuan, etc.). Returns temporary image URLs (valid ~1 hour)."
}

func (imageGenTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "prompt":{"type":"string","description":"Text description for the image (Chinese recommended, max 8192 chars)"},
    "resolution":{"type":"string","description":"Output resolution W:H, e.g. 1024:1024"},
    "seed":{"type":"integer","description":"Random seed (1-4294967295), omit for random"},
    "revise":{"type":"integer","description":"1=enable prompt revision (recommended), 0=off"},
    "images":{"type":"array","items":{"type":"string"},"description":"Reference images (max 3), base64 or URL"}
  },
  "required":["prompt"]
}`)
}

// ─── Args / Result ─────────────────────────────────────────────────────

type imageGenArgs struct {
	Prompt     string   `json:"prompt"`
	Resolution string   `json:"resolution,omitempty"`
	Seed       *int64   `json:"seed,omitempty"`
	Revise     *int64   `json:"revise,omitempty"`
	Images     []string `json:"images,omitempty"`
}

type imageGenResult struct {
	Model         string   `json:"model"`
	Status        string   `json:"status"`
	ImageURLs     []string `json:"image_urls,omitempty"`
	LocalPaths    []string `json:"local_paths,omitempty"`
	RevisedPrompt []string `json:"revised_prompt,omitempty"`
	ErrorCode     string   `json:"error_code,omitempty"`
	ErrorMsg      string   `json:"error_msg,omitempty"`
}

// ─── Execute ──────────────────────────────────────────────────────────

func (t *imageGenTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a imageGenArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("prompt", a.Prompt); err != nil {
		return "", err
	}

	model, err := findDefaultModel(t.cfg.ImageModels)
	if err != nil {
		return "", err
	}

	prov, ok := providers[model.Provider]
	if !ok {
		return "", fmt.Errorf("%w: unknown provider %q", ErrInvalidArgs, model.Provider)
	}

	if err := checkCredentials(model); err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "image_gen: submitting",
			"model", model.ID,
			"prompt_len", len(a.Prompt))
	}

	sr := submitReq{
		Prompt:     a.Prompt,
		Resolution: a.Resolution,
		Seed:       a.Seed,
		Revise:     a.Revise,
		Images:     a.Images,
	}
	url, body, headers, err := prov.buildSubmitReq(*model, sr)
	if err != nil {
		return "", fmt.Errorf("build submit request: %w", err)
	}

	respBody, err := doPost(ctx, t.cfg.Executor, url, body, headers)
	if err != nil {
		return "", fmt.Errorf("submit request: %w", err)
	}

	jobID, err := prov.parseSubmitResp(respBody)
	if err != nil {
		return "", fmt.Errorf("parse submit response: %w", err)
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "image_gen: submitted",
			"model", model.ID,
			"job_id", jobID)
	}

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
		}

		url, body, headers, err := prov.buildQueryReq(*model, jobID)
		if err != nil {
			return "", fmt.Errorf("build query request: %w", err)
		}

		respBody, err := doPost(ctx, t.cfg.Executor, url, body, headers)
		if err != nil {
			if t.logger != nil {
				t.logger.WarnContext(ctx, logger.CatTool, "image_gen: query failed",
					"model", model.ID,
					"job_id", jobID,
					"err", err.Error())
			}
			continue
		}

		status, urls, revised, err := prov.parseQueryResp(respBody)
		if err != nil {
			if t.logger != nil {
				t.logger.WarnContext(ctx, logger.CatTool, "image_gen: parse query failed",
					"model", model.ID,
					"job_id", jobID,
					"err", err.Error())
			}
			continue
		}

		switch status {
		case "5":
			if t.logger != nil {
				t.logger.InfoContext(ctx, logger.CatTool, "image_gen: completed",
					"model", model.ID,
					"job_id", jobID,
					"num_urls", len(urls))
			}
			localPaths := saveImages(ctx, t.cfg.Executor, urls, t.logger)
			r := imageGenResult{
				Model:         model.ID,
				Status:        "completed",
				ImageURLs:     urls,
				LocalPaths:    localPaths,
				RevisedPrompt: revised,
			}
			b, _ := json.Marshal(r)
			return string(b), nil
		case "4":
			if t.logger != nil {
				t.logger.ErrorContext(ctx, logger.CatTool, "image_gen: failed",
					"model", model.ID,
					"job_id", jobID,
					"status", status)
			}
			r := imageGenResult{
				Model:     model.ID,
				Status:    "failed",
				ErrorCode: "JOB_FAILED",
				ErrorMsg:  status,
			}
			b, _ := json.Marshal(r)
			return string(b), fmt.Errorf("%w: job %s", ErrImageGenFailed, jobID)
		}
	}

	return "", ErrImageGenTimeout
}

// ─── Compile-time checks ──────────────────────────────────────────────

var _ Tool = (*imageGenTool)(nil)
