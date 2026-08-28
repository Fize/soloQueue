package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

func TestPayloadToLLMMessagesPrefixesEligibleUserMessageOnce(t *testing.T) {
	receivedAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	payload := []ctxwin.PayloadMessage{{
		Role:            "user",
		Content:         "remind me in 30 minutes",
		Timestamp:       receivedAt,
		ExposeTimestamp: true,
	}}

	got := payloadToLLMMessages(payload)
	want := "[2026-08-27 09:35:59] remind me in 30 minutes"
	if len(got) != 1 || got[0].Content != want {
		t.Fatalf("content = %#v, want %q", got, want)
	}
}

func TestPayloadToLLMMessagesDoesNotRebuildTruncatedAggregate(t *testing.T) {
	receivedAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	large := strings.Repeat("a", 2000)
	cw := ctxwin.NewContextWindow(100, 10, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleUser, large+"\n\nchannel-tail", ctxwin.WithTemporalParts([]ctxwin.TemporalPart{
		{Content: large},
		{Content: "channel-tail", Timestamp: receivedAt, ExposeTimestamp: true},
	}))
	payload := cw.BuildPayload()
	if len(payload) != 1 || !strings.Contains(payload[0].Content, "omitted") {
		t.Fatalf("payload = %#v, want context-window truncation", payload)
	}

	got := payloadToLLMMessages(payload)
	want := payload[0].Content
	if len(got) != 1 || got[0].Content != want {
		t.Fatalf("content length=%d, want %d and exact unprefixed truncated payload", len(got[0].Content), len(want))
	}
}

func TestPayloadToLLMMessagesClearsMetadataForTruncatedRepeatedAggregate(t *testing.T) {
	firstAt := time.Date(2026, time.August, 27, 9, 0, 0, 0, time.Local)
	lastAt := firstAt.Add(time.Hour)
	head := "same" + strings.Repeat("h", 496)
	middle := strings.Repeat("m", 1000)
	content := head + "\n\nsame\n\n" + middle + "\n\nsame"
	cw := ctxwin.NewContextWindow(100, 10, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleUser, content, ctxwin.WithTemporalParts([]ctxwin.TemporalPart{
		{Content: head},
		{Content: "same", Timestamp: firstAt, ExposeTimestamp: true},
		{Content: middle},
		{Content: "same", Timestamp: lastAt, ExposeTimestamp: true},
	}))
	payload := cw.BuildPayload()
	got := payloadToLLMMessages(payload)
	if len(got) != 1 || strings.Contains(got[0].Content, "[2026-08-27") || !strings.Contains(got[0].Content, "omitted") {
		t.Fatalf("content = %q, want truncated aggregate with no timestamp prefix", got[0].Content)
	}
}

func TestPayloadToLLMMessagesDoesNotDuplicateExistingTimestampPrefix(t *testing.T) {
	receivedAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	content := "[2026-08-27 09:35:59] remind me"
	payload := []ctxwin.PayloadMessage{{
		Role:            "user",
		Content:         content,
		Timestamp:       receivedAt,
		ExposeTimestamp: true,
	}}

	got := payloadToLLMMessages(payload)
	if len(got) != 1 || got[0].Content != content {
		t.Fatalf("content = %#v, want exactly one prefix in %q", got, content)
	}
}

func TestPayloadToLLMMessagesLeavesIneligibleUserMessageRaw(t *testing.T) {
	payload := []ctxwin.PayloadMessage{{
		Role:      "user",
		Content:   "web request",
		Timestamp: time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local),
	}}

	got := payloadToLLMMessages(payload)
	if len(got) != 1 || got[0].Content != "web request" {
		t.Fatalf("content = %#v, want unmodified ineligible input", got)
	}
}

func TestPayloadToLLMMessagesRendersMixedAggregateParts(t *testing.T) {
	receivedAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	payload := []ctxwin.PayloadMessage{{
		Role:    "user",
		Content: "channel\n\nweb",
		TemporalParts: []ctxwin.TemporalPart{
			{Content: "channel", Timestamp: receivedAt, ExposeTimestamp: true},
			{Content: "web"},
		},
	}}

	got := payloadToLLMMessages(payload)
	if len(got) != 1 || got[0].Content != "[2026-08-27 09:35:59] channel\n\nweb" {
		t.Fatalf("content = %#v, want one aggregate message with selective prefix", got)
	}
}
