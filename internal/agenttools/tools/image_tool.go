package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// ─── Tool ──────────────────────────────────────────────────────────────

type imageTool struct {
	cfg    Config
	logger *logger.Logger
}

func newImageTool(cfg Config) *imageTool {
	return &imageTool{cfg: cfg, logger: cfg.Logger}
}

func (imageTool) Name() string { return "ImageTool" }

func (imageTool) Description() string {
	return "Generate or edit images using configured AI image models (Tencent Hunyuan, etc.). " +
		"Use action 'generate' for text-to-image creation. " +
		"Use action 'edit' for image-to-image operations (style_transfer, refine, inpaint, outpaint, replace_background, change_clothes, sketch_to_image). " +
		"Returns image URLs (valid ~1 hour) and local saved paths."
}

func (imageTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "action":{
      "type":"string",
      "enum":["generate","edit"],
      "description":"Action to perform: 'generate' for text-to-image, 'edit' for image-to-image operations."
    },
    "prompt":{"type":"string","description":"Text description for generation or editing (max 8192 chars for generate, 256 for edit)"},
    "operation":{
      "type":"string",
      "enum":["style_transfer","refine","inpaint","outpaint","replace_background","change_clothes","sketch_to_image"],
      "description":"Required for action='edit'. Operation to perform."
    },
    "image":{"type":"string","description":"Required for action='edit'. Input image URL or base64."},
    "mask_image":{"type":"string","description":"Required for operation='inpaint'. Mask image URL or base64."},
    "negative_prompt":{"type":"string","description":"Optional for edit (style_transfer). Negative prompt text."},
    "resolution":{"type":"string","description":"Output resolution W:H, e.g. 1024:1024"},
    "seed":{"type":"integer","description":"Random seed (1-4294967295), omit for random"},
    "revise":{"type":"integer","description":"1=enable prompt revision (recommended), 0=off"},
    "images":{"type":"array","items":{"type":"string"},"description":"Reference images for generate (max 3), base64 or URL"}
  },
  "required":["action"]
}`)
}

type imageGenArgs struct {
	Prompt     string   `json:"prompt"`
	Resolution string   `json:"resolution,omitempty"`
	Seed       *int64   `json:"seed,omitempty"`
	Revise     *int64   `json:"revise,omitempty"`
	Images     []string `json:"images,omitempty"`
}

type imageEditArgs struct {
	Operation      string `json:"operation"`
	Image          string `json:"image"`
	Prompt         string `json:"prompt,omitempty"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	MaskImage      string `json:"mask_image,omitempty"`
	Resolution     string `json:"resolution,omitempty"`
	Seed           *int64  `json:"seed,omitempty"`
}

type editOpConfig struct {
	reqPrompt bool
	reqMask   bool
}

var validOperations = map[string]editOpConfig{
	"style_transfer":     {reqPrompt: true, reqMask: false},
	"refine":             {reqPrompt: false, reqMask: false},
	"inpaint":            {reqPrompt: false, reqMask: true},
	"outpaint":           {reqPrompt: false, reqMask: false},
	"replace_background": {reqPrompt: true, reqMask: false},
	"change_clothes":     {reqPrompt: true, reqMask: false},
	"sketch_to_image":    {reqPrompt: true, reqMask: false},
}

func submitEditReq(a imageEditArgs) submitReq {
	return submitReq{
		Prompt:     a.Prompt,
		Resolution: a.Resolution,
		Seed:       a.Seed,
		Images:     []string{a.Image},
	}
}

type imageArgs struct {
	Action         string   `json:"action"`
	Prompt         string   `json:"prompt"`
	Operation      string   `json:"operation,omitempty"`
	Image          string   `json:"image,omitempty"`
	MaskImage      string   `json:"mask_image,omitempty"`
	NegativePrompt string   `json:"negative_prompt,omitempty"`
	Resolution     string   `json:"resolution,omitempty"`
	Seed           *int64   `json:"seed,omitempty"`
	Revise         *int64   `json:"revise,omitempty"`
	Images         []string `json:"images,omitempty"`
}

type imageResult struct {
	Model         string   `json:"model"`
	Status        string   `json:"status"`
	ImageURLs     []string `json:"image_urls,omitempty"`
	LocalPaths    []string `json:"local_paths,omitempty"`
	RevisedPrompt []string `json:"revised_prompt,omitempty"`
	ErrorCode     string   `json:"error_code,omitempty"`
	ErrorMsg      string   `json:"error_msg,omitempty"`
}

func (t *imageTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a imageArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	switch strings.ToLower(a.Action) {
	case "generate":
		return t.executeGenerate(ctx, a)
	case "edit":
		return t.executeEdit(ctx, a)
	default:
		return "", fmt.Errorf("%w: invalid action %q (must be 'generate' or 'edit')", ErrInvalidArgs, a.Action)
	}
}

func (t *imageTool) executeGenerate(ctx context.Context, a imageArgs) (string, error) {
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
		t.logger.InfoContext(ctx, logger.CatTool, "image_tool: generate submitting", "model", model.ID, "prompt_len", len(a.Prompt))
	}

	sr := submitReq(imageGenArgs{
		Prompt:     a.Prompt,
		Resolution: a.Resolution,
		Seed:       a.Seed,
		Revise:     a.Revise,
		Images:     a.Images,
	})
	url, body, headers, err := prov.buildSubmitReq(*model, sr)
	if err != nil {
		return "", fmt.Errorf("build submit request: %w", err)
	}

	respBody, err := doPost(ctx, t.cfg.Runtime, url, body, headers)
	if err != nil {
		return "", fmt.Errorf("submit request: %w", err)
	}

	jobID, err := prov.parseSubmitResp(respBody)
	if err != nil {
		return "", fmt.Errorf("parse submit response: %w", err)
	}

	return t.pollJob(ctx, model, prov, jobID)
}

func (t *imageTool) executeEdit(ctx context.Context, a imageArgs) (string, error) {
	if err := validateNotZeroLen("operation", a.Operation); err != nil {
		return "", err
	}
	if err := validateNotZeroLen("image", a.Image); err != nil {
		return "", err
	}
	op, ok := validOperations[a.Operation]
	if !ok {
		return "", fmt.Errorf("%w: unknown operation %q", ErrInvalidArgs, a.Operation)
	}
	if op.reqPrompt {
		if err := validateNotZeroLen("prompt", a.Prompt); err != nil {
			return "", err
		}
	}
	if op.reqMask {
		if err := validateNotZeroLen("mask_image", a.MaskImage); err != nil {
			return "", err
		}
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
		t.logger.InfoContext(ctx, logger.CatTool, "image_tool: edit submitting", "model", model.ID, "operation", a.Operation)
	}

	sr := submitEditReq(imageEditArgs{
		Operation:      a.Operation,
		Image:          a.Image,
		Prompt:         a.Prompt,
		NegativePrompt: a.NegativePrompt,
		MaskImage:      a.MaskImage,
		Resolution:     a.Resolution,
		Seed:           a.Seed,
	})
	url, body, headers, err := prov.buildSubmitReq(*model, sr)
	if err != nil {
		return "", fmt.Errorf("build submit request: %w", err)
	}

	respBody, err := doPost(ctx, t.cfg.Runtime, url, body, headers)
	if err != nil {
		return "", fmt.Errorf("submit request: %w", err)
	}

	jobID, err := prov.parseSubmitResp(respBody)
	if err != nil {
		return "", fmt.Errorf("parse submit response: %w", err)
	}

	return t.pollJob(ctx, model, prov, jobID)
}

func (t *imageTool) pollJob(ctx context.Context, model *ImgModelCfg, prov imageProvider, jobID string) (string, error) {
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

		respBody, err := doPost(ctx, t.cfg.Runtime, url, body, headers)
		if err != nil {
			if t.logger != nil {
				t.logger.WarnContext(ctx, logger.CatTool, "image_tool: query failed", "model", model.ID, "job_id", jobID, "err", err.Error())
			}
			continue
		}

		status, urls, revised, err := prov.parseQueryResp(respBody)
		if err != nil {
			if t.logger != nil {
				t.logger.WarnContext(ctx, logger.CatTool, "image_tool: parse query failed", "model", model.ID, "job_id", jobID, "err", err.Error())
			}
			continue
		}

		switch status {
		case "5":
			if t.logger != nil {
				t.logger.InfoContext(ctx, logger.CatTool, "image_tool: completed", "model", model.ID, "job_id", jobID, "num_urls", len(urls))
			}
			localPaths := saveImages(ctx, t.cfg.Runtime, urls, t.logger)
			r := imageResult{
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
				t.logger.ErrorContext(ctx, logger.CatTool, "image_tool: failed", "model", model.ID, "job_id", jobID, "status", status)
			}
			r := imageResult{
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

var _ Tool = (*imageTool)(nil)
