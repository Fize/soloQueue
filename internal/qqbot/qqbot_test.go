package qqbot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestAPIClient_UploadFile_Base64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assert method and endpoint
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v2/groups/group123/files" {
			t.Errorf("Path = %q, want /v2/groups/group123/files", r.URL.Path)
		}

		// Assert headers
		if r.Header.Get("Authorization") != "QQBot mock-token" {
			t.Errorf("Authorization = %q, want 'QQBot mock-token'", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want 'application/json'", r.Header.Get("Content-Type"))
		}

		// Read and assert body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}

		if req["file_type"] != float64(1) { // JSON numbers decode as float64 by default
			t.Errorf("file_type = %v, want 1", req["file_type"])
		}
		if req["file_data"] != "iVBORw0KGgoAAA..." {
			t.Errorf("file_data = %v, want 'iVBORw0KGgoAAA...'", req["file_data"])
		}
		if _, exists := req["url"]; exists {
			t.Errorf("url should not exist in body, got %v", req["url"])
		}

		// Write response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"file_uuid": "uuid123",
			"file_info": "info123",
			"ttl": 3600
		}`))
	}))
	defer server.Close()

	client := NewAPIClient(Config{}, nil)
	client.accessToken = "mock-token"
	client.expiresAt = time.Now().Add(1 * time.Hour)
	client.baseURL = server.URL

	fi, err := client.UploadFile(context.Background(), "group", "group123", 1, "", "iVBORw0KGgoAAA...")
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}

	if fi.FileUUID != "uuid123" {
		t.Errorf("FileUUID = %q, want 'uuid123'", fi.FileUUID)
	}
	if fi.FileInfo != "info123" {
		t.Errorf("FileInfo = %q, want 'info123'", fi.FileInfo)
	}
	if fi.TTL != 3600 {
		t.Errorf("TTL = %d, want 3600", fi.TTL)
	}
}
