package rag

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"sync"

	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/rag/parser"
	"github.com/agent-rust/core/internal/store"
	"github.com/oklog/ulid/v2"
)

// Ingestor parses, chunks, embeds (batched), and stores a document.
type Ingestor struct {
	DB      *store.DB
	Embed   llm.LLMClient
	Vision  llm.LLMClient // 可选，用于把图片描述成文字（VLM）；nil 时图片跳过
	KBID    string
	ChunkSz int
	Overlap int
	Parser  parser.Parser // the chosen parser for the file
}

// IngestFile parses, chunks, embeds (batched), and stores a document. Every
// failure path marks the document "failed" with a reason and logs it, so an
// ingest that dies mid-way never leaves the document stuck on "processing".
func (ing *Ingestor) IngestFile(ctx context.Context, docID, filename, mimeType string, raw []byte) error {
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
	// 把图片占位符替换成 VLM 描述（按文档位置）；没配 VLM 或失败则清除。
	text := ing.fillImagePlaceholders(ctx, docID, result)
	chunks := Chunk(text, ing.chunkSize(), ing.overlap())
	log.Printf("ingest: doc=%s parsed %d chars -> %d chunks", docID, len(text), len(chunks))

	// Ensure the vec0 table exists lazily, once we know the embedding dim.
	dim := 0
	const batch = 64
	for i := 0; i < len(chunks); i += batch {
		end := i + batch
		if end > len(chunks) {
			end = len(chunks)
		}
		vecs, err := ing.Embed.Embed(ctx, chunks[i:end])
		if err != nil {
			ing.fail(docID, "embed", i, err)
			return err
		}
		if len(vecs) != end-i {
			err := fmt.Errorf("embed returned %d vectors for %d chunks", len(vecs), end-i)
			ing.fail(docID, "embed", i, err)
			return err
		}
		for j, vec := range vecs {
			idx := i + j
			chunkID := "chunk_" + ulid.Make().String()
			if err := ing.DB.CreateChunk(store.Chunk{
				ID: chunkID, DocID: docID, KBID: ing.KBID, Ordinal: idx, Text: chunks[idx],
			}); err != nil {
				ing.fail(docID, "store", idx, err)
				return err
			}
			if dim == 0 {
				dim = len(vec)
				if err := ing.DB.EnsureVecTable(vecTable(ing.KBID), dim); err != nil {
					ing.fail(docID, "vec-table", idx, err)
					return err
				}
			}
			if err := ing.DB.InsertVector(vecTable(ing.KBID), chunkID, vec); err != nil {
				ing.fail(docID, "store", idx, err)
				return err
			}
		}
		log.Printf("ingest: doc=%s embedded %d/%d chunks", docID, end, len(chunks))
	}

	_ = ing.DB.SetDocumentStatus(docID, "ready", len(chunks), "")
	log.Printf("ingest: doc=%s ready (%d chunks)", docID, len(chunks))
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
	return 800
}
func (ing *Ingestor) overlap() int {
	if ing.Overlap > 0 {
		return ing.Overlap
	}
	return 100
}

func vecTable(kbID string) string { return store.SanitizeTableName(kbID) }

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
			return "[图片：" + descs[i] + "]"
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
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			desc, err := ing.describeOneImage(ctx, imgs[i])
			if err != nil {
				log.Printf("ingest: doc=%s image %d VLM failed: %v", docID, i, err)
				return
			}
			descs[i] = desc
		}(i)
	}
	wg.Wait()
	return descs
}

func (ing *Ingestor) describeOneImage(ctx context.Context, img parser.ParsedImage) (string, error) {
	dataURL := "data:" + img.MIME + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
	return ing.Vision.Chat(ctx, []llm.Message{{
		Role: llm.RoleUser,
		Content: "请用简洁中文描述这张图片；若是图表/流程图/截图，说明其关键信息，" +
			"控制在两三句以内。",
		Images: []llm.ImageRef{{DataURL: dataURL}},
	}})
}
