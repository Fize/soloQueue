package lsp

import (
	"context"
	"fmt"
)

func (c *Client) supportsTSRequests() bool {
	if c.serverCaps.ExecuteCommandProvider != nil {
		for _, command := range c.serverCaps.ExecuteCommandProvider.Commands {
			if command == "typescript.tsserverRequest" {
				return true
			}
		}
	}
	return false
}

// Push diagnostics can contain an early empty syntax result while a project's
// semantic analysis is still running. Query the completed syntax/semantic
// results so a tool call cannot mistake that interim snapshot for success.
func (c *Client) typescriptDiagnostics(ctx context.Context, uri string) ([]Diagnostic, error) {
	type location struct {
		Line   int `json:"line"`
		Offset int `json:"offset"`
	}
	var diagnostics []Diagnostic
	for _, command := range []string{"syntacticDiagnosticsSync", "semanticDiagnosticsSync", "suggestionDiagnosticsSync"} {
		var response struct {
			Success *bool  `json:"success"`
			Message string `json:"message"`
			Body    []struct {
				Start    location `json:"startLocation"`
				End      location `json:"endLocation"`
				Category string   `json:"category"`
				Code     int      `json:"code"`
				Message  string   `json:"message"`
			} `json:"body"`
		}
		err := c.call(ctx, "workspace/executeCommand", map[string]any{
			"command":   "typescript.tsserverRequest",
			"arguments": []any{command, map[string]any{"file": uriToPath(uri), "includeLinePosition": true}},
		}, &response)
		if err != nil {
			return nil, err
		}
		if response.Success != nil && !*response.Success {
			return nil, fmt.Errorf("TypeScript diagnostics failed: %s", response.Message)
		}
		for _, item := range response.Body {
			severity := SeverityInformation
			switch item.Category {
			case "error":
				severity = SeverityError
			case "warning":
				severity = SeverityWarning
			case "suggestion":
				severity = SeverityHint
			}
			diagnostics = append(diagnostics, Diagnostic{
				Range:    Range{Start: Position{Line: max(0, item.Start.Line-1), Character: max(0, item.Start.Offset-1)}, End: Position{Line: max(0, item.End.Line-1), Character: max(0, item.End.Offset-1)}},
				Severity: severity, Code: item.Code, Source: "typescript", Message: item.Message,
			})
		}
	}
	return diagnostics, nil
}
