package agent

import (
	"strings"
	"testing"
)

func TestBuildGeminiArgsBaseline(t *testing.T) {
	t.Parallel()

	b := &geminiBackend{}
	args := b.buildGeminiArgs("write a haiku", externalExecOptions{})
	expected := []string{
		"-p", "write a haiku",
		"--yolo",
		"-o", "stream-json",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("expected args[%d] = %q, got %q", i, want, args[i])
		}
	}
}

func TestBuildGeminiArgsWithModel(t *testing.T) {
	t.Parallel()

	b := &geminiBackend{}
	args := b.buildGeminiArgs("hi", externalExecOptions{Model: "gemini-2.5-pro"})

	var foundModel bool
	for i, a := range args {
		if a == "-m" {
			if i+1 >= len(args) || args[i+1] != "gemini-2.5-pro" {
				t.Fatalf("expected -m followed by gemini-2.5-pro, got %v", args)
			}
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatalf("expected -m flag when Model is set, got args=%v", args)
	}
}

func TestBuildGeminiArgsWithResume(t *testing.T) {
	t.Parallel()

	b := &geminiBackend{}
	args := b.buildGeminiArgs("hi", externalExecOptions{ResumeSessionID: "3"})

	var foundResume bool
	for i, a := range args {
		if a == "-r" {
			if i+1 >= len(args) || args[i+1] != "3" {
				t.Fatalf("expected -r followed by session id, got %v", args)
			}
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Fatalf("expected -r flag when ResumeSessionID is set, got args=%v", args)
	}
}

func TestBuildGeminiArgsOmitsModelWhenEmpty(t *testing.T) {
	t.Parallel()

	b := &geminiBackend{}
	args := b.buildGeminiArgs("hi", externalExecOptions{})
	for _, a := range args {
		if a == "-m" {
			t.Fatalf("expected no -m flag when Model is empty, got args=%v", args)
		}
		if a == "-r" {
			t.Fatalf("expected no -r flag when ResumeSessionID is empty, got args=%v", args)
		}
	}
}

func TestBuildGeminiArgsPassesThroughCustomArgs(t *testing.T) {
	t.Parallel()

	b := &geminiBackend{}
	args := b.buildGeminiArgs("hi", externalExecOptions{
		CustomArgs: []string{"--sandbox"},
	})

	if args[len(args)-1] != "--sandbox" {
		t.Fatalf("expected --sandbox at end of args, got %v", args)
	}
}

func envLookup(env []string, key string) (string, bool) {
	prefix := key + "="
	var value string
	var found bool
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
			found = true
		}
	}
	return value, found
}

func TestBuildGeminiEnvSetsTrustWorkspaceDefault(t *testing.T) {
	t.Parallel()

	env := buildGeminiEnv(nil)
	got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE")
	if !ok {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE to be set, got env=%v", env)
	}
	if got != "true" {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE=true, got %q", got)
	}
}

func TestBuildGeminiEnvRespectsExplicitOverride(t *testing.T) {
	t.Parallel()

	env := buildGeminiEnv(map[string]string{"GEMINI_CLI_TRUST_WORKSPACE": "false"})
	got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE")
	if !ok {
		t.Fatalf("expected GEMINI_CLI_TRUST_WORKSPACE to be set, got env=%v", env)
	}
	if got != "false" {
		t.Fatalf("expected caller's GEMINI_CLI_TRUST_WORKSPACE=false to win, got %q", got)
	}
}

func TestBuildGeminiEnvPreservesOtherExtras(t *testing.T) {
	t.Parallel()

	env := buildGeminiEnv(map[string]string{"GEMINI_API_KEY": "secret"})
	if got, ok := envLookup(env, "GEMINI_API_KEY"); !ok || got != "secret" {
		t.Fatalf("expected GEMINI_API_KEY=secret to pass through, got %q (ok=%v)", got, ok)
	}
	if got, ok := envLookup(env, "GEMINI_CLI_TRUST_WORKSPACE"); !ok || got != "true" {
		t.Fatalf("expected default GEMINI_CLI_TRUST_WORKSPACE=true, got %q (ok=%v)", got, ok)
	}
}

func TestBuildGeminiArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	b := &geminiBackend{}
	args := b.buildGeminiArgs("hi", externalExecOptions{
		CustomArgs: []string{"-o", "text", "--sandbox", "-r", "some-session"},
	})

	// -o text should be filtered, -r some-session should be filtered, --sandbox should pass through
	for i, a := range args {
		if a == "-o" && i+1 < len(args) && args[i+1] == "text" {
			t.Fatalf("blocked -o text should have been filtered: %v", args)
		}
		if a == "-r" && i+1 < len(args) && args[i+1] == "some-session" {
			t.Fatalf("blocked -r some-session should have been filtered: %v", args)
		}
	}
	if args[len(args)-1] != "--sandbox" {
		t.Fatalf("expected --sandbox to pass through, got %v", args)
	}
}
