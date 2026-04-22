package deepseek

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSSE_SimpleDataLine(t *testing.T) {
	input := "data: {\"foo\":1}\n\n"
	r := newSSEReader(strings.NewReader(input))

	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != `{"foo":1}` {
		t.Errorf("payload = %q", p)
	}

	// 后续应 EOF
	_, err = r.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("want EOF, got %v", err)
	}
}

func TestSSE_MultipleLines(t *testing.T) {
	input := "data: a\n\ndata: b\n\ndata: c\n\n"
	r := newSSEReader(strings.NewReader(input))

	for _, want := range []string{"a", "b", "c"} {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("Next(%s): %v", want, err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	_, err := r.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("after all lines: want EOF, got %v", err)
	}
}

func TestSSE_CommentLinesSkipped(t *testing.T) {
	input := ": keep-alive\n\n: ping\n\ndata: real\n\n"
	r := newSSEReader(strings.NewReader(input))

	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != "real" {
		t.Errorf("payload = %q, want real", p)
	}
}

func TestSSE_BlankLinesSkipped(t *testing.T) {
	// 连续空行（SSE event boundary）不报错
	input := "\n\n\n\ndata: x\n\n"
	r := newSSEReader(strings.NewReader(input))
	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != "x" {
		t.Errorf("payload = %q", p)
	}
}

func TestSSE_DoneMarker(t *testing.T) {
	input := "data: first\n\ndata: [DONE]\n\n"
	r := newSSEReader(strings.NewReader(input))

	p, err := r.Next()
	if err != nil {
		t.Fatalf("first Next: %v", err)
	}
	if p != "first" {
		t.Errorf("payload = %q", p)
	}

	_, err = r.Next()
	if !errors.Is(err, errSSEDone) {
		t.Errorf("second Next: want errSSEDone, got %v", err)
	}
}

func TestSSE_DoneMarker_ErrorString(t *testing.T) {
	// 顺便校验 sentinel 的字符串表示
	got := errSSEDone.Error()
	if !strings.Contains(got, "[DONE]") {
		t.Errorf("errSSEDone.Error = %q", got)
	}
}

func TestSSE_NonDataFieldsIgnored(t *testing.T) {
	// SSE 规范允许 event: id: retry: 字段，我们都忽略
	input := "event: message\nid: 123\nretry: 1000\ndata: real\n\n"
	r := newSSEReader(strings.NewReader(input))
	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != "real" {
		t.Errorf("payload = %q", p)
	}
}

func TestSSE_DataWithoutSpace(t *testing.T) {
	// "data:xxx" 没有空格也合法
	input := "data:hello\n\n"
	r := newSSEReader(strings.NewReader(input))
	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != "hello" {
		t.Errorf("payload = %q", p)
	}
}

func TestSSE_EmptyReader(t *testing.T) {
	r := newSSEReader(strings.NewReader(""))
	_, err := r.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("empty reader: want EOF, got %v", err)
	}
}
