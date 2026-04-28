package tools

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf8"
)

// ─── Atomic write helper ─────────────────────────────────────────────────

// atomicWrite 把 data 原子写入 path
//
// 步骤：
//  1. os.CreateTemp(filepath.Dir(path), ".soloqueue-tmp-*") 在同一目录内起 tmp
//     （rename 必须同盘）
//  2. tmp.Write(data) → tmp.Sync() → tmp.Close()
//  3. 若 overwrite=false 且 path 已存在 → 删除 tmp 并返回 ErrFileExists
//     （竞态窗口：先 Stat 再 Rename；若两者之间别的进程创建了文件，Rename
//     仍会覆盖 —— 本实现用 Stat 做"常见情况"兜底，OS 级强制 O_EXCL 在不同平台
//     对 rename 语义不一致，此处保持简单）
//  4. os.Rename(tmp, path) 原子替换
//  5. 任一环节失败：删 tmp（best-effort）
//
// 返回的 created 表示"目标路径之前不存在"（用于 Tool 返回 payload）。
func atomicWrite(path string, data []byte, overwrite bool) (created bool, err error) {
	dir := filepath.Dir(path)
	// 父目录必须存在（不自动 MkdirAll）
	if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
		return false, fmt.Errorf("%w: %s", ErrParentDirMissing, dir)
	}

	// 检查目标是否已存在
	_, statErr := os.Stat(path)
	existed := statErr == nil
	created = !existed

	if existed && !overwrite {
		return false, fmt.Errorf("%w: %s", ErrFileExists, path)
	}

	// 用匿名函数 + defer 保证失败清理
	tmp, err := os.CreateTemp(dir, ".soloqueue-tmp-*")
	if err != nil {
		return false, fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()
	// 失败时清理 tmp；成功 rename 后 tmpName 已不存在，Remove 变成 no-op
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("write tmp: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("sync tmp: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return false, fmt.Errorf("close tmp: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return false, fmt.Errorf("rename tmp → target: %w", err)
	}
	return created, nil
}

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

// ─── Read file with size cap ────────────────────────────────────────────

// readFileCapped 读文件，若 > limit 返回 ErrFileTooLarge
//
// 先 Stat 拿大小，避免 OOM；大小 OK 后一次 ReadAll。limit<=0 表示不限。
func readFileCapped(path string, limit int64) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if limit > 0 && fi.Size() > limit {
		return nil, fmt.Errorf("%w: %s (%d bytes > %d)", ErrFileTooLarge, path, fi.Size(), limit)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
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

// limitedWriter wraps w and drops any bytes written past cap; sets truncated=true
// if dropping happened. Returns nil errors to avoid propagating short-write.
type limitedWriter struct {
	w         io.Writer
	cap       int64
	written   int64
	truncated bool
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.written >= lw.cap {
		lw.truncated = true
		return len(p), nil // pretend we wrote; drop silently
	}
	remain := lw.cap - lw.written
	if int64(len(p)) > remain {
		n, err := lw.w.Write(p[:remain])
		lw.written += int64(n)
		lw.truncated = true
		return len(p), err
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	return n, err
}

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
