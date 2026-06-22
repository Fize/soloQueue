package tools

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"unicode/utf8"
)

// ─── Binary detection ───────────────────────────────────────────────────

// looksBinary checks whether the first N bytes contain a NUL byte; returning true suggests the content is likely binary.
//
// This is a simple heuristic similar to git/grep: UTF-8 text does not contain U+0000, so a NUL byte in the first 512 bytes is a strong signal.
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

// ctxErrOrNil is a convenience helper that returns ctx.Err() when the context is canceled, or nil otherwise.
//
// It is used inside tool loops (such as grep walks or glob iteration) to check every N items.
func ctxErrOrNil(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// validateNotZeroLen ensures s is non-empty and returns ErrInvalidArgs otherwise.
func validateNotZeroLen(field, s string) error {
	if s == "" {
		return fmt.Errorf("%w: %s is empty", ErrInvalidArgs, field)
	}
	return nil
}

// validateOldString validates that old_string is non-empty and differs from new_string.
// idx is optional — pass -1 for single-edit tools (Edit), or the edit index for MultiEdit.
func validateOldString(oldStr, newStr string, idx int) error {
	if oldStr == "" {
		if idx >= 0 {
			return fmt.Errorf("%w: edits[%d].old_string is empty", ErrInvalidArgs, idx)
		}
		return fmt.Errorf("%w: old_string is empty", ErrInvalidArgs)
	}
	if oldStr == newStr {
		if idx >= 0 {
			return fmt.Errorf("%w: edits[%d]", ErrNoopReplace, idx)
		}
		return ErrNoopReplace
	}
	return nil
}

// ─── String truncation ──────────────────────────────────────────────────

// truncateString truncates a string for display purposes, appending an ellipsis and staying rune-safe.
func truncateString(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// ─── Shell helpers ──────────────────────────────────────────────────────

// matchesAny checks whether s matches any of the provided regexes.
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
