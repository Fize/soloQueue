package sandbox

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// DockerExecutor 基于 DockerSandbox.Exec 的沙盒执行器实现。
// 所有操作通过在容器内执行 shell 命令完成。
// 自动将宿主机路径转换为容器内路径，结果中的容器路径转换回宿主机路径。
type DockerExecutor struct {
	sb      *DockerSandbox
	pathMap *PathMap
	log     *logger.Logger
}

// NewDockerExecutor 创建基于 Docker 的执行器。
// 调用方需确保 sb 已 Start。
func NewDockerExecutor(sb *DockerSandbox) *DockerExecutor {
	return &DockerExecutor{sb: sb, pathMap: sb.pathMap}
}

// SetLogger 设置 logger，nil 表示不记录日志。
func (e *DockerExecutor) SetLogger(l *logger.Logger) {
	e.log = l
}

// toContainer 将宿主机路径转换为容器路径。
func (e *DockerExecutor) toContainer(hostPath string) string {
	return e.pathMap.ToContainerPath(hostPath)
}

// toHost 将容器路径转换为宿主机路径。
func (e *DockerExecutor) toHost(containerPath string) string {
	return e.pathMap.ToHostPath(containerPath)
}

// ─── RunCommand ─────────────────────────────────────────────────────────────

func (e *DockerExecutor) RunCommand(ctx context.Context, cmd string, opts RunCommandOptions) (RunCommandResult, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	stdout, stderr, err := e.sb.Exec(ctx, cmd)

	res := RunCommandResult{
		Stdout: stdout,
		Stderr: stderr,
	}

	if execErr, ok := err.(*ExecError); ok {
		res.ExitCode = execErr.ExitCode
		res.Stdout = execErr.Stdout
		res.Stderr = execErr.Stderr
		return res, nil
	}
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: run command failed", err, "command", cmd)
		}
		return res, err
	}

	return res, nil
}

// ─── ReadFile ───────────────────────────────────────────────────────────────

func (e *DockerExecutor) ReadFile(ctx context.Context, path string, opts ReadFileOptions) (ReadFileResult, error) {
	containerPath := e.toContainer(path)

	// 先检查文件大小
	if opts.MaxSize > 0 {
		fi, err := e.Stat(ctx, path)
		if err != nil {
			return ReadFileResult{}, err
		}
		if fi.Size > opts.MaxSize {
			return ReadFileResult{}, fmt.Errorf("file too large: %s (%d bytes > %d)", path, fi.Size, opts.MaxSize)
		}
	}

	// 用 base64 编码读取，避免二进制内容损坏
	cmd := fmt.Sprintf("base64 %s", shellQuote(containerPath))
	stdout, _, err := e.sb.Exec(ctx, cmd)
	if execErr, ok := err.(*ExecError); ok {
		if execErr.ExitCode != 0 {
			return ReadFileResult{}, fmt.Errorf("read file %s: %s", path, strings.TrimSpace(string(execErr.Stderr)))
		}
	}
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: read file failed", err, "path", path)
		}
		return ReadFileResult{}, err
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(stdout)))
	if err != nil {
		return ReadFileResult{}, fmt.Errorf("base64 decode %s: %w", path, err)
	}

	return ReadFileResult{Data: data}, nil
}

// ─── WriteFile ──────────────────────────────────────────────────────────────

func (e *DockerExecutor) WriteFile(ctx context.Context, path string, data []byte, opts WriteFileOptions) (WriteFileResult, error) {
	containerPath := e.toContainer(path)

	if opts.MaxSize > 0 && int64(len(data)) > opts.MaxSize {
		return WriteFileResult{}, fmt.Errorf("write too large: %d bytes > %d", len(data), opts.MaxSize)
	}

	// 检查文件是否已存在
	_, statErr := e.Stat(ctx, path)
	existed := statErr == nil
	created := !existed

	if existed && !opts.Overwrite {
		return WriteFileResult{}, fmt.Errorf("file already exists: %s", path)
	}

	// 检查父目录是否存在
	dirStat, err := e.Stat(ctx, parentDir(path))
	if err != nil || !dirStat.IsDir {
		return WriteFileResult{}, fmt.Errorf("parent dir missing: %s", parentDir(path))
	}

	// 通过 base64 编码写入，避免 shell 转义问题
	encoded := base64.StdEncoding.EncodeToString(data)
	cmd := fmt.Sprintf("echo %s | base64 -d > %s", shellQuote(encoded), shellQuote(containerPath))
	_, stderr, err := e.sb.Exec(ctx, cmd)
	if execErr, ok := err.(*ExecError); ok && execErr.ExitCode != 0 {
		return WriteFileResult{}, fmt.Errorf("write file %s: %s", path, strings.TrimSpace(string(execErr.Stderr)))
	}
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: write file failed", err, "path", path)
		}
		return WriteFileResult{}, err
	}
	_ = stderr

	return WriteFileResult{Created: created}, nil
}

// ─── Stat ───────────────────────────────────────────────────────────────────

func (e *DockerExecutor) Stat(ctx context.Context, path string) (FileInfo, error) {
	containerPath := e.toContainer(path)
	cmd := fmt.Sprintf("stat -c '%%s|%%F' %s 2>/dev/null || echo 'ERR'", shellQuote(containerPath))
	stdout, _, err := e.sb.Exec(ctx, cmd)
	if execErr, ok := err.(*ExecError); ok && execErr.ExitCode != 0 {
		return FileInfo{}, fmt.Errorf("stat %s: %s", path, strings.TrimSpace(string(execErr.Stderr)))
	}
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: stat failed", err, "path", path)
		}
		return FileInfo{}, err
	}

	output := strings.TrimSpace(string(stdout))
	if output == "ERR" || output == "" {
		return FileInfo{}, fmt.Errorf("stat %s: not found", path)
	}

	parts := strings.SplitN(output, "|", 2)
	if len(parts) != 2 {
		return FileInfo{}, fmt.Errorf("stat %s: unexpected output: %s", path, output)
	}

	size, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat %s: parse size: %w", path, err)
	}

	isDir := parts[1] == "directory"

	return FileInfo{Size: size, IsDir: isDir}, nil
}

// ─── Glob ───────────────────────────────────────────────────────────────────

func (e *DockerExecutor) Glob(ctx context.Context, dir string, pattern string, opts GlobOptions) ([]string, error) {
	containerDir := e.toContainer(dir)

	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = 10000
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Convert doublestar pattern to find-compatible expression.
	// find -name only matches basenames (no /), so doublestar patterns
	// containing / (e.g. **/*.go) would never match and traverse the
	// entire tree producing no results. Use a shell globstar shopt for
	// doublestar patterns and fall back to -name for simple filename
	// patterns.
	cmd := buildGlobCmd(containerDir, pattern, maxItems)
	stdout, _, err := e.sb.Exec(ctx, cmd)
	if execErr, ok := err.(*ExecError); execErr != nil && ok && execErr.ExitCode != 0 {
		return nil, fmt.Errorf("glob %s: %s", dir, strings.TrimSpace(string(execErr.Stderr)))
	}
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: glob failed", err, "dir", dir, "pattern", pattern)
		}
		return nil, err
	}

	output := strings.TrimSpace(string(stdout))
	if output == "" {
		return nil, nil
	}

	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, e.toHost(line))
		}
	}

	return result, nil
}

// buildGlobCmd constructs a shell command for file globbing inside a container.
//
// Doublestar patterns containing / (e.g. **/*.go or src/**/*.ts) are rewritten
// for find which does not support ** natively. We extract the filename component
// and restrict the search to literal directory prefixes, then rely on find's
// default recursive traversal for the ** semantics.
func buildGlobCmd(dir, pattern string, maxItems int) string {
	if !strings.Contains(pattern, "/") {
		// Simple filename pattern without / (e.g. *.go): use -maxdepth 1.
		return fmt.Sprintf("find %s -maxdepth 1 -type f -name %s 2>/dev/null | head -n %d",
			shellQuote(dir), shellQuote(pattern), maxItems)
	}

	// Path pattern: extract the filename (last segment) and build the search
	// prefix from literal directory segments before the first ** or glob.
	parts := strings.Split(pattern, "/")
	fileName := parts[len(parts)-1]

	// Collect literal directory segments before any ** or glob-containing segment.
	// find is invoked from the prefix; its default recursion handles the **.
	var prefix []string
	for _, p := range parts[:len(parts)-1] {
		if p == "**" || strings.ContainsAny(p, "*?[]") {
			break
		}
		prefix = append(prefix, p)
	}

	searchDir := filepath.Join(dir, filepath.Join(prefix...))
	return fmt.Sprintf("find %s -type f -name %s 2>/dev/null | head -n %d",
		shellQuote(searchDir), shellQuote(fileName), maxItems)
}

// ─── Grep ───────────────────────────────────────────────────────────────────

func (e *DockerExecutor) Grep(ctx context.Context, dir string, pattern string, opts GrepOptions) ([]GrepMatch, error) {
	containerDir := e.toContainer(dir)

	maxMatches := opts.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 1000
	}

	cmd := fmt.Sprintf("grep -rn -- %s %s 2>/dev/null | head -n %d", shellQuote(pattern), shellQuote(containerDir), maxMatches)
	stdout, _, err := e.sb.Exec(ctx, cmd)
	if execErr, ok := err.(*ExecError); ok {
		// grep 返回 1 表示无匹配，不是错误
		if execErr.ExitCode == 1 {
			return nil, nil
		}
		if execErr.ExitCode > 1 {
			return nil, fmt.Errorf("grep %s: %s", dir, strings.TrimSpace(string(execErr.Stderr)))
		}
	}
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: grep failed", err, "dir", dir, "pattern", pattern)
		}
		return nil, err
	}

	output := strings.TrimSpace(string(stdout))
	if output == "" {
		return nil, nil
	}

	lines := strings.Split(output, "\n")
	result := make([]GrepMatch, 0, len(lines))
	for _, line := range lines {
		// 格式: file:lineNum:content
		idx1 := strings.Index(line, ":")
		if idx1 < 0 {
			continue
		}
		idx2 := strings.Index(line[idx1+1:], ":")
		if idx2 < 0 {
			continue
		}
		idx2 += idx1 + 1

		file := line[:idx1]
		lineNumStr := line[idx1+1 : idx2]
		content := line[idx2+1:]

		lineNum, _ := strconv.Atoi(lineNumStr)

		if opts.MaxLineLen > 0 && len(content) > opts.MaxLineLen {
			content = content[:opts.MaxLineLen]
		}

		result = append(result, GrepMatch{
			File:    e.toHost(file),
			Line:    lineNum,
			Content: content,
		})
	}

	return result, nil
}

// ─── HTTPGet ────────────────────────────────────────────────────────────────

func (e *DockerExecutor) HTTPGet(ctx context.Context, url string, opts HTTPOptions) (HTTPResponse, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	maxBody := opts.MaxBody
	if maxBody <= 0 {
		maxBody = 5 << 20 // 默认 5MB
	}

	cmd := fmt.Sprintf("curl -sL -o - -w '\\n%%{http_code}' --max-time %d %s 2>/dev/null | head -c %d",
		int(opts.Timeout.Seconds()), shellQuote(url), maxBody+100)

	stdout, _, err := e.sb.Exec(ctx, cmd)
	if execErr, ok := err.(*ExecError); ok && execErr.ExitCode != 0 {
		return HTTPResponse{}, fmt.Errorf("http get %s: exit code %d", url, execErr.ExitCode)
	}
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: http get failed", err, "url", url)
		}
		return HTTPResponse{}, err
	}

	return parseCurlOutput(stdout)
}

// ─── HTTPPost ───────────────────────────────────────────────────────────────

func (e *DockerExecutor) HTTPPost(ctx context.Context, url string, body string, opts HTTPOptions) (HTTPResponse, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	maxBody := opts.MaxBody
	if maxBody <= 0 {
		maxBody = 5 << 20
	}

	// 通过 base64 传递 body 避免 shell 转义问题
	encoded := base64.StdEncoding.EncodeToString([]byte(body))

	args := fmt.Sprintf("-sL -X POST -o - -w '\\n%%{http_code}' --max-time %d", int(opts.Timeout.Seconds()))
	if opts.ContentType != "" {
		args += fmt.Sprintf(" -H %s", shellQuote("Content-Type: "+opts.ContentType))
	}
	for k, v := range opts.Headers {
		args += fmt.Sprintf(" -H %s", shellQuote(k+": "+v))
	}
	args += fmt.Sprintf(" --data-binary @<(%s | base64 -d)", shellQuote("echo "+encoded))

	cmd := fmt.Sprintf("curl %s %s 2>/dev/null | head -c %d", args, shellQuote(url), maxBody+100)

	stdout, _, err := e.sb.Exec(ctx, cmd)
	if execErr, ok := err.(*ExecError); ok && execErr.ExitCode != 0 {
		return HTTPResponse{}, fmt.Errorf("http post %s: exit code %d", url, execErr.ExitCode)
	}
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: http post failed", err, "url", url)
		}
		return HTTPResponse{}, err
	}

	return parseCurlOutput(stdout)
}

// ─── helpers ────────────────────────────────────────────────────────────────

// shellQuote 将字符串用单引号包裹，内部单引号转义为 '\”
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// parentDir 返回路径的父目录
func parentDir(path string) string {
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		return path[:idx]
	}
	return "/"
}

// parseCurlOutput 解析 curl 的 -w 输出格式（body + 最后一行状态码）
func parseCurlOutput(data []byte) (HTTPResponse, error) {
	// 最后一行是状态码
	lastNewline := strings.LastIndex(string(data), "\n")
	if lastNewline < 0 {
		return HTTPResponse{StatusCode: 0, Body: data}, nil
	}

	statusStr := strings.TrimSpace(string(data[lastNewline+1:]))
	statusCode, err := strconv.Atoi(statusStr)
	if err != nil {
		return HTTPResponse{StatusCode: 0, Body: data}, nil
	}

	body := data[:lastNewline]
	return HTTPResponse{
		StatusCode: statusCode,
		Body:       body,
	}, nil
}

// Compile-time check
var _ Executor = (*DockerExecutor)(nil)
