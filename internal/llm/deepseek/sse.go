package deepseek

import (
	"bufio"
	"io"
	"strings"
)

// sseDoneMarker is the termination sentinel used by DeepSeek / OpenAI-compat
const sseDoneMarker = "[DONE]"

// sseReader reads an SSE stream line by line, returning the payload string of data lines.
//
// Usage:
//
//	r := newSSEReader(httpResp.Body)
//	for {
//	    payload, err := r.Next()
//	    if err == io.EOF { break }            // stream ended normally
//	    if err == errSSEDone { break }         // [DONE] marker
//	    if err != nil { ... }                  // parse / IO error
//	    // payload is the JSON string after "data: "
//	}
type sseReader struct {
	sc *bufio.Scanner
}

func newSSEReader(r io.Reader) *sseReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &sseReader{sc: sc}
}

// Sentinel error: [DONE] marker reached.
// Uses a distinct error so the caller can differentiate it from io.EOF (EOF means the body was closed but [DONE] was not seen).
var errSSEDone = &sseDoneErr{}

type sseDoneErr struct{}

func (*sseDoneErr) Error() string { return "sse: [DONE]" }

// Next returns the payload of the next data line.
//
// Behavior:
//   - Skips empty lines and keep-alive / comment lines starting with `:`
//   - "data: XXX" → returns "XXX"
//   - "data: [DONE]" → returns errSSEDone
//   - Other "event:", "id:", "retry:" fields are ignored (DeepSeek does not use them)
//   - Underlying scanner ends (EOF / connection closed) → returns io.EOF
func (r *sseReader) Next() (string, error) {
	for r.sc.Scan() {
		line := r.sc.Text()

		// Skip empty lines (SSE event delimiters)
		if line == "" {
			continue
		}
		// Skip comments / keep-alive
		if strings.HasPrefix(line, ":") {
			continue
		}
		// Only process "data:" fields; other fields are not used for now
		const prefix = "data:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		payload := strings.TrimPrefix(line, prefix)
		// SSE specification allows "data:xxx" or "data: xxx" (the latter is more common), consistently remove leading space
		payload = strings.TrimLeft(payload, " ")

		if payload == sseDoneMarker {
			return "", errSSEDone
		}
		return payload, nil
	}
	if err := r.sc.Err(); err != nil {
		return "", err
	}
	return "", io.EOF
}