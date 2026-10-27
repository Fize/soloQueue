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

	"github.com/xiaobaitu/soloqueue/internal/infra/speechinstall"
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
		modelDir: modelDir,
		model:    model,
	}
}

// Available returns whether all required binaries and the model file are present.
func (t *Transcriber) Available() bool {
	if t.Binary() == "" || t.SilkDecoder() == "" || t.FFmpeg() == "" {
		return false
	}
	return speechinstall.VerifyModel(t.ModelPath(), t.model) == nil
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

	decode := exec.CommandContext(ctx, t.SilkDecoder(), silkPath, pcmPath, "-Fs_API", "16000")
	if out, err := decode.CombinedOutput(); err != nil {
		return "", fmt.Errorf("silk decode: %w\n%s", err, string(out))
	}
	if info, err := os.Stat(pcmPath); err != nil {
		return "", fmt.Errorf("silk decode output missing: %w", err)
	} else if info.Size() == 0 {
		return "", fmt.Errorf("silk decode produced empty PCM")
	}

	// Step 3: convert raw PCM → WAV for whisper-cli.
	wavFile, err := os.CreateTemp(tmpDir, "soloqueue_audio_*.wav")
	if err != nil {
		return "", fmt.Errorf("create temp wav file: %w", err)
	}
	wavPath := wavFile.Name()
	wavFile.Close()
	defer os.Remove(wavPath)

	convert := exec.CommandContext(ctx, findSpeechTool("ffmpeg"),
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
	transcribe := exec.CommandContext(ctx, t.Binary(),
		"-m", t.ModelPath(),
		"-f", wavPath,
		"-l", "zh",
		"--no-timestamps",
	)
	out, err := transcribe.Output()
	if err != nil {
		return "", fmt.Errorf("whisper transcribe: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func (t *Transcriber) AudioAvailable() bool {
	if t.Binary() == "" {
		return false
	}
	return speechinstall.VerifyModel(t.ModelPath(), t.model) == nil && findSpeechTool("ffmpeg") != ""
}

func (t *Transcriber) TranscribeAudio(ctx context.Context, audioData []byte) (string, error) {
	in, err := os.CreateTemp(os.TempDir(), "soloqueue_audio_*.input")
	if err != nil {
		return "", err
	}
	inPath := in.Name()
	defer os.Remove(inPath)
	if _, err := in.Write(audioData); err != nil {
		in.Close()
		return "", err
	}
	in.Close()
	out, err := os.CreateTemp(os.TempDir(), "soloqueue_audio_*.wav")
	if err != nil {
		return "", err
	}
	outPath := out.Name()
	out.Close()
	defer os.Remove(outPath)
	convert := exec.CommandContext(ctx, findSpeechTool("ffmpeg"), "-y", "-i", inPath, "-ar", "16000", "-ac", "1", "-f", "wav", outPath)
	if output, err := convert.CombinedOutput(); err != nil {
		return "", fmt.Errorf("audio convert: %w\n%s", err, output)
	}
	cmd := exec.CommandContext(ctx, t.Binary(), "-m", t.ModelPath(), "-f", outPath, "-l", "zh", "--no-timestamps")
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("whisper transcribe: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Binary returns the detected whisper-cli path (may be empty).
func (t *Transcriber) Binary() string {
	if t.binary != "" {
		return t.binary
	}
	return findWhisperBinary()
}

// SilkDecoder returns the detected silk-decoder path (may be empty).
func (t *Transcriber) SilkDecoder() string {
	if t.silkDecoder != "" {
		return t.silkDecoder
	}
	return findSilkDecoder()
}

// FFmpeg returns the detected FFmpeg path.
func (t *Transcriber) FFmpeg() string { return findSpeechTool("ffmpeg") }

// Model returns the whisper model name.
func (t *Transcriber) Model() string { return t.model }

// findSpeechTool locates an existing binary without relying on the shell's PATH.
func findSpeechTool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	workDir := os.Getenv("SOLOQUEUE_WORK_DIR")
	if workDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		workDir = filepath.Join(home, ".soloqueue")
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	paths := []string{filepath.Join(workDir, "bin", name)}
	if runtime.GOOS == "windows" {
		switch name {
		case "whisper-cli.exe":
			paths = append(paths, filepath.Join(workDir, "bin", speechinstall.WhisperWindows.Directory, name))
		case "ffmpeg.exe":
			paths = append(paths, filepath.Join(workDir, "bin", speechinstall.FFmpegWindows.Directory, name))
		case "silk-decoder.exe":
			paths = append(paths, filepath.Join(workDir, "bin", speechinstall.DecoderDirectory, name))
		}
	}
	for index, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0) {
			if index > 0 && speechinstall.VerifyInstallation(filepath.Dir(path), name) != nil {
				continue
			}
			return path
		}
	}
	return ""
}

func findWhisperBinary() string { return findSpeechTool("whisper-cli") }
func findSilkDecoder() string   { return findSpeechTool("silk-decoder") }
