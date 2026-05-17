package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Tool ──────────────────────────────────────────────────────────────

type imageEditTool struct {
	cfg    Config
	logger *logger.Logger
}

func newImageEditTool(cfg Config) *imageEditTool {
	return &imageEditTool{cfg: cfg, logger: cfg.Logger}
}

func (imageEditTool) Name() string { return "ImageEdit" }

func (imageEditTool) Description() string {
	return "Edit images or perform image-to-image operations using configured AI image models " +
		"(Tencent Hunyuan). Supports: style_transfer (image-to-image style conversion), " +
		"refine (upscale/enhance clarity), inpaint (remove objects via mask), " +
		"outpaint (expand image to target ratio), replace_background (product background), " +
		"change_clothes (model outfit swap), sketch_to_image (line art to image). " +
		"Returns image URLs (valid ~1 hour)."
}

func (imageEditTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "operation":{
      "type":"string",
      "enum":["style_transfer","refine","inpaint","outpaint","replace_background","change_clothes","sketch_to_image"],
      "description":"Operation to perform. style_transfer=image-to-image style conversion, refine=upscale enhance clarity, inpaint=remove objects (requires mask), outpaint=expand to target ratio, replace_background=product background generation, change_clothes=model outfit swap, sketch_to_image=line art to image"
    },
    "image":{"type":"string","description":"Input image URL or base64. Required for all operations."},
    "prompt":{"type":"string","description":"Text description used by style_transfer, replace_background, change_clothes, sketch_to_image. Max 256 chars."},
    "negative_prompt":{"type":"string","description":"Negative text for style_transfer. Max 256 chars."},
    "style":{"type":"string","description":"Style number for style_transfer (default '201' anime) or sketch_to_image."},
    "mask":{"type":"string","description":"Mask image URL or base64 for inpaint. Single-channel grayscale; white pixels = remove, black = keep."},
    "ratio":{"type":"string","description":"Target aspect ratio for outpaint. Supported: 1:1, 4:3, 3:4, 16:9, 9:16."},
    "strength":{"type":"number","description":"Generation strength 0-1 for style_transfer. Lower = more similar to original."},
    "resolution":{"type":"string","description":"Output resolution W:H for style_transfer, e.g. 768:1024."},
    "enhance":{"type":"integer","description":"1=enhance image clarity for style_transfer, 0=off."},
    "restore_face":{"type":"integer","description":"Max faces for detail restoration 0-6 for style_transfer. Default 0."},
    "clothes_image":{"type":"string","description":"Clothes image URL or base64 for change_clothes."},
    "logo_add":{"type":"integer","description":"1=add AI-generated watermark, 0=off. Default 1."}
  },
  "required":["operation","image"]
}`)
}

// ─── Args / Result ─────────────────────────────────────────────────────

type imageEditArgs struct {
	Operation      string   `json:"operation"`
	Image          string   `json:"image"`
	Prompt         string   `json:"prompt,omitempty"`
	NegativePrompt string   `json:"negative_prompt,omitempty"`
	Style          string   `json:"style,omitempty"`
	Mask           string   `json:"mask,omitempty"`
	Ratio          string   `json:"ratio,omitempty"`
	Strength       *float64 `json:"strength,omitempty"`
	Resolution     string   `json:"resolution,omitempty"`
	Enhance        *int64   `json:"enhance,omitempty"`
	RestoreFace    *int64   `json:"restore_face,omitempty"`
	ClothesImage   string   `json:"clothes_image,omitempty"`
	LogoAdd        *int64   `json:"logo_add,omitempty"`
}

type imageEditResult struct {
	Model       string   `json:"model"`
	Status      string   `json:"status"`
	Operation   string   `json:"operation"`
	ImageURLs   []string `json:"image_urls,omitempty"`
	LocalPaths  []string `json:"local_paths,omitempty"`
	ErrorCode   string   `json:"error_code,omitempty"`
	ErrorMsg    string   `json:"error_msg,omitempty"`
}

// ─── Operation mapping ─────────────────────────────────────────────────

var operationAction = map[string]string{
	"style_transfer":     "ImageToImage",
	"refine":             "RefineImage",
	"inpaint":            "ImageInpaintingRemoval",
	"outpaint":           "ImageOutpainting",
	"replace_background": "ReplaceBackground",
	"change_clothes":     "ChangeClothes",
	"sketch_to_image":    "SketchToImage",
}

// ─── Execute ──────────────────────────────────────────────────────────

func (t *imageEditTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}

	var a imageEditArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}

	action, ok := operationAction[a.Operation]
	if !ok {
		return "", fmt.Errorf("%w: unknown operation %q", ErrInvalidArgs, a.Operation)
	}

	if err := validateNotZeroLen("image", a.Image); err != nil {
		return "", err
	}

	model, err := findDefaultModel(t.cfg.ImageModels)
	if err != nil {
		return "", err
	}

	if err := checkCredentials(model); err != nil {
		return "", err
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "image_edit: submitting",
			"model", model.ID,
			"operation", a.Operation)
	}

	payload := t.buildPayload(a, action)

	prov := &tencentProvider{}
	url, body, headers, err := prov.buildRequest(*model, action, payload)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	respBody, err := doPost(ctx, url, body, headers)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}

	imageURL, err := parseEditResp(respBody)
	if err != nil {
		r := imageEditResult{
			Model:     model.ID,
			Status:    "failed",
			Operation: a.Operation,
			ErrorCode: "API_ERROR",
			ErrorMsg:  err.Error(),
		}
		b, _ := json.Marshal(r)
		return string(b), fmt.Errorf("%w: %v", ErrImageEditFailed, err)
	}

	urls := []string{imageURL}
	localPaths := saveImages(ctx, urls, t.logger)

	r := imageEditResult{
		Model:      model.ID,
		Status:     "completed",
		Operation:  a.Operation,
		ImageURLs:  urls,
		LocalPaths: localPaths,
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "image_edit: completed",
			"model", model.ID,
			"operation", a.Operation,
			"url", imageURL)
	}

	b, _ := json.Marshal(r)
	return string(b), nil
}

// ─── Payload building ──────────────────────────────────────────────────

func (t *imageEditTool) buildPayload(a imageEditArgs, action string) map[string]any {
	payload := map[string]any{
		"RspImgType": "url",
	}

	setImageField := func(keyURL, keyBase64, val string) {
		if isURL(val) {
			payload[keyURL] = val
		} else {
			payload[keyBase64] = val
		}
	}

	setImageField("InputUrl", "InputImage", a.Image)

	if a.LogoAdd != nil {
		payload["LogoAdd"] = *a.LogoAdd
	} else {
		payload["LogoAdd"] = 1
	}

	switch a.Operation {
	case "style_transfer":
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
	case "refine":
	case "inpaint":
		setImageField("MaskUrl", "Mask", a.Mask)
	case "outpaint":
		payload["Ratio"] = a.Ratio
	case "replace_background":
		if a.Prompt != "" {
			payload["Prompt"] = a.Prompt
		}
	case "change_clothes":
		setImageField("ClothesImageUrl", "ClothesImage", a.ClothesImage)
		if a.Prompt != "" {
			payload["Prompt"] = a.Prompt
		}
	case "sketch_to_image":
		if a.Prompt != "" {
			payload["Prompt"] = a.Prompt
		}
		if a.Style != "" {
			payload["Styles"] = []string{a.Style}
		}
	}

	return payload
}

// ─── Response parsing ──────────────────────────────────────────────────

func parseEditResp(body []byte) (string, error) {
	var resp struct {
		Response struct {
			ResultImage string `json:"ResultImage"`
			RequestId   string `json:"RequestId"`
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
		return "", fmt.Errorf("API error [%s]: %s", resp.Response.Error.Code, resp.Response.Error.Message)
	}
	if resp.Response.ResultImage == "" {
		return "", fmt.Errorf("empty ResultImage in response")
	}
	return resp.Response.ResultImage, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// ─── Compile-time checks ──────────────────────────────────────────────

var _ Tool = (*imageEditTool)(nil)
