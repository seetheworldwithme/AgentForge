package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// RerankClient 对候选文档按与 query 的相关性重排序。独立于 LLMClient（那个是
// Chat/Embed 能力，rerank 是检索阶段的独立环节）。
type RerankClient interface {
	Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error)
}

// RerankResult 是一个候选的重排结果：Index 对应传入 documents 数组的下标，
// RelevanceScore 是相关性分数（越高越相关）。
type RerankResult struct {
	Index          int
	RelevanceScore float32
}

type rerankHTTPClient struct {
	cfg  Config
	http *http.Client
}

// NewRerankClient 构造一个 Jina/Cohere 兼容的 rerank HTTP 客户端。两者请求体
// （model/query/documents/top_n/return_documents）与响应体（results[].{index,
// relevance_score}）字段一致，因此一份实现可同时对接。endpoint 为 {BaseURL}/rerank，
// 模型名取自 cfg.Model（provider 复用 chat_model 列存储 rerank 模型名）。
func NewRerankClient(cfg Config) RerankClient {
	return &rerankHTTPClient{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *rerankHTTPClient) Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	body, _ := json.Marshal(map[string]any{
		"model":            c.cfg.Model,
		"query":            query,
		"documents":        documents,
		"top_n":            topN,
		"return_documents": false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: "rerank: " + string(b)}
	}
	var out struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float32 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	res := make([]RerankResult, len(out.Results))
	for i, r := range out.Results {
		res[i] = RerankResult{Index: r.Index, RelevanceScore: r.RelevanceScore}
	}
	return res, nil
}
