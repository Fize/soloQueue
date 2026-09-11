package tools

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"testing"
	"time"
)

func TestRunCommandOptions_ZeroValue(t *testing.T) {
	var opts RunCommandOptions
	if opts.WorkingDirectory != "" {
		t.Errorf("zero value WorkingDirectory = %q, want empty string", opts.WorkingDirectory)
	}
}

func TestRunCommandOptions_JSONUnmarshal_WithoutWorkingDirectory(t *testing.T) {
	type testArgs struct {
		Command          string `json:"command"`
		WorkingDirectory string `json:"working_directory,omitempty"`
	}

	raw := `{"command":"echo hi"}`
	var args testArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if args.WorkingDirectory != "" {
		t.Errorf("WorkingDirectory = %q, want empty string", args.WorkingDirectory)
	}
	if args.Command != "echo hi" {
		t.Errorf("Command = %q, want %q", args.Command, "echo hi")
	}
}

func TestExecutorContextComplianceContainsNoGoStatement(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "exec.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{"ReadFile": false, "Glob": false}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, targeted := targets[function.Name.Name]; !targeted {
			continue
		}
		targets[function.Name.Name] = true
		found := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if _, ok := node.(*ast.GoStmt); ok {
				found = true
			}
			return true
		})
		if found {
			t.Errorf("Executor.%s must not detach work from its context", function.Name.Name)
		}
	}
	for name, found := range targets {
		if !found {
			t.Errorf("Executor.%s declaration not found", name)
		}
	}
}

type statWithoutOpenFS struct {
	openCalls int
	statCalls int
}

func (f *statWithoutOpenFS) Open(string) (fs.File, error) {
	f.openCalls++
	return nil, errors.New("open must not be called")
}

func (f *statWithoutOpenFS) Stat(string) (fs.FileInfo, error) {
	f.statCalls++
	return stubFileInfo{name: "literal", mode: 0o600}, nil
}

type stubFileInfo struct {
	name string
	mode fs.FileMode
}

func (f stubFileInfo) Name() string       { return f.name }
func (f stubFileInfo) Size() int64        { return 0 }
func (f stubFileInfo) Mode() fs.FileMode  { return f.mode }
func (f stubFileInfo) ModTime() time.Time { return time.Time{} }
func (f stubFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f stubFileInfo) Sys() any           { return nil }

func TestContextFSStatDoesNotFallBackToOpen(t *testing.T) {
	underlying := &statWithoutOpenFS{}
	wrapped := contextFS{ctx: context.Background(), fsys: underlying}
	info, err := fs.Stat(wrapped, "literal")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name() != "literal" {
		t.Fatalf("name = %q, want literal", info.Name())
	}
	if underlying.statCalls != 1 || underlying.openCalls != 0 {
		t.Fatalf("stat calls = %d, open calls = %d", underlying.statCalls, underlying.openCalls)
	}
}
