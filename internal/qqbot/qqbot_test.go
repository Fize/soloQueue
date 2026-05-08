package qqbot

import (
	"strings"
	"testing"
)

func TestDefaultIntents(t *testing.T) {
	got := DefaultIntents()
	want := IntentGroupAndC2CEvent | IntentPublicGuildMessages
	if got != want {
		t.Errorf("DefaultIntents() = %d, want %d", got, want)
	}
}

func TestConfigEffectiveIntents(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		want     int
	}{
		{
			name: "zero intents uses default",
			cfg:  Config{Intents: 0},
			want: DefaultIntents(),
		},
		{
			name: "explicit intents override default",
			cfg:  Config{Intents: IntentGroupAndC2CEvent},
			want: IntentGroupAndC2CEvent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.EffectiveIntents(); got != tt.want {
				t.Errorf("Config.EffectiveIntents() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConfigAPIBaseURL(t *testing.T) {
	cfg := Config{Sandbox: false}
	if got := cfg.APIBaseURL(); got != "https://api.sgroup.qq.com" {
		t.Errorf("APIBaseURL() = %s, want https://api.sgroup.qq.com", got)
	}
	cfg.Sandbox = true
	if got := cfg.APIBaseURL(); got != "https://sandbox.api.sgroup.qq.com" {
		t.Errorf("APIBaseURL() sandbox = %s, want https://sandbox.api.sgroup.qq.com", got)
	}
}

func TestConfigTokenFormat(t *testing.T) {
	cfg := Config{AppID: "123456", AppSecret: "mysecret"}
	want := "Bot 123456.mysecret"
	if got := cfg.TokenFormat(); got != want {
		t.Errorf("TokenFormat() = %s, want %s", got, want)
	}
}

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		count  int // expected number of chunks
	}{
		{
			name:   "short message no split",
			text:   "hello",
			maxLen: 100,
			count:  1,
		},
		{
			name:   "exact fit no split",
			text:   strings.Repeat("a", 10),
			maxLen: 10,
			count:  1,
		},
		{
			name:   "split at newline",
			text:   "line1\nline2\nline3",
			maxLen: 8,
			count:  3, // "line1\n" (6) + "line2\n" (6) + "line3" (5)
		},
		{
			name:   "no newline hard split",
			text:   strings.Repeat("a", 25),
			maxLen: 10,
			count:  3, // 10 + 10 + 5
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := splitMessage(tt.text, tt.maxLen)
			if len(chunks) != tt.count {
				t.Errorf("splitMessage() returned %d chunks, want %d", len(chunks), tt.count)
			}
			// Verify no chunk exceeds maxLen
			for i, chunk := range chunks {
				if len(chunk) > tt.maxLen {
					t.Errorf("chunk %d exceeds maxLen: %d > %d", i, len(chunk), tt.maxLen)
				}
			}
			// Verify concatenation produces original text
			joined := strings.Join(chunks, "")
			if joined != tt.text {
				t.Errorf("joined chunks don't match original text")
			}
		})
	}
}
