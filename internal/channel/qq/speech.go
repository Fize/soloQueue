package qq

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Transcriber converts audio (SILK format from QQ) to text using whisper.cpp.
// It shells out to ffmpeg for SILK→WAV conversion and whisper-cli for transcription.
type Transcriber struct {
	binary   string // absolute path to whisper-cli, or empty if not found
	modelDir string // directory containing ggml model files
	model    string // model name (tiny/base/small/medium)
}

// NewTranscriber creates a Transcriber. binary is auto-detected from PATH.
func NewTranscriber(model, modelDir string) *Transcriber {
	return &Transcriber{
		binary:   findWhisperBinary(),
		modelDir: modelDir,
		model:    model,
	}
}

// Available returns whether both whisper-cli and the model file are present.
func (t *Transcriber) Available() bool {
	if t.binary == "" {
		return false
	}
	if _, err := os.Stat(t.ModelPath()); err != nil {
		return false
	}
	return true
}

// ModelPath returns the full path to the expected ggml model file.
func (t *Transcriber) ModelPath() string {
	return filepath.Join(t.modelDir, "ggml-"+t.model+".bin")
}

// Transcribe converts SILK audio data to 16kHz WAV via ffmpeg, then runs
// whisper-cli for speech-to-text. Returns the transcript text.
func (t *Transcriber) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	tmpDir := os.TempDir()

	// Step 1: write SILK data to temp file
	silkFile, err := os.CreateTemp(tmpDir, "soloqueue_audio_*.silk")
	if err != nil {
		return "", fmt.Errorf("create temp silk file: %w", err)
	}
	silkPath := silkFile.Name()
	defer os.Remove(silkPath)

	if _, err := silkFile.Write(audioData); err != nil {
		silkFile.Close()
		return "", fmt.Errorf("write silk data: %w", err)
	}
	silkFile.Close()

	// Step 2: convert SILK → 16kHz mono WAV via ffmpeg
	wavFile, err := os.CreateTemp(tmpDir, "soloqueue_audio_*.wav")
	if err != nil {
		return "", fmt.Errorf("create temp wav file: %w", err)
	}
	wavPath := wavFile.Name()
	wavFile.Close()
	defer os.Remove(wavPath)

	convert := exec.CommandContext(ctx, "ffmpeg",
		"-y",             // overwrite output
		"-f", "silk",     // input format
		"-i", silkPath,
		"-ar", "16000",   // 16kHz sample rate
		"-ac", "1",       // mono
		"-f", "wav",
		wavPath,
	)
	if out, err := convert.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg convert silk→wav: %w\n%s", err, string(out))
	}

	// Step 3: transcribe via whisper-cli
	transcribe := exec.CommandContext(ctx, t.binary,
		"-m", t.ModelPath(),
		"-f", wavPath,
		"-l", "zh",
		"--no-timestamps",
		"-otxt",
	)
	out, err := transcribe.Output()
	if err != nil {
		return "", fmt.Errorf("whisper transcribe: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// Binary returns the detected whisper-cli path (may be empty).
func (t *Transcriber) Binary() string { return t.binary }

// Model returns the whisper model name.
func (t *Transcriber) Model() string { return t.model }

// findWhisperBinary looks for whisper-cli in PATH. Supports both Unix and Windows.
func findWhisperBinary() string {
	names := []string{"whisper-cli", "whisper-cli.exe"}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
