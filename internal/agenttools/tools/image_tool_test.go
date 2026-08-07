package tools

import (
	"context"
	"encoding/json"
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
