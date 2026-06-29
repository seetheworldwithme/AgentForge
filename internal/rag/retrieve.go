package rag

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
)

// Retriever implements agent.RAGRetriever over SQLite + sqlite-vec + FTS5.
// 检索走双路：向量（语义）+ FTS5 trigram（关键词），RRF 融合后可选 rerank 重排。
// EmbedClient/RerankClient 是全局默认；KB 级绑定的 provider 优先（保证 query/doc
// 同模型同维度，rerank 同理）。
type Retriever struct {
	DB           *store.DB
	EmbedClient  llm.LLMClient
	RerankClient llm.RerankClient // 可选：全局默认 rerank；KB 级绑定优先
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
// L2 and cosine are monotonic on unit vectors, so ranking is unchanged; the
// number is just on the intuitive "higher = more relevant" scale.
func CosineSimilarity(l2 float32) float32 { return 1 - l2*l2/2 }

// candidateN 是每路召回的候选数（放大供 RRF 融合 / rerank）。
const candidateN = 20

// rrfK 是 RRF 融合常数（业界经验值，平衡头部与长尾）。
const rrfK = 60

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

// Search 执行向量 + FTS5 双路召回，RRF 融合，可选 rerank，返回 top-k。双路召回
// 阶段不应用相似度阈值（阈值由 agent.go 在融合/重排后统一应用，避免误删互补候选）。
func (r *Retriever) Search(ctx context.Context, kbID, query string, k int) ([]SearchHit, error) {
	if k <= 0 {
		k = 5
	}

	// === 向量路 ===
	client := r.embedClientForKB(kbID)
	if client == nil {
		return nil, fmt.Errorf("no embed provider configured for knowledge base")
	}
	vecs, err := client.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed query returned no vectors")
	}
	vecTable := store.SanitizeTableName(kbID)
	vecHits, err := r.DB.SearchVectors(vecTable, vecs[0], candidateN)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// === FTS 路（< 3 字符跳过；表不存在则降级纯向量路）===
	ftsHits, ftsErr := r.searchFTS(kbID, query)
	if ftsErr != nil {
		log.Printf("[RAG] FTS search skipped (degraded to vector-only): %v", ftsErr)
	}

	// === RRF 融合 ===
	fused := rrfFuse(vecHits, ftsHits, rrfK)
	if len(fused) == 0 {
		return nil, nil
	}

	// 取候选文本（RRF top candidateN，供 rerank）
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

	// === 可选 rerank：失败回退 RRF 顺序 ===
	orderedIDs := candIDs
	rerankScore := map[string]float32{}
	if reranker := r.rerankClientForKB(kbID); reranker != nil {
		docs := make([]string, candK)
		for i, id := range candIDs {
			docs[i] = chunkByID[id].Text
		}
		rr, err := reranker.Rerank(ctx, query, docs, candK)
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

	// === 组装 SearchHit ===
	vecDist := make(map[string]float32, len(vecHits))
	for _, h := range vecHits {
		vecDist[h.ID] = h.Distance
	}
	usedRerank := len(rerankScore) > 0
	out := make([]SearchHit, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		c, ok := chunkByID[id]
		if !ok {
			continue
		}
		doc, _ := r.DB.GetDocument(c.DocID)
		hit := SearchHit{
			ChunkID: c.ID, DocumentID: c.DocID, Filename: doc.Filename,
			Ordinal: c.Ordinal, Text: c.Text,
		}
		switch {
		case usedRerank:
			hit.Score = rerankScore[id]
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

// rrfFuse 用 Reciprocal Rank Fusion 融合向量路与 FTS 路：score = Σ 1/(k+rank)，
// rank 从 1 起。某 chunk 只在一路命中时，另一路不累加（非 +∞）。
func rrfFuse(vec []store.VectorHit, fts []store.FTSHit, kRRF int) []fusedItem {
	scores := make(map[string]float64)
	for rank, h := range vec {
		scores[h.ID] += 1.0 / float64(kRRF+rank+1)
	}
	for rank, h := range fts {
		scores[h.ChunkID] += 1.0 / float64(kRRF+rank+1)
	}
	items := make([]fusedItem, 0, len(scores))
	for id, s := range scores {
		items = append(items, fusedItem{chunkID: id, score: s})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })
	return items
}
