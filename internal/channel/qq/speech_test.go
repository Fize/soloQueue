package qq

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/infra/speechinstall"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ─── AudioURL ──────────────────────────────────────────────────────────────────

func TestIsSilkAudio(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "standard header", data: []byte("#!SILK_V3 payload"), want: true},
		{name: "QQ wrapper", data: append([]byte{0x02}, []byte("#!SILK_V3 payload")...), want: true},
		{name: "ordinary file", data: []byte("not audio"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSilkAudio(tt.data); got != tt.want {
				t.Errorf("isSilkAudio() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTranscribe_DecodesSilkBeforeWhisper(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for speech transcription")
	}
	dir := t.TempDir()
	decoderPath := fakeSpeechTool(t, dir, "silk-decoder", "decode")
	whisperPath := fakeSpeechTool(t, dir, "whisper-cli", "whisper")
	modelPath := filepath.Join(dir, "ggml-small.bin")
	if err := os.WriteFile(modelPath, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := &Transcriber{binary: whisperPath, silkDecoder: decoderPath, modelDir: dir, model: "small"}
	got, err := tr.Transcribe(context.Background(), append([]byte{0x02}, []byte("#!SILK_V3 test")...))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "transcript from whisper" {
		t.Errorf("Transcribe() = %q, want transcript from whisper", got)
	}
}

func TestTranscribe_RejectsEmptyPCM(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for speech transcription")
	}
	dir := t.TempDir()
	decoderPath := fakeSpeechTool(t, dir, "silk-decoder", "empty")

	tr := &Transcriber{
		binary:      filepath.Join(dir, "whisper-cli"),
		silkDecoder: decoderPath,
		modelDir:    dir,
		model:       "small",
	}
	_, err := tr.Transcribe(context.Background(), append([]byte{0x02}, []byte("#!SILK_V3 test")...))
	if err == nil {
		t.Fatal("Transcribe() error = nil, want empty PCM error")
	}
	if !strings.Contains(err.Error(), "empty PCM") {
		t.Fatalf("Transcribe() error = %q, want empty PCM error", err)
	}
}

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
	useFakeSpeechModel(t, "fake model")
	t.Setenv("SOLOQUEUE_WORK_DIR", dir)
	t.Setenv("PATH", t.TempDir())
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, name), []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(dir, "ggml-unit.bin")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0o644); err != nil {
		t.Fatal(err)
	}

	tr := &Transcriber{
		binary:      "/usr/bin/whisper-cli",  // fake
		silkDecoder: "/usr/bin/silk-decoder", // fake
		modelDir:    dir,
		model:       "unit",
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

func TestFindSilkDecoderUsesConfiguredWorkDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOLOQUEUE_WORK_DIR", dir)
	t.Setenv("PATH", t.TempDir())
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "silk-decoder"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	want := filepath.Join(bin, name)
	if err := os.WriteFile(want, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findSilkDecoder(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTranscriberAvailableRequiresFFmpeg(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SOLOQUEUE_WORK_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "ggml-small.bin"), []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := &Transcriber{binary: "whisper", silkDecoder: "silk", modelDir: dir, model: "small"}
	if tr.Available() {
		t.Fatal("transcriber reported ready without FFmpeg")
	}
}

// fakeSpeechTool builds a native helper executable so Windows tests exercise
// the same subprocess interface without requiring a Unix shell.
func fakeSpeechTool(t *testing.T, dir, name, mode string) string {
	t.Helper()
	source := `package main
import ("fmt"; "os"; "strings")
func main() {
 switch MODE {
 case "whisper": fmt.Println("transcript from whisper")
 case "decode", "empty":
  if len(os.Args) != 5 || !strings.HasSuffix(os.Args[1], ".silk") || !strings.HasSuffix(os.Args[2], ".pcm") || os.Args[3] != "-Fs_API" || os.Args[4] != "16000" { os.Exit(1) }
  if MODE == "decode" { if os.WriteFile(os.Args[2], []byte{0,0,0,0}, 0600) != nil { os.Exit(1) } }
 }
}`
	source = strings.ReplaceAll(source, "MODE", "\""+mode+"\"")
	sourcePath := filepath.Join(dir, name+".go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binary, sourcePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, out)
	}
	return binary
}

func TestTranscriberDetectsToolsInstalledAfterCreation(t *testing.T) {
	dir := t.TempDir()
	useFakeSpeechModel(t, "model")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SOLOQUEUE_WORK_DIR", dir)
	tr := NewTranscriber("unit", dir)
	if err := os.WriteFile(filepath.Join(dir, "ggml-unit.bin"), []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ffmpeg", "whisper-cli", "silk-decoder"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(bin, name), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if !tr.Available() {
		t.Fatal("transcriber did not detect installed tools")
	}
}

func TestTranscriberDoesNotRetainRemovedTool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SOLOQUEUE_WORK_DIR", dir)
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "whisper-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(bin, name)
	if err := os.WriteFile(path, []byte("tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr := NewTranscriber("small", dir)
	if tr.Binary() != path {
		t.Fatalf("tool not found: %q", tr.Binary())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := tr.Binary(); got != "" {
		t.Fatalf("removed tool still found: %q", got)
	}
}

func useFakeSpeechModel(t *testing.T, content string) {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	speechinstall.ModelSHA256["unit"] = fmt.Sprintf("%x", sum)
	t.Cleanup(func() { delete(speechinstall.ModelSHA256, "unit") })
}
