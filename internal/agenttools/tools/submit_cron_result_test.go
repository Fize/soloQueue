package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
)

func cronToolContext(runID string) context.Context {
	return telemetryctx.WithMetadata(context.Background(), telemetryctx.Metadata{
		Origin: telemetryctx.OriginCron,
		RunID:  runID,
	})
}

func TestSubmitCronResultSchemaMatchesGenericContract(t *testing.T) {
	tool := newSubmitCronResultTool()
	if tool.Name() != "SubmitCronResult" {
		t.Fatalf("Name = %q, want SubmitCronResult", tool.Name())
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Parameters(), &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("unexpected root schema: %#v", schema)
	}
	if _, exists := schema["required"]; exists {
		t.Fatalf("content and artifacts must each be optional: %#v", schema["required"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 2 || properties["content"] == nil || properties["artifacts"] == nil {
		t.Fatalf("properties must contain exactly content and artifacts: %#v", schema["properties"])
	}
	artifacts := properties["artifacts"].(map[string]any)
	items := artifacts["items"].(map[string]any)
	if items["additionalProperties"] != false {
		t.Fatalf("artifact items must reject additional properties: %#v", items)
	}
	required, ok := items["required"].([]any)
	if !ok || len(required) != 2 || required[0] != "name" || required[1] != "ref" {
		t.Fatalf("artifact required fields = %#v, want [name ref]", items["required"])
	}
	itemProperties := items["properties"].(map[string]any)
	if len(itemProperties) != 2 || itemProperties["name"] == nil || itemProperties["ref"] == nil {
		t.Fatalf("artifact properties must be exactly name and ref: %#v", itemProperties)
	}
}

func TestSubmitCronResultAcceptsGenericPayloads(t *testing.T) {
	tool := newSubmitCronResultTool()
	tests := []struct {
		name          string
		args          string
		wantContent   string
		wantArtifacts []submittedCronArtifact
	}{
		{
			name:        "report-like opaque markdown",
			args:        `{"content":"# 午间复盘\n\n他说\"保持谨慎\"。\n- 不解释业务结构"}`,
			wantContent: "# 午间复盘\n\n他说\"保持谨慎\"。\n- 不解释业务结构",
		},
		{
			name:        "action reminder content",
			args:        `{"content":"提醒：15:30 检查服务状态。"}`,
			wantContent: "提醒：15:30 检查服务状态。",
		},
		{
			name:          "artifact only",
			args:          `{"artifacts":[{"name":"完整报告","ref":"reports/daily.md"}]}`,
			wantArtifacts: []submittedCronArtifact{{Name: "完整报告", Ref: "reports/daily.md"}},
		},
		{
			name:          "content and artifacts",
			args:          `{"content":"处理完成","artifacts":[{"name":"日志","ref":"logs/run-1.txt"}]}`,
			wantContent:   "处理完成",
			wantArtifacts: []submittedCronArtifact{{Name: "日志", Ref: "logs/run-1.txt"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(cronToolContext("run-1"), tt.args)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			var got submittedCronResult
			if err := json.Unmarshal([]byte(result), &got); err != nil {
				t.Fatalf("canonical result is not JSON: %v", err)
			}
			if got.Content != tt.wantContent || len(got.Artifacts) != len(tt.wantArtifacts) {
				t.Fatalf("canonical result = %+v", got)
			}
			for i := range got.Artifacts {
				if got.Artifacts[i] != tt.wantArtifacts[i] {
					t.Fatalf("artifact[%d] = %+v, want %+v", i, got.Artifacts[i], tt.wantArtifacts[i])
				}
			}
			var fields map[string]any
			if err := json.Unmarshal([]byte(result), &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 2 || fields["content"] == nil || fields["artifacts"] == nil {
				t.Fatalf("canonical result must contain exactly content and artifacts: %s", result)
			}
			if !tool.TerminatesTurn(result, nil) {
				t.Fatal("valid submission must terminate the turn")
			}
		})
	}
}

func TestSubmitCronResultStrictValidationAndTermination(t *testing.T) {
	tool := newSubmitCronResultTool()
	tests := []struct {
		name string
		args string
	}{
		{name: "malformed JSON", args: `{"content":`},
		{name: "trailing JSON", args: `{"content":"ok"} {}`},
		{name: "unknown root field", args: `{"content":"ok","summary":"legacy"}`},
		{name: "empty object", args: `{}`},
		{name: "blank content", args: `{"content":" \n\t"}`},
		{name: "empty artifacts", args: `{"artifacts":[]}`},
		{name: "blank content and empty artifacts", args: `{"content":" ","artifacts":[]}`},
		{name: "null content", args: `{"content":null,"artifacts":[{"name":"n","ref":"r"}]}`},
		{name: "wrong content type", args: `{"content":42}`},
		{name: "null artifacts", args: `{"content":"ok","artifacts":null}`},
		{name: "wrong artifacts type", args: `{"artifacts":{}}`},
		{name: "unknown artifact field", args: `{"artifacts":[{"name":"n","ref":"r","extra":true}]}`},
		{name: "missing artifact name", args: `{"artifacts":[{"ref":"r"}]}`},
		{name: "missing artifact ref", args: `{"artifacts":[{"name":"n"}]}`},
		{name: "blank artifact name", args: `{"artifacts":[{"name":"  ","ref":"r"}]}`},
		{name: "blank artifact ref", args: `{"artifacts":[{"name":"n","ref":"\n"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(cronToolContext("run-1"), tt.args)
			if err == nil || !strings.HasPrefix(err.Error(), "invalid_cron_result_submission:") {
				t.Fatalf("Execute error = %v, want stable validation error", err)
			}
			if tool.TerminatesTurn(result, err) {
				t.Fatal("invalid submission must not terminate the turn")
			}
		})
	}
}

func TestSubmitCronResultAuthorizationAndCanonicalEscaping(t *testing.T) {
	tool := newSubmitCronResultTool()
	validArgs := `{"content":"ok"}`
	for _, ctx := range []context.Context{
		context.Background(),
		telemetryctx.WithMetadata(context.Background(), telemetryctx.Metadata{Origin: telemetryctx.OriginCron}),
		telemetryctx.WithMetadata(context.Background(), telemetryctx.Metadata{Origin: telemetryctx.OriginAPI, RunID: "run-1"}),
	} {
		result, err := tool.Execute(ctx, validArgs)
		if err == nil || err.Error() != "cron_result_submission_unauthorized" {
			t.Fatalf("unauthorized Execute error = %v", err)
		}
		if tool.TerminatesTurn(result, err) {
			t.Fatal("unauthorized submission must not terminate the turn")
		}
	}

	content := "他说\"观察\"\nC:\\tmp"
	args, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(cronToolContext("run-escape"), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !json.Valid([]byte(result)) || !strings.Contains(result, `\"观察\"`) || !strings.Contains(result, `\n`) || !strings.Contains(result, `\\tmp`) {
		t.Fatalf("result is not canonical escaped JSON: %s", result)
	}
	var got submittedCronResult
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatal(err)
	}
	if got.Content != content || len(got.Artifacts) != 0 {
		t.Fatalf("canonical result changed content or defaults: %+v", got)
	}
}
