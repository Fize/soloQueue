package qq

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── AudioURL ──────────────────────────────────────────────────────────────────

func TestAudioURL_AudioAttachment(t *testing.T) {
	atts := []QQAttachment{
		{ContentType: "audio/silk", URL: "https://example.com/audio.silk"},
	}
	got := AudioURL(atts)
	want := "https://example.com/audio.silk"
	if got != want {
		t.Errorf("AudioURL() = %q, want %q", got, want)
	}
}

func TestAudioURL_NoAudioAttachment(t *testing.T) {
	atts := []QQAttachment{
		{ContentType: "image/png", URL: "https://example.com/img.png"},
		{ContentType: "application/pdf", URL: "https://example.com/doc.pdf"},
	}
	got := AudioURL(atts)
	if got != "" {
		t.Errorf("AudioURL() = %q, want empty", got)
	}
}

func TestAudioURL_EmptyAttachments(t *testing.T) {
	got := AudioURL(nil)
	if got != "" {
		t.Errorf("AudioURL(nil) = %q, want empty", got)
	}
	got = AudioURL([]QQAttachment{})
	if got != "" {
		t.Errorf("AudioURL(empty) = %q, want empty", got)
	}
}

func TestAudioURL_MultipleAudioReturnsFirst(t *testing.T) {
	atts := []QQAttachment{
		{ContentType: "audio/silk", URL: "https://example.com/first.silk"},
		{ContentType: "audio/amr", URL: "https://example.com/second.amr"},
	}
	got := AudioURL(atts)
	want := "https://example.com/first.silk"
	if got != want {
		t.Errorf("AudioURL() = %q, want %q", got, want)
	}
}

func TestAudioURL_AudioWithoutURL(t *testing.T) {
	atts := []QQAttachment{
		{ContentType: "audio/silk", URL: ""},
	}
	got := AudioURL(atts)
	if got != "" {
		t.Errorf("AudioURL() = %q, want empty", got)
	}
}

// ─── Transcriber ────────────────────────────────────────────────────────────────

func TestNewTranscriber(t *testing.T) {
	tr := NewTranscriber("small", "/tmp/models")
	if tr.model != "small" {
		t.Errorf("model = %q, want small", tr.model)
	}
	if tr.modelDir != "/tmp/models" {
		t.Errorf("modelDir = %q, want /tmp/models", tr.modelDir)
	}
	if tr.Binary() == "" {
		// whisper-cli may not be installed, that's ok — we just verify fields are set
		t.Log("whisper-cli not found in PATH (expected in CI)")
	}
	if tr.Model() != "small" {
		t.Errorf("Model() = %q, want small", tr.Model())
	}
}

func TestTranscriberModelPath(t *testing.T) {
	tr := NewTranscriber("base", "/tmp/models")
	got := tr.ModelPath()
	want := filepath.Join("/tmp/models", "ggml-base.bin")
	if got != want {
		t.Errorf("ModelPath() = %q, want %q", got, want)
	}
}

func TestTranscriberModelPath_Tiny(t *testing.T) {
	tr := NewTranscriber("tiny", "/opt/whisper-models")
	got := tr.ModelPath()
	want := filepath.Join("/opt/whisper-models", "ggml-tiny.bin")
	if got != want {
		t.Errorf("ModelPath() = %q, want %q", got, want)
	}
}

func TestTranscriberAvailable_BinaryNotFound(t *testing.T) {
	// Construct a Transcriber with a fake binary to simulate "not found"
	tr := &Transcriber{binary: "", modelDir: "/tmp", model: "small"}
	if tr.Available() {
		t.Error("Available() should be false when binary is empty")
	}
}

func TestTranscriberAvailable_ModelFileMissing(t *testing.T) {
	dir := t.TempDir()
	tr := &Transcriber{
		binary:   "/usr/bin/whisper-cli", // fake — not actually executed
		modelDir: dir,
		model:    "small",
	}
	// No model file in temp dir
	if tr.Available() {
		t.Error("Available() should be false when model file does not exist")
	}
}

func TestTranscriberAvailable_BothPresent(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "ggml-small.bin")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := &Transcriber{
		binary:   "/usr/bin/whisper-cli", // fake
		modelDir: dir,
		model:    "small",
	}
	if !tr.Available() {
		t.Error("Available() should be true when binary and model file both exist")
	}
}

func TestTranscriberBinary(t *testing.T) {
	tr := &Transcriber{binary: "/usr/local/bin/whisper-cli", modelDir: "/tmp", model: "small"}
	if tr.Binary() != "/usr/local/bin/whisper-cli" {
		t.Errorf("Binary() = %q, want /usr/local/bin/whisper-cli", tr.Binary())
	}
}

func TestTranscriberModel(t *testing.T) {
	tr := &Transcriber{binary: "/usr/bin/whisper-cli", modelDir: "/tmp", model: "medium"}
	if tr.Model() != "medium" {
		t.Errorf("Model() = %q, want medium", tr.Model())
	}
}
