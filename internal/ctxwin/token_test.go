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
	// "hello world" 应该返回合理的 token 数（英文通常 1 word ≈ 1.3 tokens）
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
	// 中文字符通常每个 1-2 tokens
	got := tok.Count("你好世界")
	if got <= 0 {
		t.Errorf("Count(\"你好世界\") = %d, want > 0", got)
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
	// 生成一段长文本
	longText := strings.Repeat("This is a test sentence. ", 100)
	got := tok.Count(longText)
	if got <= 0 {
		t.Errorf("Count(longText) = %d, want > 0", got)
	}
	// 粗略验证：100 句话应该远超 100 tokens
	if got < 100 {
		t.Errorf("Count(longText) = %d, want >= 100", got)
	}
}

func TestMultipleTokenizersShareEncoding(t *testing.T) {
	t1 := NewTokenizer()
	t2 := NewTokenizer()
	// 两个 Tokenizer 应该共享底层编码
	if t1.enc != t2.enc {
		t.Error("Two Tokenizer instances should share the same encoding")
	}
}
