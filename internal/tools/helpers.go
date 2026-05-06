package tools

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"unicode/utf8"
)

// ─── Binary detection ───────────────────────────────────────────────────

// looksBinary 检查前 N 字节内是否含 NUL；返回 true 说明很可能是二进制
//
// 简单启发式（同 git / grep 的近似逻辑）：UTF-8 文本不含 U+0000，
// 因此首 512 字节里的 NUL 是强信号。
func looksBinary(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// ─── Context helper ──────────────────────────────────────────────────────

// ctxErrOrNil 便利函数：ctx 已取消时返回 ctx.Err()，否则 nil
//
// 用在工具循环里（grep walk、glob 迭代）每 N 项做一次检查。
func ctxErrOrNil(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// validateNotZeroLen 验证 s 至少非空（统一报 ErrInvalidArgs）
func validateNotZeroLen(field, s string) error {
	if s == "" {
		return fmt.Errorf("%w: %s is empty", ErrInvalidArgs, field)
	}
	return nil
}

// ─── String truncation ──────────────────────────────────────────────────

// truncateString 截断字符串用于展示（含省略号，rune-safe）
func truncateString(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// ─── Shell helpers ──────────────────────────────────────────────────────

// matchesAny 检查 s 是否命中 regexes 中任一正则
func matchesAny(s string, regexes []*regexp.Regexp) bool {
	for _, re := range regexes {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// ─── Network helpers ────────────────────────────────────────────────────

// isPrivateIP reports whether ip is in a non-routable range we want to block.
//
// Covered ranges (IPv4 & IPv6):
//   - 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16  (RFC 1918)
//   - 127.0.0.0/8, ::1                            (loopback)
//   - 169.254.0.0/16, fe80::/10                   (link-local)
//   - fc00::/7                                    (ULA)
//   - 0.0.0.0/8                                   (unspecified)
//   - 100.64.0.0/10                               (CGNAT)
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127: // 100.64.0.0/10 (CGNAT)
			return true
		case ip4[0] == 0:
			return true
		}
		return false
	}
	// IPv6: ULA fc00::/7 and similar (first octet 0xfc or 0xfd)
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}
