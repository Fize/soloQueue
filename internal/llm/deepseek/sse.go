package deepseek

import (
	"bufio"
	"io"
	"strings"
)

// sseDoneMarker 是 DeepSeek / OpenAI-compat 使用的终止 sentinel
const sseDoneMarker = "[DONE]"

// sseReader 按行读 SSE 流，返回 data 行的 payload 字符串
//
// 用法：
//
//	r := newSSEReader(httpResp.Body)
//	for {
//	    payload, err := r.Next()
//	    if err == io.EOF { break }            // 流正常结束
//	    if err == errSSEDone { break }         // [DONE] marker
//	    if err != nil { ... }                  // parse / IO 错误
//	    // payload 是 "data: " 后面的 JSON 字符串
//	}
type sseReader struct {
	sc *bufio.Scanner
}

func newSSEReader(r io.Reader) *sseReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &sseReader{sc: sc}
}

// sentinel 错误：[DONE] marker 已到达。
// 使用独立 error 以便 caller 区分于 io.EOF（EOF 表示 body 被关闭但没看到 DONE）。
var errSSEDone = &sseDoneErr{}

type sseDoneErr struct{}

func (*sseDoneErr) Error() string { return "sse: [DONE]" }

// Next 返回下一个 data 行的 payload
//
// 行为：
//   - 跳过空行和 "`:` 开头" 的 keep-alive / comment 行
//   - "data: XXX" → 返回 "XXX"
//   - "data: [DONE]" → 返回 errSSEDone
//   - 其他 "event:" / "id:" / "retry:" 字段被忽略（DeepSeek 不使用）
//   - 底层 scanner 结束（EOF / 连接关闭）→ 返回 io.EOF
func (r *sseReader) Next() (string, error) {
	for r.sc.Scan() {
		line := r.sc.Text()

		// 跳过空行（SSE event 分隔符）
		if line == "" {
			continue
		}
		// 跳过 comment / keep-alive
		if strings.HasPrefix(line, ":") {
			continue
		}
		// 只处理 data: 字段；其他字段暂不用
		const prefix = "data:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		payload := strings.TrimPrefix(line, prefix)
		// SSE 规范允许 "data:xxx" 或 "data: xxx"（后者更常见），统一去掉前导空格
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
