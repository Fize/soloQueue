package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImageTool_Metadata(t *testing.T) {
	tool := newImageTool(Config{})
	if tool.Name() != "ImageTool" {
		t.Errorf("expected ImageTool, got %s", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}

	params := tool.Parameters()
	var schema map[string]interface{}
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in schema")
	}
	if _, ok := props["action"]; !ok {
		t.Error("expected action property")
	}
	if _, ok := props["operation"]; !ok {
		t.Error("expected operation property")
	}
	for _, name := range []string{
		"image", "mask_image", "negative_prompt", "style", "ratio", "strength",
		"resolution", "enhance", "restore_face", "product", "background_template",
		"clothes_image", "clothes_type",
	} {
		if _, ok := props[name]; !ok {
			t.Errorf("expected %s property", name)
		}
	}
}

func TestImageTool_InvalidAction(t *testing.T) {
	tool := newImageTool(Config{})
	_, err := tool.Execute(context.Background(), `{"action":"invalid"}`)
	if err == nil || !strings.Contains(err.Error(), "invalid action") {
		t.Fatalf("expected invalid action error, got %v", err)
	}
}

func TestImageTool_GenerateMissingPrompt(t *testing.T) {
	tool := newImageTool(Config{})
	_, err := tool.Execute(context.Background(), `{"action":"generate","prompt":""}`)
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestImageTool_EditMissingOperation(t *testing.T) {
	tool := newImageTool(Config{})
	_, err := tool.Execute(context.Background(), `{"action":"edit","image":"http://example.com/a.jpg"}`)
	if err == nil {
		t.Fatal("expected error for missing operation")
	}
}

func TestImageTool_EditOperationValidation(t *testing.T) {
	tool := newImageTool(Config{})
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"inpaint mask", `{"action":"edit","operation":"inpaint","image":"base64"}`, "mask_image"},
		{"outpaint ratio", `{"action":"edit","operation":"outpaint","image":"base64"}`, "ratio"},
		{"replace background prompt", `{"action":"edit","operation":"replace_background","image":"https://example.com/product.png"}`, "prompt"},
		{"replace background template", `{"action":"edit","operation":"replace_background","image":"https://example.com/product.png","prompt":"BackgroundTemplate"}`, "background_template"},
		{"replace background URL", `{"action":"edit","operation":"replace_background","image":"base64","prompt":"studio"}`, "URL"},
		{"change clothes image", `{"action":"edit","operation":"change_clothes","image":"https://example.com/model.png","clothes_type":"Dress"}`, "clothes_image"},
		{"change clothes type", `{"action":"edit","operation":"change_clothes","image":"https://example.com/model.png","clothes_image":"https://example.com/dress.png"}`, "clothes_type"},
		{"sketch prompt", `{"action":"edit","operation":"sketch_to_image","image":"base64"}`, "prompt"},
		{"style strength", `{"action":"edit","operation":"style_transfer","image":"base64","strength":0}`, "strength"},
		{"style enhance", `{"action":"edit","operation":"style_transfer","image":"base64","enhance":2}`, "enhance"},
		{"style restore face", `{"action":"edit","operation":"style_transfer","image":"base64","restore_face":7}`, "restore_face"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tt.raw)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestImageTool_EditPayloads(t *testing.T) {
	strength := 0.7
	enhance := int64(1)
	restoreFace := int64(2)
	tests := []struct {
		name      string
		args      imageEditArgs
		action    string
		want      map[string]any
		wantEmpty []string
	}{
		{
			name:      "style transfer URL",
			args:      imageEditArgs{Operation: "style_transfer", Image: "https://example.com/in.png", Prompt: "anime", NegativePrompt: "blur", Style: "201", Strength: &strength, Resolution: "768:1024", Enhance: &enhance, RestoreFace: &restoreFace},
			action:    "ImageToImage",
			want:      map[string]any{"InputUrl": "https://example.com/in.png", "Prompt": "anime", "NegativePrompt": "blur", "Styles": []string{"201"}, "Strength": 0.7, "ResultConfig": map[string]string{"Resolution": "768:1024"}, "EnhanceImage": int64(1), "RestoreFace": int64(2), "LogoAdd": 0, "RspImgType": "url"},
			wantEmpty: []string{"InputImage"},
		},
		{name: "refine base64", args: imageEditArgs{Operation: "refine", Image: "base64-image"}, action: "RefineImage", want: map[string]any{"InputImage": "base64-image", "RspImgType": "url"}, wantEmpty: []string{"InputUrl", "LogoAdd"}},
		{name: "inpaint base64 mask", args: imageEditArgs{Operation: "inpaint", Image: "base64-image", MaskImage: "base64-mask"}, action: "ImageInpaintingRemoval", want: map[string]any{"InputImage": "base64-image", "Mask": "base64-mask", "LogoAdd": 0, "RspImgType": "url"}, wantEmpty: []string{"InputUrl", "MaskUrl"}},
		{name: "outpaint URL", args: imageEditArgs{Operation: "outpaint", Image: "https://example.com/in.png", Ratio: "4:3"}, action: "ImageOutpainting", want: map[string]any{"InputUrl": "https://example.com/in.png", "Ratio": "4:3", "LogoAdd": 0, "RspImgType": "url"}},
		{name: "replace background", args: imageEditArgs{Operation: "replace_background", Image: "https://example.com/product.png", Prompt: "marble", NegativePrompt: "dark", Product: "shoe", BackgroundTemplate: "white", MaskImage: "https://example.com/mask.png", Resolution: "1280:1280"}, action: "ReplaceBackground", want: map[string]any{"ProductUrl": "https://example.com/product.png", "Prompt": "marble", "NegativePrompt": "dark", "Product": "shoe", "BackgroundTemplate": "white", "MaskUrl": "https://example.com/mask.png", "Resolution": "1280:1280", "LogoAdd": 0, "RspImgType": "url"}, wantEmpty: []string{"InputUrl"}},
		{name: "change clothes", args: imageEditArgs{Operation: "change_clothes", Image: "https://example.com/model.png", ClothesImage: "https://example.com/dress.png", ClothesType: "Dress"}, action: "ChangeClothes", want: map[string]any{"ModelUrl": "https://example.com/model.png", "ClothesUrl": "https://example.com/dress.png", "ClothesType": "Dress", "LogoAdd": 0, "RspImgType": "url"}, wantEmpty: []string{"InputUrl"}},
		{name: "sketch base64", args: imageEditArgs{Operation: "sketch_to_image", Image: "base64-sketch", Prompt: "landscape", Style: "101"}, action: "SketchToImage", want: map[string]any{"InputImage": "base64-sketch", "Prompt": "landscape", "LogoAdd": 0, "RspImgType": "url"}, wantEmpty: []string{"Styles"}},
	}

	tool := newImageTool(Config{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, payload, err := tool.buildEditRequest(tt.args)
			if err != nil {
				t.Fatalf("buildEditRequest: %v", err)
			}
			if action != tt.action {
				t.Fatalf("action = %q, want %q", action, tt.action)
			}
			for key, want := range tt.want {
				if got := payload[key]; !reflect.DeepEqual(got, want) {
					t.Errorf("%s = %#v, want %#v", key, got, want)
				}
			}
			for _, key := range tt.wantEmpty {
				if _, ok := payload[key]; ok {
					t.Errorf("unexpected %s = %#v", key, payload[key])
				}
			}
		})
	}
}

func TestImageTool_EditExecuteSuccessAndAPIError(t *testing.T) {
	model := ImgModelCfg{ID: "hunyuan", Provider: "tencent", Enabled: true, IsDefault: true, SecretId: "id", SecretKey: "key", APIBaseHost: "aiart.tencentcloudapi.com", Region: "ap-guangzhou"}
	t.Run("success", func(t *testing.T) {
		oldTransport := http.DefaultClient.Transport
		http.DefaultClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("image")), Header: make(http.Header), Request: req}, nil
		})
		t.Cleanup(func() { http.DefaultClient.Transport = oldTransport })
		oldImgDir := imgDir
		imgDir = t.TempDir()
		t.Cleanup(func() { imgDir = oldImgDir })

		var gotAction string
		exec := NewExecutor()
		exec.HTTPPostFn = func(_ context.Context, _ string, _ string, opts HTTPOptions) (HTTPResponse, error) {
			gotAction = opts.Headers["X-TC-Action"]
			return HTTPResponse{StatusCode: 200, Body: []byte(`{"Response":{"ResultImage":"https://example.invalid/result.png","RequestId":"req-1"}}`)}, nil
		}
		tool := newImageTool(Config{Executor: exec, ImageModels: []ImgModelCfg{model}})
		result, err := tool.Execute(context.Background(), `{"action":"edit","operation":"refine","image":"base64-image"}`)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if gotAction != "RefineImage" {
			t.Fatalf("action = %q", gotAction)
		}
		var got imageResult
		if err := json.Unmarshal([]byte(result), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != "completed" || got.Operation != "refine" || !reflect.DeepEqual(got.ImageURLs, []string{"https://example.invalid/result.png"}) || len(got.LocalPaths) != 1 {
			t.Fatalf("result = %+v", got)
		}
	})

	t.Run("structured API error", func(t *testing.T) {
		exec := NewExecutor()
		exec.HTTPPostFn = func(context.Context, string, string, HTTPOptions) (HTTPResponse, error) {
			return HTTPResponse{StatusCode: 200, Body: []byte(`{"Response":{"Error":{"Code":"InvalidParameter","Message":"bad input"},"RequestId":"req-2"}}`)}, nil
		}
		tool := newImageTool(Config{Executor: exec, ImageModels: []ImgModelCfg{model}})
		result, err := tool.Execute(context.Background(), `{"action":"edit","operation":"refine","image":"base64-image"}`)
		if !errors.Is(err, ErrImageToolEditFailed) {
			t.Fatalf("error = %v", err)
		}
		var got imageResult
		if jsonErr := json.Unmarshal([]byte(result), &got); jsonErr != nil {
			t.Fatal(jsonErr)
		}
		if got.Status != "failed" || got.Operation != "refine" || got.ErrorCode != "InvalidParameter" || got.ErrorMsg != "bad input" {
			t.Fatalf("result = %+v", got)
		}
	})
}

func TestImageTool_EditExecuteTransportFailuresAreStructured(t *testing.T) {
	model := ImgModelCfg{ID: "hunyuan", Provider: "tencent", Enabled: true, IsDefault: true, SecretId: "id", SecretKey: "key", APIBaseHost: "aiart.tencentcloudapi.com", Region: "ap-guangzhou"}
	tests := []struct {
		name     string
		response HTTPResponse
		err      error
		wantCode string
	}{
		{name: "network error", err: errors.New("connection reset"), wantCode: "TRANSPORT_ERROR"},
		{name: "non-2xx response", response: HTTPResponse{StatusCode: http.StatusBadGateway, Body: []byte("upstream failed")}, wantCode: "HTTP_STATUS_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := NewExecutor()
			calls := 0
			exec.HTTPPostFn = func(context.Context, string, string, HTTPOptions) (HTTPResponse, error) {
				calls++
				return tt.response, tt.err
			}
			tool := newImageTool(Config{Executor: exec, ImageModels: []ImgModelCfg{model}})
			result, err := tool.Execute(context.Background(), `{"action":"edit","operation":"refine","image":"base64-image"}`)
			if !errors.Is(err, ErrImageToolEditFailed) {
				t.Fatalf("error = %v", err)
			}
			if calls != 1 {
				t.Fatalf("calls = %d, want 1 (billable edit must not retry)", calls)
			}
			var got imageResult
			if jsonErr := json.Unmarshal([]byte(result), &got); jsonErr != nil {
				t.Fatal(jsonErr)
			}
			if got.Status != "failed" || got.Operation != "refine" || got.ErrorCode != tt.wantCode || got.ErrorMsg == "" {
				t.Fatalf("result = %+v", got)
			}
		})
	}
}

func TestSaveImageResults_Base64Multiple(t *testing.T) {
	oldImgDir := imgDir
	imgDir = t.TempDir()
	t.Cleanup(func() { imgDir = oldImgDir })

	paths := saveImageResults(context.Background(), NewExecutor(), []string{
		"data:image/png;base64,aGVsbG8=",
		"d29ybGQ=",
	}, nil)
	if len(paths) != 2 {
		t.Fatalf("paths = %v", paths)
	}
	for i, want := range []string{"hello", "world"} {
		got, err := os.ReadFile(paths[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", filepath.Base(paths[i]), got, want)
		}
	}
}
