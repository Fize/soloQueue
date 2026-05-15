package tools

import (
	"encoding/json"
	"testing"
)

// TestShellParameters_WorkingDirectory verifies that Parameters() returns a JSON
// schema that includes the optional working_directory property as a string type.
func TestShellParameters_WorkingDirectory(t *testing.T) {
	tool := &shellExecTool{}

	params := tool.Parameters()

	// Must be valid JSON
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("Parameters() returned invalid JSON: %v", err)
	}

	// Must be an object with properties
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema.properties is missing or not an object")
	}

	wd, ok := props["working_directory"]
	if !ok {
		t.Fatal("schema.properties is missing 'working_directory'")
	}

	wdMap, ok := wd.(map[string]any)
	if !ok {
		t.Fatal("working_directory property is not an object")
	}

	if wdMap["type"] != "string" {
		t.Errorf("working_directory.type = %v, want 'string'", wdMap["type"])
	}

	// Verify it's optional by checking it's NOT in the required list
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("schema.required is missing or not an array")
	}
	for _, r := range required {
		if r == "working_directory" {
			t.Error("working_directory should not be in the required list")
		}
	}
}
