package tools

import "errors"

// ─── Argument / validation errors ───────────────────────────────────────────

var (
	// ErrInvalidArgs indicates JSON parsing failure, missing fields, or meaningless field values.
	ErrInvalidArgs = errors.New("tools: invalid arguments")

	// ErrFileTooLarge indicates the target file exceeds MaxFileSize.
	ErrFileTooLarge = errors.New("tools: file too large")

	// ErrContentTooLarge indicates the written content exceeds MaxWriteSize.
	ErrContentTooLarge = errors.New("tools: content too large")

	// ErrParentDirMissing indicates the parent directory for Write does not exist.
	ErrParentDirMissing = errors.New("tools: parent directory missing")

	// ErrFileExists indicates Write was called with overwrite=false and the file already exists.
	ErrFileExists = errors.New("tools: file already exists")

	// ErrOldStringNotFound indicates the Edit old_string was not found in the file.
	ErrOldStringNotFound = errors.New("tools: old_string not found in file")

	// ErrOldStringAmbiguous indicates the Edit old_string matched multiple locations when replace_all was false.
	ErrOldStringAmbiguous = errors.New("tools: old_string matches multiple locations")

	// ErrNoopReplace indicates the Edit old_string equals the new_string (a no-op replacement).
	ErrNoopReplace = errors.New("tools: old_string equals new_string (noop)")

	// ErrTooManyEdits indicates MultiEdit received more edits than allowed.
	ErrTooManyEdits = errors.New("tools: too many edits")

	// ErrTooManyFiles indicates MultiWrite received more files than allowed.
	ErrTooManyFiles = errors.New("tools: too many files")

	// ErrTotalBytesTooLarge indicates the total size of MultiWrite content exceeded the limit.
	ErrTotalBytesTooLarge = errors.New("tools: total bytes too large")

	// ErrEmptyInput indicates the edits/files input is empty.
	ErrEmptyInput = errors.New("tools: empty input")

	// ErrBinaryContent indicates the input contains binary content (NUL byte in the header).
	ErrBinaryContent = errors.New("tools: binary content")

	// ErrHostNotAllowed indicates the WebFetch URL host is not on the allowlist.
	ErrHostNotAllowed = errors.New("tools: host not allowed")

	// ErrPrivateAddress indicates the WebFetch URL resolved to a private, loopback, or link-local address.
	ErrPrivateAddress = errors.New("tools: private address blocked")

	// ErrSchemeNotAllowed indicates the WebFetch URL scheme is not http/https.
	ErrSchemeNotAllowed = errors.New("tools: scheme not allowed")

	// ErrCommandNotAllowed indicates the Bash command did not match the allowlist (deprecated, kept for compatibility).
	ErrCommandNotAllowed = errors.New("tools: command not allowed")

	// ErrCommandBlocked indicates the Bash command hit the blocklist.
	ErrCommandBlocked = errors.New("tools: command blocked by security policy")

	// ─── ImageGen errors ───────────────────────────────────────────────

	// ErrImageGenNoDefaultModel indicates there is no enabled image model marked as the default.
	ErrImageGenNoDefaultModel = errors.New("tools: no default image model enabled")

	// ErrImageGenAuth indicates image model credentials are not configured.
	ErrImageGenAuth = errors.New("tools: image model credentials not set")

	// ErrImageGenTimeout indicates image generation timed out.
	ErrImageGenTimeout = errors.New("tools: image generation timed out after 5 minutes")

	// ErrImageGenFailed indicates image generation failed.
	ErrImageGenFailed = errors.New("tools: image generation failed")

	// ErrImageEditFailed indicates image editing/image generation failed.
	ErrImageEditFailed = errors.New("tools: image edit failed")
)
