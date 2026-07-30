package qq

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var silkMagic = []byte("#!SILK_V3")

// isSilkAudio reports whether data is a QQ/WeChat SILK V3 stream. Tencent may
// prefix the normal SILK header with a one-byte 0x02 wrapper.
func isSilkAudio(data []byte) bool {
	return bytes.HasPrefix(data, silkMagic) || (len(data) > 1 && data[0] == 0x02 && bytes.HasPrefix(data[1:], silkMagic))
}

// Transcriber converts audio (SILK format from QQ) to text using whisper.cpp.
// It shells out to silk-decoder for SILK→PCM, ffmpeg for PCM→WAV, and
// whisper-cli for transcription.
type Transcriber struct {
	binary      string // absolute path to whisper-cli, or empty if not found
	silkDecoder string // absolute path to silk-decoder, or empty if not found
	modelDir    string // directory containing ggml model files
	model       string // model name (tiny/base/small/medium)
}

// NewTranscriber creates a Transcriber. Required binaries are auto-detected.
func NewTranscriber(model, modelDir string) *Transcriber {
	return &Transcriber{
		binary:      findWhisperBinary(),
		silkDecoder: findSilkDecoder(),
		modelDir:    modelDir,
		model:       model,
	}
}

// Available returns whether all required binaries and the model file are present.
func (t *Transcriber) Available() bool {
	if t.binary == "" || t.silkDecoder == "" {
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

// Transcribe converts SILK audio data to 16kHz WAV, then runs whisper-cli for
// speech-to-text. Returns the transcript text.
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

	// Step 2: decode SILK → 16kHz mono PCM. ffmpeg does not ship a SILK demuxer.
	pcmFile, err := os.CreateTemp(tmpDir, "soloqueue_audio_*.pcm")
	if err != nil {
		return "", fmt.Errorf("create temp pcm file: %w", err)
	}
	pcmPath := pcmFile.Name()
	pcmFile.Close()
	defer os.Remove(pcmPath)

	decode := exec.CommandContext(ctx, t.silkDecoder, "-Fs_API", "16000", silkPath, pcmPath)
	if out, err := decode.CombinedOutput(); err != nil {
		return "", fmt.Errorf("silk decode: %w\n%s", err, string(out))
	}

	// Step 3: convert raw PCM → WAV for whisper-cli.
	wavFile, err := os.CreateTemp(tmpDir, "soloqueue_audio_*.wav")
	if err != nil {
		return "", fmt.Errorf("create temp wav file: %w", err)
	}
	wavPath := wavFile.Name()
	wavFile.Close()
	defer os.Remove(wavPath)

	convert := exec.CommandContext(ctx, "ffmpeg",
		"-y",          // overwrite output
		"-f", "s16le", // signed 16-bit little-endian PCM
		"-ar", "16000", // 16kHz sample rate
		"-ac", "1", // mono
		"-i", pcmPath,
		"-f", "wav",
		wavPath,
	)
	if out, err := convert.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg convert pcm→wav: %w\n%s", err, string(out))
	}

	// Step 4: transcribe via whisper-cli
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

// SilkDecoder returns the detected silk-decoder path (may be empty).
func (t *Transcriber) SilkDecoder() string { return t.silkDecoder }

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

func findSilkDecoder() string {
	if path, err := exec.LookPath("silk-decoder"); err == nil {
		return path
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	name := "silk-decoder"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(homeDir, ".soloqueue", "bin", name)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}
