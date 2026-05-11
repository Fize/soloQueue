package tools

import "errors"

// ─── Argument / validation errors ───────────────────────────────────────────

var (
	// ErrInvalidArgs JSON 解析失败 / 字段缺失 / 字段值无意义
	ErrInvalidArgs = errors.New("tools: invalid arguments")

	// ErrFileTooLarge 目标文件超 MaxFileSize
	ErrFileTooLarge = errors.New("tools: file too large")

	// ErrContentTooLarge 写入内容超 MaxWriteSize
	ErrContentTooLarge = errors.New("tools: content too large")

	// ErrParentDirMissing Write 目标父目录不存在
	ErrParentDirMissing = errors.New("tools: parent directory missing")

	// ErrFileExists Write overwrite=false 且文件已存在
	ErrFileExists = errors.New("tools: file already exists")

	// ErrOldStringNotFound Edit 的 old_string 在文件中未找到
	ErrOldStringNotFound = errors.New("tools: old_string not found in file")

	// ErrOldStringAmbiguous Edit（非 replace_all）的 old_string 匹配多处
	ErrOldStringAmbiguous = errors.New("tools: old_string matches multiple locations")

	// ErrNoopReplace Edit 的 old_string == new_string（无意义替换）
	ErrNoopReplace = errors.New("tools: old_string equals new_string (noop)")

	// ErrTooManyEdits MultiEdit 的 edits 超上限
	ErrTooManyEdits = errors.New("tools: too many edits")

	// ErrTooManyFiles MultiWrite 的 files 超上限
	ErrTooManyFiles = errors.New("tools: too many files")

	// ErrTotalBytesTooLarge MultiWrite 的 Σ Content 超上限
	ErrTotalBytesTooLarge = errors.New("tools: total bytes too large")

	// ErrEmptyInput edits/files 为空
	ErrEmptyInput = errors.New("tools: empty input")

	// ErrBinaryContent 读到二进制文件（头部含 NUL 字节）
	ErrBinaryContent = errors.New("tools: binary content")

	// ErrHostNotAllowed WebFetch 的 URL host 不在白名单
	ErrHostNotAllowed = errors.New("tools: host not allowed")

	// ErrPrivateAddress WebFetch 的 URL 解析为私有 / 环回 / 链路本地
	ErrPrivateAddress = errors.New("tools: private address blocked")

	// ErrSchemeNotAllowed WebFetch 的 scheme 不是 http/https
	ErrSchemeNotAllowed = errors.New("tools: scheme not allowed")

	// ErrCommandNotAllowed Bash 的 command 未匹配白名单（已废弃，保留兼容）
	ErrCommandNotAllowed = errors.New("tools: command not allowed")

	// ErrCommandBlocked Bash 的 command 命中黑名单
	ErrCommandBlocked = errors.New("tools: command blocked by security policy")

	// ─── ImageGen errors ───────────────────────────────────────────────

	// ErrImageGenNoDefaultModel 没有 enabled + isDefault 的图片模型
	ErrImageGenNoDefaultModel = errors.New("tools: no default image model enabled")

	// ErrImageGenAuth 图片模型凭证未设置
	ErrImageGenAuth = errors.New("tools: image model credentials not set")

	// ErrImageGenTimeout 图片生成超时
	ErrImageGenTimeout = errors.New("tools: image generation timed out after 5 minutes")

	// ErrImageGenFailed 图片生成失败
	ErrImageGenFailed = errors.New("tools: image generation failed")
)
