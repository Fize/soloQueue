package qq

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsInstalledSpeechTranscribesSilk(t *testing.T) {
	sample := os.Getenv("SOLOQUEUE_SPEECH_SILK_FIXTURE")
	if sample == "" {
		t.Skip("requires the installed Windows speech integration fixture")
	}
	workDir := os.Getenv("SOLOQUEUE_WORK_DIR")
	transcriber := NewTranscriber("tiny", filepath.Join(workDir, "models"))
	if !transcriber.Available() {
		t.Fatal("installed speech tools are not available")
	}
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	transcript, err := transcriber.Transcribe(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(transcript) == "" {
		t.Fatal("transcription is empty")
	}
}

func TestWindowsInstalledSpeechTranscribesAudio(t *testing.T) {
	sample := os.Getenv("SOLOQUEUE_SPEECH_AUDIO_FIXTURE")
	if sample == "" {
		t.Skip("requires the installed Windows speech integration fixture")
	}
	transcriber := NewTranscriber("tiny", filepath.Join(os.Getenv("SOLOQUEUE_WORK_DIR"), "models"))
	if !transcriber.AudioAvailable() {
		t.Fatal("installed audio transcription tools are not available")
	}
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	transcript, err := transcriber.TranscribeAudio(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(transcript) == "" {
		t.Fatal("transcription is empty")
	}
}
