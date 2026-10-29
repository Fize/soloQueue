//go:build cgo

package localonnx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/infra/tokenizer"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
	ort "github.com/yalue/onnxruntime_go"
)

var layaOptions = []string{
	"general: " + tasktype.GeneralDescription,
	"engineering: " + tasktype.EngineeringDescription,
	"research: " + tasktype.ResearchDescription,
}
var layaLabels = []string{string(tasktype.General), string(tasktype.Engineering), string(tasktype.Research)}

// Laya holds one local decision model in an isolated process.
type Laya struct {
	mu                                 sync.Mutex
	tokenizer                          *sentencepiece.Tokenizer
	session                            *ort.DynamicAdvancedSession
	cls, sep, mask, maxLen, headMaxLen int
}
type LayaResult struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
}

func OpenLaya(modelDir, runtimePath string) (*Laya, error) {
	tok, err := sentencepiece.NewTokenizer(filepath.Join(modelDir, "tokenizer", "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	m := tok.Model()
	cls, sep, mask := m.PieceToId("<bos>"), m.PieceToId("<eos>"), m.PieceToId("<mask>")
	if cls < 0 || sep < 0 || mask < 0 {
		return nil, errors.New("laya special tokens missing")
	}
	var cfg struct {
		MaxLen     int `json:"max_len"`
		HeadMaxLen int `json:"head_max_len"`
	}
	data, err := os.ReadFile(filepath.Join(modelDir, "rl_agent_config.json"))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.MaxLen <= 0 || cfg.HeadMaxLen <= 0 || cfg.MaxLen > 8192 || cfg.HeadMaxLen > cfg.MaxLen {
		return nil, errors.New("invalid laya token budgets")
	}
	if err = configureRuntimeDirectory(runtimePath); err != nil {
		return nil, err
	}
	ort.SetSharedLibraryPath(runtimePath)
	if err = ort.InitializeEnvironment(); err != nil {
		return nil, err
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		_ = ort.DestroyEnvironment()
		return nil, err
	}
	defer opts.Destroy()
	if err = opts.SetIntraOpNumThreads(2); err != nil {
		_ = ort.DestroyEnvironment()
		return nil, err
	}
	sess, err := ort.NewDynamicAdvancedSession(filepath.Join(modelDir, "model.onnx"), []string{"input_ids", "attention_mask", "marker_pos", "marker_mask", "qtype"}, []string{"logits"}, opts)
	if err != nil {
		_ = ort.DestroyEnvironment()
		return nil, err
	}
	return &Laya{tokenizer: tok, session: sess, cls: cls, sep: sep, mask: mask, maxLen: cfg.MaxLen, headMaxLen: cfg.HeadMaxLen}, nil
}

// pythonJSON formats a JSON message list as the model's reference input does.
func pythonJSON(raw []byte) (string, error) {
	if !json.Valid(raw) {
		return "", errors.New("invalid message JSON")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}
	b := compact.Bytes()
	var out strings.Builder
	for i := 0; i < len(b); {
		switch b[i] {
		case '"':
			start := i
			i++
			for i < len(b) {
				if b[i] == '\\' {
					i += 2
					continue
				}
				if b[i] == '"' {
					i++
					break
				}
				i++
			}
			var value string
			if err := json.Unmarshal(b[start:i], &value); err != nil {
				return "", err
			}
			out.WriteByte('"')
			for _, r := range value {
				switch r {
				case '"', '\\':
					out.WriteByte('\\')
					out.WriteRune(r)
				case '\b':
					out.WriteString(`\b`)
				case '\f':
					out.WriteString(`\f`)
				case '\n':
					out.WriteString(`\n`)
				case '\r':
					out.WriteString(`\r`)
				case '\t':
					out.WriteString(`\t`)
				default:
					if r < 0x20 {
						fmt.Fprintf(&out, `\u%04x`, r)
					} else {
						out.WriteRune(r)
					}
				}
			}
			out.WriteByte('"')
		case ',', ':':
			out.WriteByte(b[i])
			out.WriteByte(' ')
			i++
		default:
			out.WriteByte(b[i])
			i++
		}
	}
	return out.String(), nil
}

func (m *Laya) sequence(raw []byte) ([]int64, []int64, error) {
	text, err := pythonJSON(raw)
	if err != nil {
		return nil, nil, err
	}
	text = strings.ReplaceAll(text, "<mask>", " ")
	head, err := m.tokenizer.Encode("choice question: What is the primary nature of the user's request?")
	if err != nil {
		return nil, nil, err
	}
	fragments := make([][]int, len(layaOptions))
	total := 0
	for i, opt := range layaOptions {
		v, e := m.tokenizer.Encode(" " + opt)
		if e != nil {
			return nil, nil, e
		}
		if len(v) > 48 {
			v = v[:48]
		}
		fragments[i] = append([]int{m.mask}, v...)
		total += len(fragments[i])
	}
	budget := m.headMaxLen - total
	if budget < 16 {
		per := (m.headMaxLen - 16) / len(fragments)
		if per < 4 {
			per = 4
		}
		total = 0
		for i := range fragments {
			if len(fragments[i]) > per {
				fragments[i] = fragments[i][:per]
			}
			total += len(fragments[i])
		}
		budget = m.headMaxLen - total
	}
	if budget < 8 {
		budget = 8
	}
	if len(head) > budget {
		head = head[:budget]
	}
	ids := []int64{int64(m.cls)}
	for _, x := range head {
		ids = append(ids, int64(x))
	}
	ids = append(ids, int64(m.sep))
	markers := make([]int64, 0, len(fragments))
	for _, f := range fragments {
		markers = append(markers, int64(len(ids)))
		for _, x := range f {
			ids = append(ids, int64(x))
		}
	}
	ids = append(ids, int64(m.sep))
	room := m.maxLen - len(ids) - 1
	if room < 0 {
		return nil, nil, errors.New("laya head exceeds model limit")
	}
	state, err := m.tokenizer.Encode(text)
	if err != nil {
		return nil, nil, err
	}
	if len(state) > room {
		state = state[len(state)-room:]
	}
	for _, x := range state {
		ids = append(ids, int64(x))
	}
	ids = append(ids, int64(m.sep))
	return ids, markers, nil
}

func (m *Laya) Decide(raw []byte) (LayaResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return LayaResult{}, errors.New("laya closed")
	}
	ids, markers, err := m.sequence(raw)
	if err != nil {
		return LayaResult{}, err
	}
	shape := ort.NewShape(1, int64(len(ids)))
	mask := make([]int64, len(ids))
	for i := range mask {
		mask[i] = 1
	}
	a, err := ort.NewTensor(shape, ids)
	if err != nil {
		return LayaResult{}, err
	}
	defer a.Destroy()
	b, err := ort.NewTensor(shape, mask)
	if err != nil {
		return LayaResult{}, err
	}
	defer b.Destroy()
	ms := ort.NewShape(1, int64(len(markers)))
	c, err := ort.NewTensor(ms, markers)
	if err != nil {
		return LayaResult{}, err
	}
	defer c.Destroy()
	valid := make([]bool, len(markers))
	for i := range valid {
		valid[i] = true
	}
	d, err := ort.NewTensor(ms, valid)
	if err != nil {
		return LayaResult{}, err
	}
	defer d.Destroy()
	q, err := ort.NewTensor(ort.NewShape(1), []int64{0})
	if err != nil {
		return LayaResult{}, err
	}
	defer q.Destroy()
	out := []ort.Value{nil}
	if err = m.session.Run([]ort.Value{a, b, c, d, q}, out); err != nil {
		return LayaResult{}, err
	}
	defer out[0].Destroy()
	tensor, ok := out[0].(*ort.Tensor[float32])
	if !ok || len(tensor.GetData()) != 3 {
		return LayaResult{}, errors.New("invalid laya output")
	}
	logits := tensor.GetData()
	max := float64(logits[0])
	for _, v := range logits {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return LayaResult{}, errors.New("invalid laya logits")
		}
		if float64(v) > max {
			max = float64(v)
		}
	}
	probs := make(map[string]float64, 3)
	var sum float64
	for _, v := range logits {
		sum += math.Exp(float64(v) - max)
	}
	best := 0
	for i, v := range logits {
		probs[layaLabels[i]] = math.Exp(float64(v)-max) / sum
		if v > logits[best] {
			best = i
		}
	}
	return LayaResult{Choice: layaLabels[best], Probabilities: probs}, nil
}
func (m *Laya) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		return nil
	}
	err := m.session.Destroy()
	m.session = nil
	return errors.Join(err, ort.DestroyEnvironment())
}
