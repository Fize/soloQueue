package ctxwin

import (
	"strings"
	"testing"
)

func TestNewTokenizer(t *testing.T) {
	tok := NewTokenizer()
	if tok == nil {
		t.Fatal("NewTokenizer returned nil")
	}
	if tok.enc == nil {
		t.Fatal("Tokenizer.enc is nil")
	}
}

func TestCountEmpty(t *testing.T) {
	tok := NewTokenizer()
	if got := tok.Count(""); got != 0 {
		t.Errorf("Count(\"\") = %d, want 0", got)
	}
}

func TestCountBasic(t *testing.T) {
	tok := NewTokenizer()
	// "hello world" should return a reasonable number of tokens (English usually 1 word ≈ 1.3 tokens)
	got := tok.Count("hello world")
	if got <= 0 {
		t.Errorf("Count(\"hello world\") = %d, want > 0", got)
	}
	if got > 10 {
		t.Errorf("Count(\"hello world\") = %d, want <= 10 (reasonable upper bound)", got)
	}
}

func TestCountChinese(t *testing.T) {
	tok := NewTokenizer()
	// Chinese characters usually take 1-2 tokens each
	got := tok.Count("hello world")
	if got <= 0 {
		t.Errorf("Count(\"hello world\") = %d, want > 0", got)
	}
}

func TestCountDeterministic(t *testing.T) {
	tok := NewTokenizer()
	text := "The quick brown fox jumps over the lazy dog."
	first := tok.Count(text)
	second := tok.Count(text)
	if first != second {
		t.Errorf("Count not deterministic: first=%d, second=%d", first, second)
	}
}

func TestCountLongText(t *testing.T) {
	tok := NewTokenizer()
	// Generate a long text string
	longText := strings.Repeat("This is a test sentence. ", 100)
	got := tok.Count(longText)
	if got <= 0 {
		t.Errorf("Count(longText) = %d, want > 0", got)
	}
	// Rough verification: 100 sentences should be well over 100 tokens
	if got < 100 {
		t.Errorf("Count(longText) = %d, want >= 100", got)
	}
}

func TestMultipleTokenizersShareEncoding(t *testing.T) {
	t1 := NewTokenizer()
	t2 := NewTokenizer()
	// Two Tokenizer instances should share the same underlying encoding
	if t1.enc != t2.enc {
		t.Error("Two Tokenizer instances should share the same encoding")
	}
}