package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ─── TC3 signing ──────────────────────────────────────────────────────

func TestTC3Sign_KnownVector(t *testing.T) {
	secretId := "AKID" + "z8krbsJ5yKBZQpn74WFkmLPx3EXAMPLE"
	secretKey := "Gu5t9xGARNpq86cd98joQYCN3" + "EXAMPLE"
	host := "cvm.tencentcloudapi.com"
	service := "cvm"
	payload := `{"Limit": 1, "Filters": [{"Values": ["\u672a\u547d\u540d"], "Name": "instance-name"}]}`
	timestamp := int64(1551113065)

	auth, err := tc3Sign(secretId, secretKey, host, service, payload, timestamp)
	if err != nil {
		t.Fatalf("tc3Sign failed: %v", err)
	}
	if !strings.Contains(auth, "TC3-HMAC-SHA256") {
		t.Errorf("expected TC3-HMAC-SHA256 signature, got: %s", auth)
	}
}

func TestTC3Sign_EmptyPayload(t *testing.T) {
	auth, err := tc3Sign("id", "key", "host.example.com", "svc", "{}", 1000000000)
	if err != nil {
		t.Fatalf("tc3Sign failed: %v", err)
	}
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=") {
		t.Errorf("unexpected auth format: %s", auth)
	}
}

func TestSHA256Hex(t *testing.T) {
	h := sha256Hex("hello")
	if len(h) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(h))
	}
}

// ─── findDefaultModel ─────────────────────────────────────────────────

func TestFindDefaultModel_OK(t *testing.T) {
	models := []ImgModelCfg{
		{ID: "a", Enabled: true, IsDefault: false},
		{ID: "b", Enabled: true, IsDefault: true},
	}
	m, err := findDefaultModel(models)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.ID != "b" {
		t.Errorf("expected b, got %s", m.ID)
	}
}

func TestFindDefaultModel_OnlyEnabled(t *testing.T) {
	models := []ImgModelCfg{
		{ID: "a", Enabled: true, IsDefault: false},
	}
	_, err := findDefaultModel(models)
	if err != ErrImageGenNoDefaultModel {
		t.Errorf("expected ErrImageGenNoDefaultModel, got %v", err)
	}
}

func TestFindDefaultModel_Disabled(t *testing.T) {
	models := []ImgModelCfg{
		{ID: "a", Enabled: false, IsDefault: true},
	}
	_, err := findDefaultModel(models)
	if err != ErrImageGenNoDefaultModel {
		t.Errorf("expected ErrImageGenNoDefaultModel, got %v", err)
	}
}

func TestFindDefaultModel_Empty(t *testing.T) {
	_, err := findDefaultModel(nil)
	if err != ErrImageGenNoDefaultModel {
		t.Errorf("expected ErrImageGenNoDefaultModel, got %v", err)
	}
}

// ─── checkCredentials ─────────────────────────────────────────────────

func TestCheckCredentials_TencentMissing(t *testing.T) {
	os.Unsetenv("TEST_TENCENT_ID")
	os.Unsetenv("TEST_TENCENT_KEY")
	m := &ImgModelCfg{Provider: "tencent", SecretIdEnv: "TEST_TENCENT_ID", SecretKeyEnv: "TEST_TENCENT_KEY"}
	if err := checkCredentials(m); err == nil {
		t.Error("expected error for missing credentials")
	}
}

func TestCheckCredentials_TencentOK(t *testing.T) {
	os.Setenv("TEST_TENCENT_ID", "test-id")
	os.Setenv("TEST_TENCENT_KEY", "test-key")
	defer os.Unsetenv("TEST_TENCENT_ID")
	defer os.Unsetenv("TEST_TENCENT_KEY")
	m := &ImgModelCfg{Provider: "tencent", SecretIdEnv: "TEST_TENCENT_ID", SecretKeyEnv: "TEST_TENCENT_KEY"}
	if err := checkCredentials(m); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckCredentials_Doubao(t *testing.T) {
	os.Setenv("TEST_DOUBAO_KEY", "key")
	defer os.Unsetenv("TEST_DOUBAO_KEY")
	m := &ImgModelCfg{Provider: "doubao", APIKeyEnv: "TEST_DOUBAO_KEY"}
	if err := checkCredentials(m); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckCredentials_DoubaoMissing(t *testing.T) {
	os.Unsetenv("TEST_DOUBAO_KEY_MISSING")
	m := &ImgModelCfg{Provider: "doubao", APIKeyEnv: "TEST_DOUBAO_KEY_MISSING"}
	if err := checkCredentials(m); err == nil {
		t.Error("expected error for missing credentials")
	}
}

func TestCheckCredentials_TencentDirectKey(t *testing.T) {
	os.Unsetenv("ANY_ID")
	os.Unsetenv("ANY_KEY")
	m := &ImgModelCfg{Provider: "tencent", SecretId: "direct-id", SecretKey: "direct-key"}
	if err := checkCredentials(m); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckCredentials_DoubaoDirectKey(t *testing.T) {
	os.Unsetenv("ANY_KEY")
	m := &ImgModelCfg{Provider: "doubao", APIKey: "direct-key"}
	if err := checkCredentials(m); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckCredentials_DirectTakesPriority(t *testing.T) {
	os.Setenv("ENV_ID", "env-id")
	os.Setenv("ENV_KEY", "env-key")
	defer os.Unsetenv("ENV_ID")
	defer os.Unsetenv("ENV_KEY")
	m := &ImgModelCfg{Provider: "tencent",
		SecretId: "direct-id", SecretIdEnv: "ENV_ID",
		SecretKey: "direct-key", SecretKeyEnv: "ENV_KEY",
	}
	if err := checkCredentials(m); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ─── Tool metadata ────────────────────────────────────────────────────

func TestImageGenTool_Name(t *testing.T) {
	tool := newImageGenTool(Config{})
	if tool.Name() != "ImageGenerate" {
		t.Errorf("expected ImageGenerate, got %s", tool.Name())
	}
}

func TestImageGenTool_Description(t *testing.T) {
	tool := newImageGenTool(Config{})
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestImageGenTool_Parameters(t *testing.T) {
	tool := newImageGenTool(Config{})
	params := tool.Parameters()
	var schema map[string]interface{}
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}
	if typ, ok := schema["type"].(string); !ok || typ != "object" {
		t.Errorf("expected type=object, got %v", typ)
	}
}

// ─── Parse tencent responses ──────────────────────────────────────────

func TestTencentParseSubmitResp_OK(t *testing.T) {
	body := []byte(`{"Response":{"JobId":"test-job-123","RequestId":"req-1"}}`)
	p := tencentProvider{}
	jobID, err := p.parseSubmitResp(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jobID != "test-job-123" {
		t.Errorf("expected test-job-123, got %s", jobID)
	}
}

func TestTencentParseSubmitResp_Error(t *testing.T) {
	body := []byte(`{"Response":{"Error":{"Code":"InvalidParameter","Message":"bad prompt"}}}`)
	p := tencentProvider{}
	_, err := p.parseSubmitResp(body)
	if err == nil {
		t.Error("expected error")
	}
}

func TestTencentParseSubmitResp_EmptyJobId(t *testing.T) {
	body := []byte(`{"Response":{"JobId":"","RequestId":"req-1"}}`)
	p := tencentProvider{}
	_, err := p.parseSubmitResp(body)
	if err == nil {
		t.Error("expected error for empty JobId")
	}
}

func TestTencentParseQueryResp_Completed(t *testing.T) {
	body := []byte(`{"Response":{"JobStatusCode":"5","JobStatusMsg":"completed","ResultImage":["http://example.com/1.jpg"],"RevisedPrompt":["a dog"]}}`)
	p := tencentProvider{}
	status, urls, revised, err := p.parseQueryResp(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "5" {
		t.Errorf("expected status 5, got %s", status)
	}
	if len(urls) != 1 || urls[0] != "http://example.com/1.jpg" {
		t.Errorf("unexpected urls: %v", urls)
	}
	if len(revised) != 1 || revised[0] != "a dog" {
		t.Errorf("unexpected revised: %v", revised)
	}
}

func TestTencentParseQueryResp_Pending(t *testing.T) {
	body := []byte(`{"Response":{"JobStatusCode":"2","JobStatusMsg":"processing"}}`)
	p := tencentProvider{}
	status, _, _, err := p.parseQueryResp(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "2" {
		t.Errorf("expected status 2, got %s", status)
	}
}

func TestTencentParseQueryResp_Error(t *testing.T) {
	body := []byte(`{"Response":{"Error":{"Code":"FailedOperation.JobNotExist","Message":"job not found"}}}`)
	p := tencentProvider{}
	_, _, _, err := p.parseQueryResp(body)
	if err == nil {
		t.Error("expected error")
	}
}

// ─── Execute validation ───────────────────────────────────────────────

func TestImageGenExecute_NoDefaultModel(t *testing.T) {
	cfg := Config{
		ImageModels: []ImgModelCfg{},
	}
	tool := newImageGenTool(cfg)
	_, err := tool.Execute(context.Background(), `{"prompt":"test"}`)
	if err == nil || !strings.Contains(err.Error(), ErrImageGenNoDefaultModel.Error()) {
		t.Errorf("expected no default model error, got: %v", err)
	}
}

func TestImageGenExecute_MissingCredentials(t *testing.T) {
	cfg := Config{
		ImageModels: []ImgModelCfg{
			{ID: "test", Provider: "tencent", Enabled: true, IsDefault: true,
				SecretIdEnv: "MISSING_ID", SecretKeyEnv: "MISSING_KEY",
				APIBaseHost: "aiart.tencentcloudapi.com", Region: "ap-guangzhou"},
		},
	}
	tool := newImageGenTool(cfg)
	_, err := tool.Execute(context.Background(), `{"prompt":"test"}`)
	if err == nil || !strings.Contains(err.Error(), ErrImageGenAuth.Error()) {
		t.Errorf("expected auth error, got: %v", err)
	}
}

func TestImageGenExecute_EmptyPrompt(t *testing.T) {
	t.Setenv("TEST_ID", "id")
	t.Setenv("TEST_KEY", "key")
	cfg := Config{
		ImageModels: []ImgModelCfg{
			{ID: "test", Provider: "tencent", Enabled: true, IsDefault: true,
				SecretIdEnv: "TEST_ID", SecretKeyEnv: "TEST_KEY",
				APIBaseHost: "host", Region: "region"},
		},
	}
	tool := newImageGenTool(cfg)
	_, err := tool.Execute(context.Background(), `{"prompt":""}`)
	if err == nil || !strings.Contains(err.Error(), ErrInvalidArgs.Error()) {
		t.Errorf("expected invalid args, got: %v", err)
	}
}
