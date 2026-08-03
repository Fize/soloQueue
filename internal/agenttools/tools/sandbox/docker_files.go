package sandbox

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
)

func (d *DockerRunner) containerPath(path string) string {
	return filepath.ToSlash(d.pathMap.ToContainerPath(path))
}

func (d *DockerRunner) containerIDFor(ctx context.Context) (string, error) {
	if err := d.Start(ctx); err != nil {
		return "", fmt.Errorf("sandbox: ensure container started: %w", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.containerID == "" {
		return "", fmt.Errorf("sandbox: container is not ready")
	}
	return d.containerID, nil
}

func (d *DockerRunner) Stat(ctx context.Context, path string) (FileInfo, error) {
	cid, err := d.containerIDFor(ctx)
	if err != nil {
		return FileInfo{}, err
	}
	stat, err := d.cli.ContainerStatPath(ctx, cid, d.containerPath(path))
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{Size: stat.Size, IsDir: stat.Mode.IsDir()}, nil
}

func (d *DockerRunner) ReadFile(ctx context.Context, path string, opts ReadFileOptions) ([]byte, error) {
	cid, err := d.containerIDFor(ctx)
	if err != nil {
		return nil, err
	}
	containerPath := d.containerPath(path)
	reader, stat, err := d.cli.CopyFromContainer(ctx, cid, containerPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if stat.Mode.IsDir() {
		return nil, fmt.Errorf("sandbox: path is a directory: %s", path)
	}
	if opts.MaxSize > 0 && stat.Size > opts.MaxSize {
		return nil, fmt.Errorf("file too large: %s (%d bytes > %d)", path, stat.Size, opts.MaxSize)
	}

	tr := tar.NewReader(reader)
	for {
		header, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("sandbox: read archive: %w", nextErr)
		}
		if !header.FileInfo().Mode().IsRegular() {
			continue
		}
		limit := header.Size
		if opts.MaxSize > 0 && limit > opts.MaxSize {
			return nil, fmt.Errorf("file too large: %s (%d bytes > %d)", path, limit, opts.MaxSize)
		}
		data, readErr := io.ReadAll(io.LimitReader(tr, limit+1))
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("sandbox: file changed while reading: %s", path)
		}
		return data, nil
	}
	return nil, fmt.Errorf("sandbox: regular file not found in archive: %s", path)
}

func (d *DockerRunner) WriteFile(ctx context.Context, path string, data []byte, opts WriteFileOptions) (WriteFileResult, error) {
	if opts.MaxSize > 0 && int64(len(data)) > opts.MaxSize {
		return WriteFileResult{}, fmt.Errorf("write too large: %d bytes > %d", len(data), opts.MaxSize)
	}
	cid, err := d.containerIDFor(ctx)
	if err != nil {
		return WriteFileResult{}, err
	}
	containerPath := d.containerPath(path)
	parent := filepath.ToSlash(filepath.Dir(containerPath))
	parentStat, err := d.cli.ContainerStatPath(ctx, cid, parent)
	if err != nil {
		return WriteFileResult{}, fmt.Errorf("parent dir missing: %s: %w", filepath.Dir(path), err)
	}
	if !parentStat.Mode.IsDir() {
		return WriteFileResult{}, fmt.Errorf("parent is not a directory: %s", filepath.Dir(path))
	}

	_, statErr := d.cli.ContainerStatPath(ctx, cid, containerPath)
	existed := statErr == nil
	if statErr != nil && !errdefs.IsNotFound(statErr) {
		return WriteFileResult{}, statErr
	}
	if existed && !opts.Overwrite {
		return WriteFileResult{}, fmt.Errorf("file already exists: %s", path)
	}

	tmpBase, err := randomTempName()
	if err != nil {
		return WriteFileResult{}, err
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	uid, gid := sandboxContainerIDs()
	if err := tw.WriteHeader(&tar.Header{
		Name: tmpBase,
		Mode: 0o600,
		Size: int64(len(data)),
		Uid:  uid,
		Gid:  gid,
	}); err != nil {
		return WriteFileResult{}, err
	}
	if _, err := tw.Write(data); err != nil {
		return WriteFileResult{}, err
	}
	if err := tw.Close(); err != nil {
		return WriteFileResult{}, err
	}
	if err := d.cli.CopyToContainer(ctx, cid, parent, &archive, container.CopyToContainerOptions{
		CopyUIDGID: true,
	}); err != nil {
		return WriteFileResult{}, fmt.Errorf("sandbox: copy temporary file: %w", err)
	}

	tmpPath := filepath.ToSlash(filepath.Join(parent, tmpBase))
	res, moveErr := d.runArgv(ctx, []string{"/bin/mv", "-f", "--", tmpPath, containerPath}, nil, RunCommandOptions{
		MaxOutput: 16 << 10,
	})
	if moveErr != nil || res.ExitCode != 0 {
		_, _ = d.runArgv(context.Background(), []string{"/bin/rm", "-f", "--", tmpPath}, nil, RunCommandOptions{MaxOutput: 4 << 10})
		if moveErr != nil {
			return WriteFileResult{}, moveErr
		}
		return WriteFileResult{}, fmt.Errorf("sandbox: atomic rename failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return WriteFileResult{Created: !existed}, nil
}

func (d *DockerRunner) MkdirAll(ctx context.Context, path string) error {
	res, err := d.runArgv(ctx, []string{"/bin/mkdir", "-p", "--", d.containerPath(path)}, nil, RunCommandOptions{
		MaxOutput: 16 << 10,
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("sandbox: mkdir failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func randomTempName() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return ".soloqueue-tmp-" + hex.EncodeToString(b[:]), nil
}

func (d *DockerRunner) Glob(ctx context.Context, dir, pattern string, opts GlobOptions) ([]string, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = 10_000
	}
	files, err := d.listRegularFiles(ctx, dir, 32<<20)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, min(maxItems, len(files)))
	for _, file := range files {
		rel, relErr := filepath.Rel(dir, file)
		if relErr != nil {
			continue
		}
		matched, matchErr := doublestar.PathMatch(pattern, filepath.ToSlash(rel))
		if matchErr != nil {
			return nil, matchErr
		}
		if matched {
			result = append(result, file)
			if len(result) >= maxItems {
				break
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

func (d *DockerRunner) listRegularFiles(ctx context.Context, dir string, maxOutput int64) ([]string, error) {
	mappedDir := d.containerPath(dir)
	res, err := d.runArgv(ctx, []string{"/usr/bin/find", mappedDir, "-type", "f", "-print0"}, nil, RunCommandOptions{
		MaxOutput: maxOutput,
	})
	if err != nil {
		return nil, err
	}
	if res.Truncated {
		return nil, fmt.Errorf("sandbox: file enumeration exceeded %d bytes", maxOutput)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("sandbox: find failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	raw := bytes.Split(res.Stdout, []byte{0})
	files := make([]string, 0, len(raw))
	for _, item := range raw {
		if len(item) == 0 {
			continue
		}
		files = append(files, d.pathMap.ToHostPath(string(item)))
	}
	return files, nil
}

func (d *DockerRunner) Grep(ctx context.Context, dir, pattern string, opts GrepOptions) ([]GrepMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	maxMatches := opts.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 1000
	}
	files, err := d.listRegularFiles(ctx, dir, 32<<20)
	if err != nil {
		return nil, err
	}
	var result []GrepMatch
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if opts.GlobPattern != "" {
			rel, relErr := filepath.Rel(dir, file)
			if relErr != nil {
				continue
			}
			ok, _ := doublestar.PathMatch(opts.GlobPattern, filepath.ToSlash(rel))
			if !ok {
				continue
			}
		}
		data, readErr := d.ReadFile(ctx, file, ReadFileOptions{MaxSize: 64 << 20})
		if readErr != nil || looksBinaryFile(data) {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		line := 0
		for scanner.Scan() {
			line++
			text := scanner.Text()
			if !re.MatchString(text) {
				continue
			}
			if opts.MaxLineLen > 0 && len(text) > opts.MaxLineLen {
				text = text[:opts.MaxLineLen] + "…"
			}
			result = append(result, GrepMatch{File: file, Line: line, Content: text})
			if len(result) >= maxMatches {
				return result, nil
			}
		}
	}
	return result, nil
}

func looksBinaryFile(data []byte) bool {
	if len(data) > 512 {
		data = data[:512]
	}
	return bytes.IndexByte(data, 0) >= 0
}

func (d *DockerRunner) DoHTTP(ctx context.Context, req HTTPRequest) (HTTPResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	maxBody := req.MaxBody
	if maxBody <= 0 {
		maxBody = 5 << 20
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	seconds := int(timeout.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	args := []string{
		"--silent", "--show-error",
		"--max-time", fmt.Sprintf("%d", seconds),
		"--max-redirs", "0",
		"--request", method,
		"--write-out", "\n__SOLOQUEUE_STATUS__:%{http_code}",
	}
	keys := make([]string, 0, len(req.Headers))
	for key := range req.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--header", key+": "+req.Headers[key])
	}
	if req.ContentType != "" {
		args = append(args, "--header", "Content-Type: "+req.ContentType)
	}
	if len(req.Body) > 0 {
		args = append(args, "--data-binary", "@-")
	}
	args = append(args, "--", req.URL)
	res, err := d.runArgv(ctx, append([]string{"/usr/bin/curl"}, args...), nil, RunCommandOptions{
		Stdin:     string(req.Body),
		MaxOutput: maxBody + 256,
	})
	if err != nil {
		return HTTPResponse{}, err
	}
	if res.Truncated {
		return HTTPResponse{}, fmt.Errorf("sandbox: HTTP response exceeded %d bytes", maxBody)
	}
	if res.ExitCode != 0 {
		return HTTPResponse{}, fmt.Errorf("sandbox: curl failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	marker := []byte("\n__SOLOQUEUE_STATUS__:")
	index := bytes.LastIndex(res.Stdout, marker)
	if index < 0 {
		return HTTPResponse{}, fmt.Errorf("sandbox: HTTP status marker missing")
	}
	var status int
	if _, err := fmt.Sscanf(string(res.Stdout[index+len(marker):]), "%d", &status); err != nil {
		return HTTPResponse{}, fmt.Errorf("sandbox: invalid HTTP status: %w", err)
	}
	body := append([]byte(nil), res.Stdout[:index]...)
	return HTTPResponse{StatusCode: status, Body: body}, nil
}
