package rag

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
)

// Retriever implements agent.RAGRetriever over SQLite + sqlite-vec + FTS5.
// 检索流程：query 改写扩展（默认开启）→ 每个 query 向量+FTS5 双路召回 → 跨 query
// RRF 融合 → 可选 rerank。EmbedClient/RerankClient/ChatClient 是全局默认；KB 级绑定的
// provider 优先（保证 query/doc 同模型同维度，rerank/chat 同理）。
type Retriever struct {
	DB           *store.DB
	EmbedClient  llm.LLMClient
	RerankClient llm.RerankClient // 可选：全局默认 rerank；KB 级绑定优先
	ChatClient   llm.LLMClient    // 可选：全局默认 chat，用于 query 改写扩展；KB 级绑定优先
}

type SearchHit struct {
	ChunkID    string
	DocumentID string
	Filename   string
	Ordinal    int
	Text       string
	Distance   float32 // 向量路 L2 距离（仅向量路命中的 chunk 有意义）
	Score      float32 // 最终相关性分数：rerank 的 relevance_score 或回退的 cosine/1.0
}

// CosineSimilarity converts an L2 distance reported by a vec0 index (on
// normalized embeddings) to a cosine similarity in [-1, 1]: similarity = 1 - d²/2.
func CosineSimilarity(l2 float32) float32 { return 1 - l2*l2/2 }

// candidateN 是每路召回的候选数（放大供 RRF 融合 / rerank）。
const candidateN = 20

// rrfK 是 RRF 融合常数（业界经验值，平衡头部与长尾）。
const rrfK = 60

// maxSubQueries 是 query 改写扩展最多生成的子查询数。
const maxSubQueries = 2

func (r *Retriever) Retrieve(ctx context.Context, kbID, query string, k int) ([]agent.RetrievedChunk, error) {
	hits, err := r.Search(ctx, kbID, query, k)
	if err != nil {
		return nil, err
	}
	out := make([]agent.RetrievedChunk, 0, len(hits))
	for _, h := range hits {
		out = append(out, agent.RetrievedChunk{
			ID: h.ChunkID, Text: h.Text, DocID: h.DocumentID, Filename: h.Filename,
			Similarity: h.Score,
		})
	}
	return out, nil
}

// embedClientForKB returns the embed client bound to the KB so retrieval uses
// the same model (and dimension) as ingest, falling back to the default.
func (r *Retriever) embedClientForKB(kbID string) llm.LLMClient {
	if kb, err := r.DB.GetKB(kbID); err == nil && kb.EmbedProviderID != "" {
		if p, err := r.DB.GetProvider(kb.EmbedProviderID); err == nil && p.EmbedModel != "" {
			return llm.NewOpenAIClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.EmbedModel,
			})
		}
	}
	return r.EmbedClient
}

// rerankClientForKB 返回 KB 绑定的 rerank 客户端，否则回退全局默认（可能 nil，
// 此时跳过 rerank 走纯 RRF）。
func (r *Retriever) rerankClientForKB(kbID string) llm.RerankClient {
	if kb, err := r.DB.GetKB(kbID); err == nil && kb.RerankProviderID != "" {
		if p, err := r.DB.GetProvider(kb.RerankProviderID); err == nil && p.ChatModel != "" {
			return llm.NewRerankClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.ChatModel,
			})
		}
	}
	return r.RerankClient
}

// chatClientForKB 返回 KB 绑定的 chat 客户端（用于 query 改写扩展），否则回退全局
// 默认（可能 nil，此时跳过扩展）。
func (r *Retriever) chatClientForKB(kbID string) llm.LLMClient {
	if kb, err := r.DB.GetKB(kbID); err == nil && kb.ChatProviderID != "" {
		if p, err := r.DB.GetProvider(kb.ChatProviderID); err == nil && p.ChatModel != "" {
			return llm.NewOpenAIClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.ChatModel,
			})
		}
	}
	return r.ChatClient
}

// Search 执行 query 改写扩展 → 多 query 双路召回 → 跨 query RRF 融合 → 可选 rerank，
// 返回 top-k。双路召回阶段不应用相似度阈值（阈值由 agent.go 在融合/重排后统一应用）。
func (r *Retriever) Search(ctx context.Context, kbID, query string, k int) ([]SearchHit, error) {
	if k <= 0 {
		k = 5
	}
	client := r.embedClientForKB(kbID)
	if client == nil {
		return nil, fmt.Errorf("no embed provider configured for knowledge base")
	}

	// query 改写扩展：默认开启，失败/超时回退 [query]。
	queries := r.expandQuery(ctx, kbID, query)

	// 跨 query 双路召回 + RRF 融合。原 query（queries[0]）的向量路错误致命；
	// 子查询的错误跳过该子查询，不影响整体。
	vecTable := store.SanitizeTableName(kbID)
	scores := make(map[string]float64)
	vecDist := make(map[string]float32)
	for i, q := range queries {
		vecHits, ftsHits, err := r.recallByQuery(ctx, client, vecTable, kbID, q)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("embed query: %w", err)
			}
			log.Printf("[RAG] sub-query %q recall failed, skipped: %v", q, err)
			continue
		}
		for rank, h := range vecHits {
			scores[h.ID] += 1.0 / float64(rrfK+rank+1)
			if _, ok := vecDist[h.ID]; !ok {
				vecDist[h.ID] = h.Distance // 记录首次命中的距离（用于无 rerank 时的 cosine）
			}
		}
		for rank, h := range ftsHits {
			scores[h.ChunkID] += 1.0 / float64(rrfK+rank+1)
		}
	}
	if len(scores) == 0 {
		return nil, nil
	}

	// RRF 排序取 top candidateN
	fused := make([]fusedItem, 0, len(scores))
	for id, s := range scores {
		fused = append(fused, fusedItem{chunkID: id, score: s})
	}
	sort.Slice(fused, func(i, j int) bool { return fused[i].score > fused[j].score })
	candK := min(len(fused), candidateN)
	candIDs := make([]string, candK)
	for i := 0; i < candK; i++ {
		candIDs[i] = fused[i].chunkID
	}
	chunks, err := r.DB.GetChunksByIDs(candIDs)
	if err != nil {
		return nil, err
	}
	chunkByID := make(map[string]store.Chunk, len(chunks))
	for _, c := range chunks {
		chunkByID[c.ID] = c
	}

	// 可选 rerank：失败回退 RRF 顺序。rerank 用原 query（queries[0]）。
	orderedIDs := candIDs
	rerankScore := map[string]float32{}
	if reranker := r.rerankClientForKB(kbID); reranker != nil {
		docs := make([]string, candK)
		for i, id := range candIDs {
			docs[i] = chunkByID[id].Text
		}
		rr, err := reranker.Rerank(ctx, queries[0], docs, candK)
		if err != nil {
			log.Printf("[RAG] rerank failed, fallback to RRF order: %v", err)
		} else if len(rr) > 0 {
			orderedIDs = make([]string, 0, len(rr))
			for _, x := range rr {
				if x.Index >= 0 && x.Index < len(candIDs) {
					orderedIDs = append(orderedIDs, candIDs[x.Index])
					rerankScore[candIDs[x.Index]] = x.RelevanceScore
				}
			}
		}
	}
	if len(orderedIDs) > k {
		orderedIDs = orderedIDs[:k]
	}

	// 父子分块：召回的是子块，按 parent_id 上溯到父块返回（去重，保持顺序）。
	// 老数据（无 parent_id）按 flat 处理，返回自身。
	parentIDSet := make(map[string]struct{})
	for _, id := range orderedIDs {
		if c, ok := chunkByID[id]; ok && c.ParentID != "" {
			parentIDSet[c.ParentID] = struct{}{}
		}
	}
	parentByID := make(map[string]store.Chunk)
	if len(parentIDSet) > 0 {
		pids := make([]string, 0, len(parentIDSet))
		for pid := range parentIDSet {
			pids = append(pids, pid)
		}
		if parents, err := r.DB.GetChunksByIDs(pids); err == nil {
			for _, p := range parents {
				parentByID[p.ID] = p
			}
		}
	}

	usedRerank := len(rerankScore) > 0
	seen := make(map[string]struct{}, len(orderedIDs))
	out := make([]SearchHit, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		c, ok := chunkByID[id]
		if !ok {
			continue
		}
		// 决定返回的 chunk：qa 返回自身（问答对自含答案）；content/summary 上溯父块返回上下文
		ret := c
		if c.Kind != "qa" && c.ParentID != "" {
			if p, ok := parentByID[c.ParentID]; ok {
				ret = p
			}
		}
		if _, dup := seen[ret.ID]; dup {
			continue
		}
		seen[ret.ID] = struct{}{}
		doc, _ := r.DB.GetDocument(ret.DocID)
		hit := SearchHit{
			ChunkID: ret.ID, DocumentID: ret.DocID, Filename: doc.Filename,
			Ordinal: ret.Ordinal, Text: ret.Text,
		}
		switch {
		case usedRerank:
			hit.Score = rerankScore[id] // 用触发子块的 rerank 分
		default:
			if d, hasVec := vecDist[id]; hasVec {
				hit.Distance = d
				hit.Score = CosineSimilarity(d)
			} else {
				hit.Score = 1.0 // 纯 FTS 路：关键词强命中
			}
		}
		out = append(out, hit)
	}
	return out, nil
}

// recallByQuery 对单个 query 执行向量 + FTS5 双路召回。向量路错误（embed/搜索失败）
// 透传给调用方；FTS 路错误（表不存在等）记日志并降级为空。
func (r *Retriever) recallByQuery(ctx context.Context, client llm.LLMClient, vecTable, kbID, query string) ([]store.VectorHit, []store.FTSHit, error) {
	vecs, err := client.Embed(ctx, []string{query})
	if err != nil {
		return nil, nil, err
	}
	if len(vecs) == 0 {
		return nil, nil, fmt.Errorf("embed returned no vectors")
	}
	vecHits, err := r.DB.SearchVectors(vecTable, vecs[0], candidateN)
	if err != nil {
		return nil, nil, err
	}
	ftsHits, ftsErr := r.searchFTS(kbID, query)
	if ftsErr != nil {
		log.Printf("[RAG] FTS search skipped (degraded to vector-only): %v", ftsErr)
	}
	return vecHits, ftsHits, nil
}

// expandQuery 用 chat provider 把原 query 扩展为最多 maxSubQueries 个子查询（默认
// 开启）。返回 [原query, 子查询...]；未配置 chat provider、失败或超时则返回 [原query]，
// 检索退化为单 query。
func (r *Retriever) expandQuery(ctx context.Context, kbID, query string) []string {
	chat := r.chatClientForKB(kbID)
	if chat == nil {
		return []string{query}
	}
	qctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	subs, err := r.generateSubQueries(qctx, chat, query)
	if err != nil {
		log.Printf("[RAG] query expansion failed, using original only: %v", err)
		return []string{query}
	}
	return append([]string{query}, subs...)
}

// generateSubQueries 让 chat 模型为检索改写出最多 maxSubQueries 个变体查询。解析按行，
// 去除常见编号前缀/引号/空白，过滤掉与原 query 相同或过短的行。
func (r *Retriever) generateSubQueries(ctx context.Context, chat llm.LLMClient, query string) ([]string, error) {
	prompt := "你是查询改写助手。用户要在知识库检索下面这个问题，但原问题可能口语化、简短或带指代。" +
		"请改写出最多 2 个更有利于检索的变体（更具体、更接近文档的正式表述），每行一个，" +
		"不要编号、不要解释、不要重复原问题。若原问题已足够清晰，可以不输出。\n原问题：" + query
	resp, err := chat.Chat(ctx, []llm.Message{{Role: llm.RoleUser, Content: prompt}})
	if err != nil {
		return nil, err
	}
	var subs []string
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "0123456789.-、)）. ") // 去编号前缀
		line = strings.TrimSpace(strings.Trim(line, "\"'“”‘’")) // 去首尾引号
		if line == "" || line == query || utf8.RuneCountInString(line) < 2 {
			continue
		}
		dup := false
		for _, s := range subs {
			if s == line {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		subs = append(subs, line)
		if len(subs) >= maxSubQueries {
			break
		}
	}
	return subs, nil
}

// searchFTS 构造 FTS5 phrase 并检索；< 3 个 unicode 字符返回空（跳过），表不存在
// 等错误透传给调用方降级为纯向量路。
func (r *Retriever) searchFTS(kbID, query string) ([]store.FTSHit, error) {
	expr, ok := fts5Query(query)
	if !ok {
		return nil, nil
	}
	return r.DB.SearchFTS(store.FTSTableName(kbID), expr, candidateN)
}

// fts5Query 把原始 query 构造成 FTS5 phrase 表达式：双引号包裹（转义内部双引号）
// 避免 AND/OR/NOT 等被当作 FTS5 语法。< 3 个 unicode 字符返回 false（trigram 不命中）。
func fts5Query(query string) (string, bool) {
	if utf8.RuneCountInString(query) < 3 {
		return "", false
	}
	esc := strings.ReplaceAll(query, `"`, `""`)
	return `"` + esc + `"`, true
}

// fusedItem 是 RRF 融合后的候选（按分数降序）。
type fusedItem struct {
	chunkID string
	score   float64
}
