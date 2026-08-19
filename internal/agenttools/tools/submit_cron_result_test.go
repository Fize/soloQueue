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

func TestSubmitCronResultSchemaMatchesFrozenContract(t *testing.T) {
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
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 2 || required[0] != "summary" || required[1] != "sections" {
		t.Fatalf("required = %#v, want [summary sections]", schema["required"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 2 || properties["summary"] == nil || properties["sections"] == nil {
		t.Fatalf("properties must contain exactly summary and sections: %#v", schema["properties"])
	}
	sections := properties["sections"].(map[string]any)
	items := sections["items"].(map[string]any)
	if items["additionalProperties"] != false {
		t.Fatalf("section items must reject additional properties: %#v", items)
	}
	itemProperties := items["properties"].(map[string]any)
	if len(itemProperties) != 2 || itemProperties["title"] == nil || itemProperties["content"] == nil {
		t.Fatalf("section properties must be exactly title and content: %#v", itemProperties)
	}
}

func TestSubmitCronResultStrictValidationAndTermination(t *testing.T) {
	tool := newSubmitCronResultTool()
	tests := []struct {
		name string
		args string
	}{
		{name: "malformed JSON", args: `{"summary":`},
		{name: "trailing JSON", args: `{"summary":"ok","sections":[]} {}`},
		{name: "unknown root field", args: `{"summary":"ok","sections":[],"status":"success"}`},
		{name: "missing sections", args: `{"summary":"ok"}`},
		{name: "blank summary", args: `{"summary":" \n\t","sections":[]}`},
		{name: "unknown section field", args: `{"summary":"ok","sections":[{"title":"t","content":"c","extra":true}]}`},
		{name: "blank section title", args: `{"summary":"ok","sections":[{"title":"  ","content":"c"}]}`},
		{name: "blank section content", args: `{"summary":"ok","sections":[{"title":"t","content":"\n"}]}`},
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
	validArgs := `{"summary":"ok","sections":[]}`
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

	input := struct {
		Summary  string `json:"summary"`
		Sections []struct {
			Title   string `json:"title"`
			Content string `json:"content"`
		} `json:"sections"`
	}{Summary: "他说\"观察\"\nC:\\tmp"}
	input.Sections = append(input.Sections, struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}{Title: "结论", Content: "第一行\n第二行\\路径"})
	args, err := json.Marshal(input)
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
	var got map[string]any
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["summary"] != input.Summary {
		t.Fatalf("canonical result changed content or fields: %#v", got)
	}
	if !tool.TerminatesTurn(result, nil) {
		t.Fatal("successful submission must terminate the turn")
	}
}
