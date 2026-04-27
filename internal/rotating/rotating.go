// Package rotating 提供按大小轮转的文件写入器
//
// 当文件超过 maxSize 时，执行重命名链轮转：
//
//	{base}.jsonl → {base}.1.jsonl, {base}.1.jsonl → {base}.2.jsonl, ...
//
// 当轮转文件数超过 maxFiles 时，删除最老的文件。
// 被 logger 和 timeline 包共同使用。
package rotating

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
)

// ─── Writer ──────────────────────────────────────────────────────────────────

// Writer 是按大小轮转的文件写入器
type Writer struct {
	mu       sync.Mutex
	dir      string
	baseName string // e.g. "timeline"（不含扩展名）
	current  *os.File
	curSize  int64
	maxSize  int64 // 单文件最大字节数，0=不限
	maxFiles int   // 保留的轮转文件数（不含活跃文件），0=不限
}

// Open 创建或追加打开轮转文件
//
// dir 为文件所在目录（自动创建），baseName 为文件名前缀（如 "timeline"）。
// maxSize 为单文件最大字节数（0=不限），maxFiles 为保留的轮转文件数（0=不限）。
// 文件命名规则：活跃文件 {baseName}.jsonl，轮转文件 {baseName}.jsonl.1, {baseName}.jsonl.2, ...
func Open(dir, baseName string, maxSize int64, maxFiles int) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create dir %s: %w", dir, err)
	}

	w := &Writer{
		dir:      dir,
		baseName: baseName,
		maxSize:  maxSize,
		maxFiles: maxFiles,
	}

	if err := w.open(); err != nil {
		return nil, err
	}

	return w, nil
}

// Write 写入数据（自动追加 \n），超限时触发轮转
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.rotateIfNeeded(); err != nil {
		return 0, err
	}

	n, err := w.current.Write(p)
	if err != nil {
		return n, err
	}
	// 追加换行
	if len(p) == 0 || p[len(p)-1] != '\n' {
		if _, err := w.current.Write([]byte("\n")); err != nil {
			return n, err
		}
		w.curSize += int64(n) + 1
	} else {
		w.curSize += int64(n)
	}

	return n, nil
}

// Close 关闭当前文件
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != nil {
		return w.current.Close()
	}
	return nil
}

// CurrentPath 返回当前活跃文件路径
func (w *Writer) CurrentPath() string {
	return w.activePath()
}

// SetMaxSize 动态设置单文件最大字节数
//
// 主要用于测试。生产代码应在 Open 时指定。
func (w *Writer) SetMaxSize(maxSize int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.maxSize = maxSize
}

// ─── 内部方法 ────────────────────────────────────────────────────────────────

// open 打开或追加到当前活跃文件
func (w *Writer) open() error {
	path := w.activePath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open file %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}

	if w.current != nil {
		_ = w.current.Close()
	}
	w.current = f
	w.curSize = info.Size()
	return nil
}

// rotateIfNeeded 检查是否需要轮转
func (w *Writer) rotateIfNeeded() error {
	if w.maxSize > 0 && w.curSize >= w.maxSize {
		return w.rollSize()
	}
	return nil
}

// rollSize 按大小滚动：将当前文件依次重命名，然后打开新文件
func (w *Writer) rollSize() error {
	if w.current != nil {
		if err := w.current.Close(); err != nil {
			return fmt.Errorf("close current file: %w", err)
		}
		w.current = nil
	}

	base := w.basePath()

	// 找最大编号
	maxN := 0
	for {
		p := fmt.Sprintf("%s.%d", base, maxN+1)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			break
		}
		maxN++
		if maxN > 999 {
			break
		}
	}

	// 从高到低依次重命名
	for i := maxN; i >= 1; i-- {
		if err := os.Rename(fmt.Sprintf("%s.%d", base, i), fmt.Sprintf("%s.%d", base, i+1)); err != nil {
			return fmt.Errorf("rotate rename %s.%d: %w", base, i, err)
		}
	}
	if err := os.Rename(base, base+".1"); err != nil {
		return fmt.Errorf("rotate rename %s: %w", base, err)
	}

	// 删除超出 maxFiles 的旧文件
	w.trimFiles()

	return w.open()
}

// trimFiles 删除超过 maxFiles 的轮转文件
func (w *Writer) trimFiles() {
	if w.maxFiles <= 0 {
		return
	}
	base := w.basePath()
	for n := w.maxFiles + 1; n <= 999; n++ {
		p := fmt.Sprintf("%s.%d", base, n)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			break
		}
		_ = os.Remove(p)
	}
}

// activePath 返回当前活跃文件路径
func (w *Writer) activePath() string {
	return filepath.Join(w.dir, w.baseName+".jsonl")
}

// basePath 返回不带编号的基础路径（用于轮转重命名）
func (w *Writer) basePath() string {
	return filepath.Join(w.dir, w.baseName+".jsonl")
}

// ─── 文件列表 ────────────────────────────────────────────────────────────────

// ListFiles 返回所有轮转文件路径（从最老到最新）
//
// 扫描 dir 下匹配 {baseName}.jsonl、{baseName}.jsonl.1、... 的文件，
// 按编号从小到大排列（最老 → 最新）。
func ListFiles(dir, baseName string) ([]string, error) {
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(baseName) + `\.jsonl(?:\.(\d+))?$`)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type fileEntry struct {
		path string
		num  int // 0 = 活跃文件（无编号），1/2/... = 轮转文件
	}
	var files []fileEntry

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := pattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		num := 0
		if m[1] != "" {
			num, _ = strconv.Atoi(m[1])
		}
		files = append(files, fileEntry{
			path: filepath.Join(dir, e.Name()),
			num:  num,
		})
	}

	// 按编号排序：编号小的更老（先写入的）
	sort.Slice(files, func(i, j int) bool {
		return files[i].num < files[j].num
	})

	// 对于轮转文件，编号大的反而更老（.5 比 .1 更早写入），
	// 但 .0（活跃文件）是最新的。需要重新考虑排序：
	// 实际时间顺序：.5（最老）→ .4 → .3 → .2 → .1 → .0（最新）
	// 所以编号大的先读取
	sort.Slice(files, func(i, j int) bool {
		// 编号 0（活跃）排在最后
		if files[i].num == 0 {
			return false
		}
		if files[j].num == 0 {
			return true
		}
		// 编号大的更老，排前面
		return files[i].num > files[j].num
	})

	result := make([]string, len(files))
	for i, f := range files {
		result[i] = f.path
	}
	return result, nil
}
