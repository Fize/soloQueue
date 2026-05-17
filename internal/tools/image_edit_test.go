package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ─── Tool metadata ────────────────────────────────────────────────────

func TestImageEditTool_Name(t *testing.T) {
	tool := newImageEditTool(Config{})
	if tool.Name() != "ImageEdit" {
		t.Errorf("expected ImageEdit, got %s", tool.Name())
	}
}

func TestImageEditTool_Description(t *testing.T) {
	tool := newImageEditTool(Config{})
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestImageEditTool_Parameters(t *testing.T) {
	tool := newImageEditTool(Config{})
	params := tool.Parameters()
	var schema map[string]interface{}
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}
	if typ, ok := schema["type"].(string); !ok || typ != "object" {
		t.Errorf("expected type=object, got %v", typ)
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in schema")
	}
	if _, ok := props["operation"]; !ok {
		t.Error("expected operation property")
	}
	if _, ok := props["image"]; !ok {
		t.Error("expected image property")
	}
	req, ok := schema["required"].([]interface{})
	if !ok {
		t.Fatal("expected required array")
	}
	hasOp := false
	hasImg := false
	for _, r := range req {
		if s, ok := r.(string); ok {
			if s == "operation" {
				hasOp = true
			}
			if s == "image" {
				hasImg = true
			}
		}
	}
	if !hasOp || !hasImg {
		t.Errorf("expected operation and image in required, got %v", req)
	}
}

// ─── Operation mapping ────────────────────────────────────────────────

func TestOperationMapping_AllOperations(t *testing.T) {
	expected := []string{
		"style_transfer", "refine", "inpaint", "outpaint",
		"replace_background", "change_clothes", "sketch_to_image",
	}
	for _, op := range expected {
		if _, ok := operationAction[op]; !ok {
			t.Errorf("operation %q missing from mapping", op)
		}
	}
	if len(operationAction) != len(expected) {
		t.Errorf("expected %d operations, got %d", len(expected), len(operationAction))
	}
}

func TestOperationMapping_CorrectActions(t *testing.T) {
	cases := map[string]string{
		"style_transfer":     "ImageToImage",
		"refine":             "RefineImage",
		"inpaint":            "ImageInpaintingRemoval",
		"outpaint":           "ImageOutpainting",
		"replace_background": "ReplaceBackground",
		"change_clothes":     "ChangeClothes",
		"sketch_to_image":    "SketchToImage",
	}
	for op, want := range cases {
		got, ok := operationAction[op]
		if !ok {
			t.Errorf("operation %q not found", op)
			continue
		}
		if got != want {
			t.Errorf("%s: expected %s, got %s", op, want, got)
		}
	}
}

// ─── isURL ────────────────────────────────────────────────────────────

func TestIsURL(t *testing.T) {
	if !isURL("https://example.com/img.jpg") {
		t.Error("expected true for https URL")
	}
	if !isURL("http://example.com/img.jpg") {
		t.Error("expected true for http URL")
	}
	if isURL("data:image/png;base64,abc123") {
		t.Error("expected false for data URI")
	}
	if isURL("not-a-url") {
		t.Error("expected false for plain string")
	}
	if isURL("") {
		t.Error("expected false for empty string")
	}
}

// ─── parseEditResp ────────────────────────────────────────────────────

func TestParseEditResp_OK(t *testing.T) {
	body := []byte(`{"Response":{"ResultImage":"https://example.com/out.jpg","RequestId":"req-1"}}`)
	url, err := parseEditResp(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example.com/out.jpg" {
		t.Errorf("expected url, got %s", url)
	}
}

func TestParseEditResp_Error(t *testing.T) {
	body := []byte(`{"Response":{"Error":{"Code":"InvalidParameter","Message":"bad input"}}}`)
	_, err := parseEditResp(body)
	if err == nil {
		t.Error("expected error")
	}
	if !strings.Contains(err.Error(), "InvalidParameter") {
		t.Errorf("expected InvalidParameter in error, got: %v", err)
	}
}

func TestParseEditResp_EmptyResult(t *testing.T) {
	body := []byte(`{"Response":{"ResultImage":"","RequestId":"req-1"}}`)
	_, err := parseEditResp(body)
	if err == nil {
		t.Error("expected error for empty ResultImage")
	}
}

// ─── Payload building ─────────────────────────────────────────────────

func TestBuildPayload_StyleTransfer(t *testing.T) {
	tool := newImageEditTool(Config{})
	strength := 0.7
	a := imageEditArgs{
		Operation:      "style_transfer",
		Image:          "https://example.com/input.jpg",
		Prompt:         "anime girl",
		NegativePrompt: "blurry",
		Style:          "201",
		Strength:       &strength,
		Resolution:     "768:1024",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["InputUrl"] != "https://example.com/input.jpg" {
		t.Errorf("expected InputUrl, got %v", payload["InputUrl"])
	}
	if payload["Prompt"] != "anime girl" {
		t.Errorf("expected Prompt, got %v", payload["Prompt"])
	}
	if payload["NegativePrompt"] != "blurry" {
		t.Errorf("expected NegativePrompt, got %v", payload["NegativePrompt"])
	}
	styles, ok := payload["Styles"].([]string)
	if !ok || len(styles) != 1 || styles[0] != "201" {
		t.Errorf("expected Styles=[201], got %v", payload["Styles"])
	}
	if payload["Strength"].(float64) != 0.7 {
		t.Errorf("expected Strength=0.7, got %v", payload["Strength"])
	}
	rc, ok := payload["ResultConfig"].(map[string]string)
	if !ok || rc["Resolution"] != "768:1024" {
		t.Errorf("expected Resolution=768:1024, got %v", payload["ResultConfig"])
	}
	if payload["RspImgType"] != "url" {
		t.Errorf("expected RspImgType=url, got %v", payload["RspImgType"])
	}
}

func TestBuildPayload_Base64Image(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation: "refine",
		Image:     "9j/4QlQaHR0c...base64data",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if _, ok := payload["InputUrl"]; ok {
		t.Error("expected InputImage for base64, got InputUrl")
	}
	if payload["InputImage"] != "9j/4QlQaHR0c...base64data" {
		t.Errorf("expected InputImage, got %v", payload["InputImage"])
	}
}

func TestBuildPayload_Refine(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation: "refine",
		Image:     "https://example.com/blurry.jpg",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["InputUrl"] != "https://example.com/blurry.jpg" {
		t.Errorf("expected InputUrl, got %v", payload["InputUrl"])
	}
	if payload["RspImgType"] != "url" {
		t.Errorf("expected RspImgType=url, got %v", payload["RspImgType"])
	}
}

func TestBuildPayload_Inpaint(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation: "inpaint",
		Image:     "https://example.com/input.jpg",
		Mask:      "https://example.com/mask.png",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["MaskUrl"] != "https://example.com/mask.png" {
		t.Errorf("expected MaskUrl, got %v", payload["MaskUrl"])
	}
	if _, ok := payload["Mask"]; ok {
		t.Error("expected MaskUrl for URL, got Mask")
	}
}

func TestBuildPayload_InpaintBase64Mask(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation: "inpaint",
		Image:     "https://example.com/input.jpg",
		Mask:      "base64maskdata",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["Mask"] != "base64maskdata" {
		t.Errorf("expected Mask, got %v", payload["Mask"])
	}
	if _, ok := payload["MaskUrl"]; ok {
		t.Error("expected Mask for base64, got MaskUrl")
	}
}

func TestBuildPayload_Outpaint(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation: "outpaint",
		Image:     "https://example.com/input.jpg",
		Ratio:     "4:3",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["Ratio"] != "4:3" {
		t.Errorf("expected Ratio=4:3, got %v", payload["Ratio"])
	}
}

func TestBuildPayload_ReplaceBackground(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation: "replace_background",
		Image:     "https://example.com/product.jpg",
		Prompt:    "on a marble table",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["Prompt"] != "on a marble table" {
		t.Errorf("expected Prompt, got %v", payload["Prompt"])
	}
}

func TestBuildPayload_ChangeClothes(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation:    "change_clothes",
		Image:        "https://example.com/model.jpg",
		ClothesImage: "https://example.com/dress.jpg",
		Prompt:       "wearing a red dress",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["ClothesImageUrl"] != "https://example.com/dress.jpg" {
		t.Errorf("expected ClothesImageUrl, got %v", payload["ClothesImageUrl"])
	}
	if payload["Prompt"] != "wearing a red dress" {
		t.Errorf("expected Prompt, got %v", payload["Prompt"])
	}
}

func TestBuildPayload_SketchToImage(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation: "sketch_to_image",
		Image:     "https://example.com/sketch.jpg",
		Prompt:    "a beautiful landscape",
		Style:     "101",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["Prompt"] != "a beautiful landscape" {
		t.Errorf("expected Prompt, got %v", payload["Prompt"])
	}
	styles, ok := payload["Styles"].([]string)
	if !ok || len(styles) != 1 || styles[0] != "101" {
		t.Errorf("expected Styles=[101], got %v", payload["Styles"])
	}
}

func TestBuildPayload_LogoAddDefault(t *testing.T) {
	tool := newImageEditTool(Config{})
	a := imageEditArgs{
		Operation: "refine",
		Image:     "https://example.com/img.jpg",
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["LogoAdd"].(int) != 1 {
		t.Errorf("expected LogoAdd=1 (default), got %v", payload["LogoAdd"])
	}
}

func TestBuildPayload_LogoAddCustom(t *testing.T) {
	tool := newImageEditTool(Config{})
	logoOff := int64(0)
	a := imageEditArgs{
		Operation: "refine",
		Image:     "https://example.com/img.jpg",
		LogoAdd:   &logoOff,
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["LogoAdd"].(int64) != 0 {
		t.Errorf("expected LogoAdd=0, got %v", payload["LogoAdd"])
	}
}

func TestBuildPayload_EnhanceAndRestoreFace(t *testing.T) {
	tool := newImageEditTool(Config{})
	enh := int64(1)
	rf := int64(3)
	a := imageEditArgs{
		Operation:   "style_transfer",
		Image:       "https://example.com/img.jpg",
		Enhance:     &enh,
		RestoreFace: &rf,
	}
	payload := tool.buildPayload(a, operationAction[a.Operation])
	if payload["EnhanceImage"].(int64) != 1 {
		t.Errorf("expected EnhanceImage=1, got %v", payload["EnhanceImage"])
	}
	if payload["RestoreFace"].(int64) != 3 {
		t.Errorf("expected RestoreFace=3, got %v", payload["RestoreFace"])
	}
}

// ─── Execute validation ───────────────────────────────────────────────

func TestImageEditExecute_NoDefaultModel(t *testing.T) {
	cfg := Config{
		ImageModels: []ImgModelCfg{},
	}
	tool := newImageEditTool(cfg)
	_, err := tool.Execute(context.Background(), `{"operation":"refine","image":"https://example.com/img.jpg"}`)
	if err == nil || !strings.Contains(err.Error(), ErrImageGenNoDefaultModel.Error()) {
		t.Errorf("expected no default model error, got: %v", err)
	}
}

func TestImageEditExecute_MissingCredentials(t *testing.T) {
	cfg := Config{
		ImageModels: []ImgModelCfg{
			{ID: "test", Provider: "tencent", Enabled: true, IsDefault: true,
				SecretIdEnv: "MISSING_ID", SecretKeyEnv: "MISSING_KEY",
				APIBaseHost: "aiart.tencentcloudapi.com", Region: "ap-guangzhou"},
		},
	}
	tool := newImageEditTool(cfg)
	_, err := tool.Execute(context.Background(), `{"operation":"refine","image":"https://example.com/img.jpg"}`)
	if err == nil || !strings.Contains(err.Error(), ErrImageGenAuth.Error()) {
		t.Errorf("expected auth error, got: %v", err)
	}
}

func TestImageEditExecute_EmptyImage(t *testing.T) {
	t.Setenv("TEST_ID", "id")
	t.Setenv("TEST_KEY", "key")
	cfg := Config{
		ImageModels: []ImgModelCfg{
			{ID: "test", Provider: "tencent", Enabled: true, IsDefault: true,
				SecretIdEnv: "TEST_ID", SecretKeyEnv: "TEST_KEY",
				APIBaseHost: "host", Region: "region"},
		},
	}
	tool := newImageEditTool(cfg)
	_, err := tool.Execute(context.Background(), `{"operation":"refine","image":""}`)
	if err == nil || !strings.Contains(err.Error(), ErrInvalidArgs.Error()) {
		t.Errorf("expected invalid args, got: %v", err)
	}
}

func TestImageEditExecute_InvalidOperation(t *testing.T) {
	t.Setenv("TEST_ID", "id")
	t.Setenv("TEST_KEY", "key")
	cfg := Config{
		ImageModels: []ImgModelCfg{
			{ID: "test", Provider: "tencent", Enabled: true, IsDefault: true,
				SecretIdEnv: "TEST_ID", SecretKeyEnv: "TEST_KEY",
				APIBaseHost: "host", Region: "region"},
		},
	}
	tool := newImageEditTool(cfg)
	_, err := tool.Execute(context.Background(), `{"operation":"nonexistent","image":"https://example.com/img.jpg"}`)
	if err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Errorf("expected unknown operation error, got: %v", err)
	}
}

func TestImageEditExecute_BadJSON(t *testing.T) {
	t.Setenv("TEST_ID", "id")
	t.Setenv("TEST_KEY", "key")
	cfg := Config{
		ImageModels: []ImgModelCfg{
			{ID: "test", Provider: "tencent", Enabled: true, IsDefault: true,
				SecretIdEnv: "TEST_ID", SecretKeyEnv: "TEST_KEY",
				APIBaseHost: "host", Region: "region"},
		},
	}
	tool := newImageEditTool(cfg)
	_, err := tool.Execute(context.Background(), `not json`)
	if err == nil || !strings.Contains(err.Error(), ErrInvalidArgs.Error()) {
		t.Errorf("expected invalid args, got: %v", err)
	}
}
