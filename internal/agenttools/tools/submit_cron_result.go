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
  "required": ["summary", "sections"],
  "properties": {
    "summary": {"type": "string", "minLength": 1, "pattern": "\\S"},
    "sections": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["title", "content"],
        "properties": {
          "title": {"type": "string", "minLength": 1, "pattern": "\\S"},
          "content": {"type": "string", "minLength": 1, "pattern": "\\S"}
        }
      }
    }
  }
}`

type submitCronResultTool struct{}

type submittedCronSection struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type submittedCronResult struct {
	Summary  string                 `json:"summary"`
	Sections []submittedCronSection `json:"sections"`
}

func newSubmitCronResultTool() *submitCronResultTool {
	return &submitCronResultTool{}
}

func (t *submitCronResultTool) Name() string { return "SubmitCronResult" }

func (t *submitCronResultTool) Description() string {
	return "Submit the final validated result for a scheduled Cron execution. Call exactly once after all work is complete."
}

func (t *submitCronResultTool) Parameters() json.RawMessage {
	return json.RawMessage(submitCronResultParameters)
}

func (t *submitCronResultTool) Execute(ctx context.Context, args string) (string, error) {
	metadata := telemetryctx.FromContext(ctx)
	if metadata.Origin != telemetryctx.OriginCron || strings.TrimSpace(metadata.RunID) == "" {
		return "", errors.New("cron_result_submission_unauthorized")
	}

	var payload struct {
		Summary  string                  `json:"summary"`
		Sections *[]submittedCronSection `json:"sections"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return "", fmt.Errorf("invalid_cron_result_submission: decode arguments: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return "", fmt.Errorf("invalid_cron_result_submission: trailing arguments: %w", err)
	}
	if strings.TrimSpace(payload.Summary) == "" {
		return "", errors.New("invalid_cron_result_submission: summary must be a non-empty string")
	}
	if payload.Sections == nil {
		return "", errors.New("invalid_cron_result_submission: sections must be an array")
	}
	for i, section := range *payload.Sections {
		if strings.TrimSpace(section.Title) == "" {
			return "", fmt.Errorf("invalid_cron_result_submission: sections[%d].title must be a non-empty string", i)
		}
		if strings.TrimSpace(section.Content) == "" {
			return "", fmt.Errorf("invalid_cron_result_submission: sections[%d].content must be a non-empty string", i)
		}
	}

	canonical, err := json.Marshal(submittedCronResult{Summary: payload.Summary, Sections: *payload.Sections})
	if err != nil {
		return "", errors.New("invalid_cron_result_submission: serialization failed")
	}
	return string(canonical), nil
}

func (t *submitCronResultTool) TerminatesTurn(result string, err error) bool {
	return err == nil && result != ""
}
