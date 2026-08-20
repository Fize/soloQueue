package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
)

const submitCronResultParameters = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "content": {"type": "string"},
    "artifacts": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name", "ref"],
        "properties": {
          "name": {"type": "string", "minLength": 1, "pattern": "\\S"},
          "ref": {"type": "string", "minLength": 1, "pattern": "\\S"}
        }
      }
    }
  },
  "anyOf": [
    {"required": ["content"], "properties": {"content": {"type": "string", "pattern": "\\S"}}},
    {"required": ["artifacts"], "properties": {"artifacts": {"type": "array", "minItems": 1}}}
  ]
}`

type submitCronResultTool struct{}

type submittedCronArtifact struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

type submittedCronResult struct {
	Content   string                  `json:"content"`
	Artifacts []submittedCronArtifact `json:"artifacts"`
}

func newSubmitCronResultTool() *submitCronResultTool {
	return &submitCronResultTool{}
}

func (t *submitCronResultTool) Name() string { return "SubmitCronResult" }

func (t *submitCronResultTool) Description() string {
	return "Submit opaque final content and optional artifact references for a scheduled Cron execution. Call exactly once after all work is complete."
}

func (t *submitCronResultTool) Parameters() json.RawMessage {
	return json.RawMessage(submitCronResultParameters)
}

func (t *submitCronResultTool) Execute(ctx context.Context, args string) (string, error) {
	metadata := telemetryctx.FromContext(ctx)
	if metadata.Origin != telemetryctx.OriginCron || strings.TrimSpace(metadata.RunID) == "" {
		return "", errors.New("cron_result_submission_unauthorized")
	}

	var wire struct {
		Content   json.RawMessage `json:"content"`
		Artifacts json.RawMessage `json:"artifacts"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return "", fmt.Errorf("invalid_cron_result_submission: decode arguments: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return "", fmt.Errorf("invalid_cron_result_submission: trailing arguments: %w", err)
	}

	content := ""
	if wire.Content != nil {
		if bytes.Equal(bytes.TrimSpace(wire.Content), []byte("null")) {
			return "", errors.New("invalid_cron_result_submission: content must be a string")
		}
		if err := json.Unmarshal(wire.Content, &content); err != nil {
			return "", fmt.Errorf("invalid_cron_result_submission: content must be a string: %w", err)
		}
	}
	if strings.TrimSpace(content) == "" {
		content = ""
	}

	artifacts := []submittedCronArtifact{}
	if wire.Artifacts != nil {
		if bytes.Equal(bytes.TrimSpace(wire.Artifacts), []byte("null")) {
			return "", errors.New("invalid_cron_result_submission: artifacts must be an array")
		}
		artifactDecoder := json.NewDecoder(bytes.NewReader(wire.Artifacts))
		artifactDecoder.DisallowUnknownFields()
		if err := artifactDecoder.Decode(&artifacts); err != nil {
			return "", fmt.Errorf("invalid_cron_result_submission: artifacts must be an array of name/ref objects: %w", err)
		}
	}
	for i, artifact := range artifacts {
		if strings.TrimSpace(artifact.Name) == "" {
			return "", fmt.Errorf("invalid_cron_result_submission: artifacts[%d].name must be a non-empty string", i)
		}
		if strings.TrimSpace(artifact.Ref) == "" {
			return "", fmt.Errorf("invalid_cron_result_submission: artifacts[%d].ref must be a non-empty string", i)
		}
	}
	if content == "" && len(artifacts) == 0 {
		return "", errors.New("invalid_cron_result_submission: content or at least one artifact is required")
	}

	canonical, err := json.Marshal(submittedCronResult{Content: content, Artifacts: artifacts})
	if err != nil {
		return "", errors.New("invalid_cron_result_submission: serialization failed")
	}
	return string(canonical), nil
}

func (t *submitCronResultTool) TerminatesTurn(result string, err error) bool {
	return err == nil && result != ""
}
