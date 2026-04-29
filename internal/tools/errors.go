package tools

import "errors"

// ─── Argument / validation errors ───────────────────────────────────────────

var (
	// ErrInvalidArgs JSON 解析失败 / 字段缺失 / 字段值无意义
	ErrInvalidArgs = errors.New("tools: invalid arguments")

	// ErrPathOutOfSandbox 路径不在 AllowedDirs 任一沙箱内
	ErrPathOutOfSandbox = errors.New("tools: path out of sandbox")

	// ErrFileTooLarge 目标文件超 MaxFileSize
	ErrFileTooLarge = errors.New("tools: file too large")

	// ErrContentTooLarge 写入内容超 MaxWriteSize
	ErrContentTooLarge = errors.New("tools: content too large")

	// ErrParentDirMissing write_file 目标父目录不存在
	ErrParentDirMissing = errors.New("tools: parent directory missing")

	// ErrFileExists write_file overwrite=false 且文件已存在
	ErrFileExists = errors.New("tools: file already exists")

	// ErrOldStringNotFound replace 的 old_string 在文件中未找到
	ErrOldStringNotFound = errors.New("tools: old_string not found in file")

	// ErrOldStringAmbiguous replace（非 replace_all）的 old_string 匹配多处
	ErrOldStringAmbiguous = errors.New("tools: old_string matches multiple locations")

	// ErrNoopReplace replace 的 old_string == new_string（无意义替换）
	ErrNoopReplace = errors.New("tools: old_string equals new_string (noop)")

	// ErrTooManyEdits multi_replace 的 edits 超上限
	ErrTooManyEdits = errors.New("tools: too many edits")

	// ErrTooManyFiles multi_write 的 files 超上限
	ErrTooManyFiles = errors.New("tools: too many files")

	// ErrTotalBytesTooLarge multi_write 的 Σ Content 超上限
	ErrTotalBytesTooLarge = errors.New("tools: total bytes too large")

	// ErrEmptyInput edits/files 为空
	ErrEmptyInput = errors.New("tools: empty input")

	// ErrBinaryContent 读到二进制文件（头部含 NUL 字节）
	ErrBinaryContent = errors.New("tools: binary content")

	// ErrHostNotAllowed http_fetch 的 URL host 不在白名单
	ErrHostNotAllowed = errors.New("tools: host not allowed")

	// ErrPrivateAddress http_fetch 的 URL 解析为私有 / 环回 / 链路本地
	ErrPrivateAddress = errors.New("tools: private address blocked")

	// ErrSchemeNotAllowed http_fetch 的 scheme 不是 http/https
	ErrSchemeNotAllowed = errors.New("tools: scheme not allowed")

	// ErrCommandNotAllowed shell_exec 的 command 未匹配白名单（已废弃，保留兼容）
	ErrCommandNotAllowed = errors.New("tools: command not allowed")

	// ErrCommandBlocked shell_exec 的 command 命中黑名单
	ErrCommandBlocked = errors.New("tools: command blocked by security policy")
)
