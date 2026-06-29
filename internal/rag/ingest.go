package rag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/rag/parser"
	"github.com/agent-rust/core/internal/store"
	"github.com/oklog/ulid/v2"
)

// Ingestor parses, chunks, embeds (batched), and stores a document.
type Ingestor struct {
	DB         *store.DB
	Embed      llm.LLMClient
	Vision     llm.LLMClient // 可选，用于把图片描述成文字（VLM）；nil 时图片跳过
	Chat       llm.LLMClient // 可选，用于 QA 索引生成 + 摘要索引生成；nil 时跳过
	KBID       string
	EmbedModel string // embedding 模型名，用作缓存 key（不同模型维度不同）
	IndexMode  string // chunk | qa；空视为 chunk
	ChunkSz    int
	Overlap    int
	Parser     parser.Parser // the chosen parser for the file
}

// IngestFile parses, chunks, embeds (batched), and stores a document. Every
// failure path marks the document "failed" with a reason and logs it, so an
// ingest that dies mid-way never leaves the document stuck on "processing".
func (ing *Ingestor) IngestFile(ctx context.Context, docID, filename, mimeType string, raw []byte, resumeFrom int) error {
	log.Printf("ingest: doc=%s file=%q start", docID, filename)
	p := ing.Parser
	if p == nil {
		p = parser.Txt{}
	}
	result, err := p.Parse(bytes.NewReader(raw))
	if err != nil {
		ing.fail(docID, "parse", 0, err)
		return err
	}
	// 全新入库(resumeFrom==0)：重置进度，让图片描述阶段前端显示"解析中…"而非旧的 0/total。
	if resumeFrom == 0 {
		_ = ing.DB.SetDocumentProgress(docID, 0, 0)
	}
	// 把图片占位符替换成 VLM 描述（按文档位置）；没配 VLM 或失败则清除。
	text := ing.fillImagePlaceholders(ctx, docID, result)
	// 父子分块：先切父块（大，提供上下文）。叶子单元（向量化单元）按索引模式生成：
	//   - chunk 模式：父块切成子块（content）
	//   - qa 模式：父块由 chat 转成问答对（qa），用问题向量化；失败回退 content
	//   - 额外：若配了 chat，为每个父块生成摘要叶子（summary）
	parents := Chunk(text, ing.chunkSize()*4, ing.overlap())
	type leafEntry struct {
		parentIdx int
		embText   string // 向量化文本（qa 用问题，summary 用摘要，content 用子块）
		storeText string // 存 chunk.text（qa 存问答对，其余同 embText）
		kind      string
	}
	var leaves []leafEntry
	for pi, p := range parents {
		if ing.IndexMode == "qa" && ing.Chat != nil {
			if qas := ing.generateQA(ctx, docID, p); len(qas) > 0 {
				for _, qa := range qas {
					leaves = append(leaves, leafEntry{parentIdx: pi, embText: qa.q,
						storeText: "问：" + qa.q + "\n答：" + qa.a, kind: "qa"})
				}
			} else {
				// QA 生成失败，回退为 content 子块
				for _, ch := range Chunk(p, ing.chunkSize(), ing.overlap()) {
					leaves = append(leaves, leafEntry{parentIdx: pi, embText: ch, storeText: ch, kind: "content"})
				}
			}
		} else {
			for _, ch := range Chunk(p, ing.chunkSize(), ing.overlap()) {
				leaves = append(leaves, leafEntry{parentIdx: pi, embText: ch, storeText: ch, kind: "content"})
			}
		}
		// 注：摘要索引（每父块生成摘要）默认不启用——大文档下每父块一次 Chat 会严重拖慢入库。
		// 如需启用，应在 KB 级别显式开关，而非「配了 chat 就自动摘要」。generateSummary 方法保留备用。
	}
	log.Printf("ingest: doc=%s parsed %d chars -> %d parents, %d leaves (resumeFrom=%d)", docID, len(text), len(parents), len(leaves), resumeFrom)

	// 续传一致性：resumeFrom 超过叶子数（切分参数变化/图片描述变化等）则从头。
	if resumeFrom > len(leaves) {
		log.Printf("ingest: doc=%s resumeFrom %d > leaves %d, starting fresh", docID, resumeFrom, len(leaves))
		resumeFrom = 0
	}

	// FTS5 表不依赖 embedding 维度，先建好（trigram 全文索引）。
	if err := ing.DB.EnsureFTSTable(ftsTable(ing.KBID)); err != nil {
		ing.fail(docID, "fts-table", 0, err)
		return err
	}

	// 父块（仅存文本，不向量化/FTS，供检索时 join 返回上下文）。
	// 全新(resumeFrom==0)：生成并写入；恢复(resumeFrom>0)：父块已写，从 DB 读回 ID。
	parentIDs := make([]string, len(parents))
	if resumeFrom == 0 {
		for pi, p := range parents {
			parentIDs[pi] = "chunk_" + ulid.Make().String()
			if err := ing.DB.CreateChunk(store.Chunk{
				ID: parentIDs[pi], DocID: docID, KBID: ing.KBID, Ordinal: pi, Text: p,
			}); err != nil {
				ing.fail(docID, "store", pi, err)
				return err
			}
		}
	} else {
		existing, err := ing.DB.ListChunksByDoc(docID)
		if err != nil {
			ing.fail(docID, "store", 0, err)
			return err
		}
		for _, c := range existing {
			if c.Ordinal >= 0 && c.Ordinal < len(parents) {
				parentIDs[c.Ordinal] = c.ID
			}
		}
		// 父块完整性校验：切分参数变化（chunk_size/index_mode）或上轮中断在写父块会导致
		// 父块缺失。此时不续传（否则叶子悬空、新旧切分混合索引），标 failed 让用户 retry 重建。
		for _, id := range parentIDs {
			if id == "" {
				err := fmt.Errorf("续传不一致：父块缺失（可能配置已变更），请重试重建索引")
				ing.fail(docID, "resume", resumeFrom, err)
				return err
			}
		}
	}

	// 进度初始化（done 从 resumeFrom 开始，前端进度条不倒退）。
	_ = ing.DB.SetDocumentProgress(docID, resumeFrom, len(leaves))

	// 逐批处理叶子[resumeFrom:]：嵌入（走缓存）+ 写库 + 更新进度。每批结束刷新 chunk_done，
	// 中断（core 关闭/ctx 超时）后下次从 chunk_done 恢复。
	pending := leaves[resumeFrom:]
	dim := 0
	const batch = 64
	for s := 0; s < len(pending); s += batch {
		e := s + batch
		if e > len(pending) {
			e = len(pending)
		}
		batchLeaves := pending[s:e]
		texts := make([]string, len(batchLeaves))
		hashes := make([]string, len(batchLeaves))
		for k, l := range batchLeaves {
			texts[k] = l.embText
			hashes[k] = hashText(l.embText)
		}
		// 查 embedding 缓存
		vecByHash := make(map[string][]float32, len(batchLeaves))
		model := ing.EmbedModel
		if cached, err := ing.DB.GetCachedEmbeddings(model, hashes); err == nil {
			for h, v := range cached {
				vecByHash[h] = v
			}
		} else {
			log.Printf("ingest: doc=%s embedding cache read failed (proceeding without cache): %v", docID, err)
		}
		// 未命中批量嵌入
		var missK []int
		for k, h := range hashes {
			if _, ok := vecByHash[h]; !ok {
				missK = append(missK, k)
			}
		}
		if len(missK) > 0 {
			missTexts := make([]string, len(missK))
			for k, idx := range missK {
				missTexts[k] = texts[idx]
			}
			vecs, err := ing.Embed.Embed(ctx, missTexts)
			if err != nil {
				// ctx 取消（暂停）或超时：保留当前 status + 刷新进度，下次续传接力（不标 failed）。
				if ctx.Err() != nil {
					done := resumeFrom + s
					log.Printf("ingest: doc=%s stopped at leaf %d/%d (ctx: %v), will resume", docID, done, len(leaves), ctx.Err())
					_ = ing.DB.SetDocumentProgress(docID, done, len(leaves))
					return nil
				}
				ing.fail(docID, "embed", resumeFrom+s, err)
				return err
			}
			if len(vecs) != len(missK) {
				err := fmt.Errorf("embed returned %d vectors for %d chunks", len(vecs), len(missK))
				ing.fail(docID, "embed", resumeFrom+s, err)
				return err
			}
			for k, vec := range vecs {
				idx := missK[k]
				vecByHash[hashes[idx]] = vec
				if dim == 0 && len(vec) > 0 {
					dim = len(vec)
				}
				if err := ing.DB.PutCachedEmbedding(model, hashes[idx], vec); err != nil {
					log.Printf("ingest: doc=%s embedding cache write failed: %v", docID, err)
				}
			}
		}
		// 确定向量维度（首批），建 vec0 表（IF NOT EXISTS，后续批无害）。
		if dim == 0 {
			for _, v := range vecByHash {
				if len(v) > 0 {
					dim = len(v)
					break
				}
			}
		}
		if dim == 0 {
			err := fmt.Errorf("no embeddings produced for batch at leaf %d", resumeFrom+s)
			ing.fail(docID, "embed", resumeFrom+s, err)
			return err
		}
		if err := ing.DB.EnsureVecTable(vecTable(ing.KBID), dim); err != nil {
			ing.fail(docID, "vec-table", 0, err)
			return err
		}
		// 写本批叶子：文本(storeText) + 向量 + FTS(embText) + parent_id + kind。
		for k, l := range batchLeaves {
			leafIdx := resumeFrom + s + k
			chunkID := "chunk_" + ulid.Make().String()
			if err := ing.DB.CreateChunk(store.Chunk{
				ID: chunkID, DocID: docID, KBID: ing.KBID, Ordinal: len(parents) + leafIdx, Text: l.storeText,
				ParentID: parentIDs[l.parentIdx], Kind: l.kind,
			}); err != nil {
				ing.fail(docID, "store", leafIdx, err)
				return err
			}
			if err := ing.DB.InsertVector(vecTable(ing.KBID), chunkID, vecByHash[hashes[k]]); err != nil {
				ing.fail(docID, "store", leafIdx, err)
				return err
			}
			if err := ing.DB.InsertFTS(ftsTable(ing.KBID), chunkID, l.embText); err != nil {
				ing.fail(docID, "fts-store", leafIdx, err)
				return err
			}
		}
		// 刷新进度
		done := resumeFrom + e
		_ = ing.DB.SetDocumentProgress(docID, done, len(leaves))
		log.Printf("ingest: doc=%s progress %d/%d leaves", docID, done, len(leaves))
	}

	_ = ing.DB.SetDocumentStatus(docID, "ready", len(leaves), "")
	log.Printf("ingest: doc=%s ready (%d parents, %d leaves)", docID, len(parents), len(leaves))
	return nil
}

// fail marks the document failed and logs the stage + cause. `done` is the
// number of chunks already written, surfaced as chunk_count for debugging.
func (ing *Ingestor) fail(docID, stage string, done int, err error) {
	log.Printf("ingest: doc=%s FAILED at %s: %v", docID, stage, err)
	_ = ing.DB.SetDocumentStatus(docID, "failed", done, stage+": "+err.Error())
}

func (ing *Ingestor) chunkSize() int {
	if ing.ChunkSz > 0 {
		return ing.ChunkSz
	}
	return 500
}
func (ing *Ingestor) overlap() int {
	if ing.Overlap > 0 {
		return ing.Overlap
	}
	return 60
}

func vecTable(kbID string) string { return store.SanitizeTableName(kbID) }
func ftsTable(kbID string) string { return store.FTSTableName(kbID) }

// hashText 返回文本的 SHA-256 十六进制摘要，用作 embedding 缓存的 text_hash key。
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// 单文档最多描述这么多张图片，超出跳过（避免大文档把 VLM 打爆）。
const maxImagesPerDoc = 20

// fillImagePlaceholders 把 ParseResult 里的图片占位符替换成 VLM 描述（按位置）。
// 没配 Vision、图片数超限或单图描述失败时，对应占位符替换为空（图片跳过）。
func (ing *Ingestor) fillImagePlaceholders(ctx context.Context, docID string, res parser.ParseResult) string {
	if len(res.Images) == 0 {
		return parser.ReplacePlaceholders(res.Text, func(int) string { return "" })
	}
	descs := ing.describeImages(ctx, docID, res.Images)
	return parser.ReplacePlaceholders(res.Text, func(i int) string {
		if i < len(descs) && descs[i] != "" {
			return "\n[图片内容]\n" + descs[i] + "\n"
		}
		return ""
	})
}

// describeImages 并发调用 VLM 描述每张图片，返回与输入等长的描述切片
// （未配置/失败的位置为空字符串）。
func (ing *Ingestor) describeImages(ctx context.Context, docID string, imgs []parser.ParsedImage) []string {
	descs := make([]string, len(imgs))
	if ing.Vision == nil {
		log.Printf("ingest: doc=%s %d images skipped (no vision model)", docID, len(imgs))
		return descs
	}
	n := len(imgs)
	if n > maxImagesPerDoc {
		log.Printf("ingest: doc=%s has %d images, describing first %d only", docID, n, maxImagesPerDoc)
		n = maxImagesPerDoc
	}
	// 限制 VLM 并发（4）：一次 20 张并发易触发 provider 限流，反而整体更慢。
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	sem := make(chan struct{}, 4)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			desc, err := ing.describeOneImage(ctx, imgs[i])
			if err != nil {
				log.Printf("ingest: doc=%s image %d VLM failed: %v", docID, i, err)
				return
			}
			descs[i] = desc
			mu.Lock()
			done++
			log.Printf("ingest: doc=%s described image %d/%d", docID, done, n)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return descs
}

func (ing *Ingestor) describeOneImage(ctx context.Context, img parser.ParsedImage) (string, error) {
	dataURL := "data:" + img.MIME + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
	// 让 VLM 转录图片里的全部文字内容（而非概括），这样代码/配置/表格等
	// 细节能进 RAG 被检索到；只有纯示意图才做简要说明。
	prompt := "请提取这张图片的全部内容，用于知识库检索：\n" +
		"1. 图片里的所有文字（标题、正文、列表、代码、配置、命令、表格等）逐字完整转录，" +
		"保持原文顺序与结构，代码用 ``` 代码块、表格用 Markdown 表格，不要概括或改写；\n" +
		"2. 若图片是流程图/示意图/照片、文字很少，则简要说明其关键信息（节点、流程、关系、场景）；\n" +
		"3. 只输出内容本身，不要加任何前言或解释。"
	return ing.Vision.Chat(ctx, []llm.Message{{
		Role:    llm.RoleUser,
		Content: prompt,
		Images:  []llm.ImageRef{{DataURL: dataURL}},
	}})
}

// qaPair 是 QA 索引模式生成的一个问答对。
type qaPair struct{ q, a string }

// generateQA 让 chat 模型把一段文本转成问答对（QA 索引模式）。问题像用户会问的自然
// 问题，答案基于原文。失败返回 nil（调用方回退为 content 子块）。
func (ing *Ingestor) generateQA(ctx context.Context, docID, text string) []qaPair {
	if ing.Chat == nil {
		return nil
	}
	prompt := "请把下面这段文本转成问答对，用于知识库检索。每行一个，格式：问题|答案（用竖线分隔）。" +
		"问题要像用户会自然提问的样子，答案基于原文、简洁准确。最多 5 个，不要输出任何其他内容。\n文本：" + text
	resp, err := ing.Chat.Chat(ctx, []llm.Message{{Role: llm.RoleUser, Content: prompt}})
	if err != nil {
		log.Printf("ingest: doc=%s QA generation failed: %v", docID, err)
		return nil
	}
	var pairs []qaPair
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
			pairs = append(pairs, qaPair{q: strings.TrimSpace(parts[0]), a: strings.TrimSpace(parts[1])})
		}
	}
	return pairs
}

// generateSummary 让 chat 模型为一段文本生成摘要（摘要索引）。失败返回空串。
func (ing *Ingestor) generateSummary(ctx context.Context, docID, text string) string {
	if ing.Chat == nil {
		return ""
	}
	prompt := "请用一两句话概括下面这段文本的核心内容，用于知识库检索。只输出摘要本身，不要解释。\n文本：" + text
	resp, err := ing.Chat.Chat(ctx, []llm.Message{{Role: llm.RoleUser, Content: prompt}})
	if err != nil {
		log.Printf("ingest: doc=%s summary generation failed: %v", docID, err)
		return ""
	}
	return strings.TrimSpace(resp)
}
