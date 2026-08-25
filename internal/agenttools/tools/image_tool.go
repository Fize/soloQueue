package tools

import (
	"context"
	"encoding/json"
	"errors"
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
    "image":{"type":"string","description":"Required for action='edit'. Input image URL or base64 (URL required for replace_background and change_clothes)."},
    "mask_image":{"type":"string","description":"Mask image URL or base64. Required for inpaint; optional URL for replace_background."},
    "negative_prompt":{"type":"string","description":"Optional negative prompt for style_transfer or replace_background."},
    "style":{"type":"string","description":"Optional style number for style_transfer."},
    "ratio":{"type":"string","enum":["1:1","4:3","3:4","16:9","9:16"],"description":"Required target aspect ratio for outpaint."},
    "strength":{"type":"number","exclusiveMinimum":0,"maximum":1,"description":"Optional generation strength for style_transfer."},
    "resolution":{"type":"string","description":"Output resolution W:H, e.g. 1024:1024"},
    "enhance":{"type":"integer","enum":[0,1],"description":"Optional style_transfer clarity enhancement switch."},
    "restore_face":{"type":"integer","minimum":0,"maximum":6,"description":"Optional style_transfer maximum faces to restore."},
    "product":{"type":"string","description":"Optional product subject name for replace_background."},
    "background_template":{"type":"string","description":"Optional replace_background template when prompt is BackgroundTemplate."},
    "clothes_image":{"type":"string","description":"Required clothes image URL for change_clothes."},
    "clothes_type":{"type":"string","enum":["Upper-body","Lower-body","Dress"],"description":"Required clothes type for change_clothes."},
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
	Operation          string   `json:"operation"`
	Image              string   `json:"image"`
	Prompt             string   `json:"prompt,omitempty"`
	NegativePrompt     string   `json:"negative_prompt,omitempty"`
	MaskImage          string   `json:"mask_image,omitempty"`
	Style              string   `json:"style,omitempty"`
	Ratio              string   `json:"ratio,omitempty"`
	Strength           *float64 `json:"strength,omitempty"`
	Resolution         string   `json:"resolution,omitempty"`
	Enhance            *int64   `json:"enhance,omitempty"`
	RestoreFace        *int64   `json:"restore_face,omitempty"`
	Product            string   `json:"product,omitempty"`
	BackgroundTemplate string   `json:"background_template,omitempty"`
	ClothesImage       string   `json:"clothes_image,omitempty"`
	ClothesType        string   `json:"clothes_type,omitempty"`
}

type editOpConfig struct {
	reqPrompt       bool
	reqMask         bool
	reqRatio        bool
	reqClothesImage bool
	reqClothesType  bool
	urlInputOnly    bool
}

var validOperations = map[string]editOpConfig{
	"style_transfer":     {},
	"refine":             {},
	"inpaint":            {reqMask: true},
	"outpaint":           {reqRatio: true},
	"replace_background": {reqPrompt: true, urlInputOnly: true},
	"change_clothes":     {reqClothesImage: true, reqClothesType: true, urlInputOnly: true},
	"sketch_to_image":    {reqPrompt: true},
}

var operationAction = map[string]string{
	"style_transfer":     "ImageToImage",
	"refine":             "RefineImage",
	"inpaint":            "ImageInpaintingRemoval",
	"outpaint":           "ImageOutpainting",
	"replace_background": "ReplaceBackground",
	"change_clothes":     "ChangeClothes",
	"sketch_to_image":    "SketchToImage",
}

type imageArgs struct {
	Action             string   `json:"action"`
	Prompt             string   `json:"prompt"`
	Operation          string   `json:"operation,omitempty"`
	Image              string   `json:"image,omitempty"`
	MaskImage          string   `json:"mask_image,omitempty"`
	NegativePrompt     string   `json:"negative_prompt,omitempty"`
	Style              string   `json:"style,omitempty"`
	Ratio              string   `json:"ratio,omitempty"`
	Strength           *float64 `json:"strength,omitempty"`
	Resolution         string   `json:"resolution,omitempty"`
	Enhance            *int64   `json:"enhance,omitempty"`
	RestoreFace        *int64   `json:"restore_face,omitempty"`
	Product            string   `json:"product,omitempty"`
	BackgroundTemplate string   `json:"background_template,omitempty"`
	ClothesImage       string   `json:"clothes_image,omitempty"`
	ClothesType        string   `json:"clothes_type,omitempty"`
	Seed               *int64   `json:"seed,omitempty"`
	Revise             *int64   `json:"revise,omitempty"`
	Images             []string `json:"images,omitempty"`
}

type imageResult struct {
	Model         string   `json:"model"`
	Status        string   `json:"status"`
	Operation     string   `json:"operation,omitempty"`
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

	respBody, err := doPost(ctx, t.cfg.Executor, url, body, headers)
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
	if op.reqRatio {
		if err := validateNotZeroLen("ratio", a.Ratio); err != nil {
			return "", err
		}
		switch a.Ratio {
		case "1:1", "4:3", "3:4", "16:9", "9:16":
		default:
			return "", fmt.Errorf("%w: invalid ratio %q", ErrInvalidArgs, a.Ratio)
		}
	}
	if op.reqClothesImage {
		if err := validateNotZeroLen("clothes_image", a.ClothesImage); err != nil {
			return "", err
		}
	}
	if op.reqClothesType {
		if err := validateNotZeroLen("clothes_type", a.ClothesType); err != nil {
			return "", err
		}
		switch a.ClothesType {
		case "Upper-body", "Lower-body", "Dress":
		default:
			return "", fmt.Errorf("%w: invalid clothes_type %q", ErrInvalidArgs, a.ClothesType)
		}
	}
	if op.urlInputOnly && !isURL(a.Image) {
		return "", fmt.Errorf("%w: image must be a URL for operation %q", ErrInvalidArgs, a.Operation)
	}
	if a.Operation == "change_clothes" && !isURL(a.ClothesImage) {
		return "", fmt.Errorf("%w: clothes_image must be a URL", ErrInvalidArgs)
	}
	if a.Operation == "replace_background" && a.MaskImage != "" && !isURL(a.MaskImage) {
		return "", fmt.Errorf("%w: mask_image must be a URL for operation %q", ErrInvalidArgs, a.Operation)
	}
	if a.Operation == "replace_background" && a.Prompt == "BackgroundTemplate" && strings.TrimSpace(a.BackgroundTemplate) == "" {
		return "", fmt.Errorf("%w: background_template is required when prompt is BackgroundTemplate", ErrInvalidArgs)
	}
	if a.Strength != nil && (*a.Strength <= 0 || *a.Strength > 1) {
		return "", fmt.Errorf("%w: strength must be greater than 0 and at most 1", ErrInvalidArgs)
	}
	if a.Enhance != nil && *a.Enhance != 0 && *a.Enhance != 1 {
		return "", fmt.Errorf("%w: enhance must be 0 or 1", ErrInvalidArgs)
	}
	if a.RestoreFace != nil && (*a.RestoreFace < 0 || *a.RestoreFace > 6) {
		return "", fmt.Errorf("%w: restore_face must be between 0 and 6", ErrInvalidArgs)
	}

	model, err := findDefaultModel(t.cfg.ImageModels)
	if err != nil {
		return "", err
	}
	if model.Provider != "tencent" {
		return "", fmt.Errorf("%w: unknown provider %q", ErrInvalidArgs, model.Provider)
	}
	if err := checkCredentials(model); err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "image_tool: edit submitting", "model", model.ID, "operation", a.Operation)
	}

	editArgs := imageEditArgs{
		Operation:          a.Operation,
		Image:              a.Image,
		Prompt:             a.Prompt,
		NegativePrompt:     a.NegativePrompt,
		MaskImage:          a.MaskImage,
		Style:              a.Style,
		Ratio:              a.Ratio,
		Strength:           a.Strength,
		Resolution:         a.Resolution,
		Enhance:            a.Enhance,
		RestoreFace:        a.RestoreFace,
		Product:            a.Product,
		BackgroundTemplate: a.BackgroundTemplate,
		ClothesImage:       a.ClothesImage,
		ClothesType:        a.ClothesType,
	}
	action, payload, err := t.buildEditRequest(editArgs)
	if err != nil {
		return "", fmt.Errorf("build edit request: %w", err)
	}
	prov := &tencentProvider{}
	url, body, headers, err := prov.buildRequest(*model, action, payload)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	respBody, err := doPost(ctx, t.cfg.Executor, url, body, headers)
	if err != nil {
		code, msg := editErrorDetails(err)
		return failedEditResult(model.ID, a.Operation, code, msg, err)
	}

	imageValue, err := parseEditResp(respBody)
	if err != nil {
		code, msg := editErrorDetails(err)
		return failedEditResult(model.ID, a.Operation, code, msg, err)
	}

	results := []string{imageValue}
	localPaths := saveImageResults(ctx, t.cfg.Executor, results, t.logger)
	r := imageResult{Model: model.ID, Status: "completed", Operation: a.Operation, ImageURLs: results, LocalPaths: localPaths}
	b, _ := json.Marshal(r)
	return string(b), nil
}

func (t *imageTool) buildEditRequest(a imageEditArgs) (string, map[string]any, error) {
	action, ok := operationAction[a.Operation]
	if !ok {
		return "", nil, fmt.Errorf("%w: unknown operation %q", ErrInvalidArgs, a.Operation)
	}
	payload := map[string]any{"RspImgType": "url"}
	setImageField := func(urlKey, base64Key, value string) {
		if isURL(value) {
			payload[urlKey] = value
		} else {
			payload[base64Key] = value
		}
	}

	switch a.Operation {
	case "replace_background":
		payload["ProductUrl"] = a.Image
	case "change_clothes":
		payload["ModelUrl"] = a.Image
		payload["ClothesUrl"] = a.ClothesImage
		payload["ClothesType"] = a.ClothesType
	default:
		setImageField("InputUrl", "InputImage", a.Image)
	}

	switch a.Operation {
	case "style_transfer":
		payload["LogoAdd"] = 0
		if a.Prompt != "" {
			payload["Prompt"] = a.Prompt
		}
		if a.NegativePrompt != "" {
			payload["NegativePrompt"] = a.NegativePrompt
		}
		if a.Style != "" {
			payload["Styles"] = []string{a.Style}
		}
		if a.Strength != nil {
			payload["Strength"] = *a.Strength
		}
		if a.Resolution != "" {
			payload["ResultConfig"] = map[string]string{"Resolution": a.Resolution}
		}
		if a.Enhance != nil {
			payload["EnhanceImage"] = *a.Enhance
		}
		if a.RestoreFace != nil {
			payload["RestoreFace"] = *a.RestoreFace
		}
	case "inpaint":
		payload["LogoAdd"] = 0
		setImageField("MaskUrl", "Mask", a.MaskImage)
	case "outpaint":
		payload["LogoAdd"] = 0
		payload["Ratio"] = a.Ratio
	case "replace_background":
		payload["LogoAdd"] = 0
		payload["Prompt"] = a.Prompt
		if a.NegativePrompt != "" {
			payload["NegativePrompt"] = a.NegativePrompt
		}
		if a.Product != "" {
			payload["Product"] = a.Product
		}
		if a.BackgroundTemplate != "" {
			payload["BackgroundTemplate"] = a.BackgroundTemplate
		}
		if a.MaskImage != "" {
			payload["MaskUrl"] = a.MaskImage
		}
		if a.Resolution != "" {
			payload["Resolution"] = a.Resolution
		}
	case "change_clothes":
		payload["LogoAdd"] = 0
	case "sketch_to_image":
		payload["LogoAdd"] = 0
		payload["Prompt"] = a.Prompt
	}
	return action, payload, nil
}

type editAPIError struct{ Code, Message string }

func (e *editAPIError) Error() string { return fmt.Sprintf("API error [%s]: %s", e.Code, e.Message) }

func parseEditResp(body []byte) (string, error) {
	var resp struct {
		Response struct {
			ResultImage string `json:"ResultImage"`
			Error       *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error,omitempty"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.Response.Error != nil {
		return "", &editAPIError{Code: resp.Response.Error.Code, Message: resp.Response.Error.Message}
	}
	if resp.Response.ResultImage == "" {
		return "", errors.New("empty ResultImage in response")
	}
	return resp.Response.ResultImage, nil
}

func editErrorDetails(err error) (string, string) {
	var transportErr *httpTransportError
	if errors.As(err, &transportErr) {
		return "TRANSPORT_ERROR", transportErr.Error()
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return "HTTP_STATUS_ERROR", statusErr.Error()
	}
	var apiErr *editAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Code, apiErr.Message
	}
	return "API_ERROR", err.Error()
}

func failedEditResult(model, operation, code, message string, cause error) (string, error) {
	r := imageResult{Model: model, Status: "failed", Operation: operation, ErrorCode: code, ErrorMsg: message}
	b, _ := json.Marshal(r)
	return string(b), fmt.Errorf("%w: %v", ErrImageToolEditFailed, cause)
}

func isURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
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

		respBody, err := doPost(ctx, t.cfg.Executor, url, body, headers)
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
			localPaths := saveImages(ctx, t.cfg.Executor, urls, t.logger)
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
