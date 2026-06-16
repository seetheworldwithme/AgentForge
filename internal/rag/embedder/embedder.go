// internal/rag/embedder/embedder.go
package embedder

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
)

type Embedder interface {
	Embed(texts []string) ([][]float32, error)
	EmbedOne(text string) ([]float32, error)
	Dim() int
}

// FakeEmbedder 确定性 embedding，基于内容词频哈希。仅测试用。
//
// 注意：维度通过未导出字段 dim 持有、由 Dim() 方法暴露，以满足 Embedder
// 接口（Go 不允许同类型的字段与方法同名）。请用 NewFakeEmbedder 构造。
type FakeEmbedder struct {
	dim int
}

// NewFakeEmbedder 创建指定维度的 FakeEmbedder。
func NewFakeEmbedder(dim int) *FakeEmbedder {
	return &FakeEmbedder{dim: dim}
}

func (e *FakeEmbedder) Dim() int { return e.dim }

func (e *FakeEmbedder) EmbedOne(text string) ([]float32, error) {
	vecs, err := e.Embed([]string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (e *FakeEmbedder) Embed(texts []string) ([][]float32, error) {
	if e.dim <= 0 {
		return nil, fmt.Errorf("FakeEmbedder.Dim must be > 0")
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = e.hashVector(text)
	}
	return out, nil
}

func (e *FakeEmbedder) hashVector(text string) []float32 {
	v := make([]float32, e.dim)
	for _, tok := range tokenize(text) {
		h := sha256.Sum256([]byte(tok))
		idx := int(binary.BigEndian.Uint32(h[:4])) % e.dim
		val := math.Float32frombits(binary.BigEndian.Uint32(h[4:8]))
		if val < 0 {
			val = -val
		}
		if val == 0 {
			val = 0.001
		}
		v[idx] += val
	}
	var norm float32
	for _, x := range v {
		norm += x * x
	}
	if norm > 0 {
		norm = float32(math.Sqrt(float64(norm)))
		for i := range v {
			v[i] /= norm
		}
	}
	return v
}

func tokenize(text string) []string {
	var out []string
	cur := ""
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
