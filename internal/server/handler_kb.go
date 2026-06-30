package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/rag"
	"github.com/agent-rust/core/internal/rag/parser"
	"github.com/agent-rust/core/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

type KBHandler struct {
	DB           *store.DB
	EmbedClient  llm.LLMClient // for ingest + retrieve (nil until configured)
	EmbedModel   string        // 全局默认 embedding 模型名（缓存 key）；KB 绑定的优先
	RAG          agent.RAGRetriever
	UploadDir    string
	ingestMu     sync.Map // docID -> *sync.Mutex：同一文档入库串行化（upload/恢复/retry 互斥）
	ingestCancel sync.Map // docID -> context.CancelFunc：暂停时取消入库 goroutine
}

func (h *KBHandler) Routes(r chi.Router) {
	r.Get("/kb", h.list)
	r.Post("/kb", h.create)
	r.Put("/kb/{id}", h.update)
	r.Delete("/kb/{id}", h.delete)
	r.Post("/kb/{id}/documents", h.upload)
	r.Get("/kb/{id}/documents", h.listDocs)
	r.Delete("/kb/{id}/documents/{doc_id}", h.deleteDoc)
	r.Post("/kb/{id}/documents/{doc_id}/retry", h.retryDoc)
	r.Post("/kb/{id}/documents/{doc_id}/pause", h.pauseDoc)
	r.Post("/kb/{id}/documents/{doc_id}/resume", h.resumeDoc)
	r.Get("/kb/{id}/documents/{doc_id}/chunks", h.listChunks)
	r.Get("/kb/{id}/documents/{doc_id}/status", h.docStatus)
	r.Post("/kb/{id}/chunk-preview", h.chunkPreview)
	r.Post("/kb/{id}/retrieve", h.retrieve)
}

type kbDTO struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	EmbedProvider   string `json:"embed_provider_id"`
	ChatProvider    string `json:"chat_provider_id"`
	RerankProvider  string `json:"rerank_provider_id"`
	IndexMode       string `json:"index_mode"`
	ChunkSize       int    `json:"chunk_size"`
	ChunkOverlap    int    `json:"chunk_overlap"`
	DocCount        int    `json:"doc_count"`
	ReadyCount      int    `json:"ready_count"`
	ProcessingCount int    `json:"processing_count"`
	FailedCount     int    `json:"failed_count"`
	DuplicateCount  int    `json:"duplicate_count"`
	CreatedAt       string `json:"created_at"`
}

// kbToDTO 把 KB + 文档状态计数组装成 DTO；counts 缺省（零值）时各计数为 0。
func kbToDTO(k store.KnowledgeBase, c store.KBStatusCount) kbDTO {
	return kbDTO{
		ID: k.ID, Name: k.Name, Description: k.Description,
		EmbedProvider: k.EmbedProviderID, ChatProvider: k.ChatProviderID,
		RerankProvider: k.RerankProviderID,
		IndexMode:      k.IndexMode,
		ChunkSize:      k.ChunkSize, ChunkOverlap: k.ChunkOverlap,
		DocCount: k.DocCount, CreatedAt: k.CreatedAt,
		ReadyCount: c.Ready, ProcessingCount: c.Processing,
		FailedCount: c.Failed, DuplicateCount: c.Duplicate,
	}
}

type documentDTO struct {
	ID         string `json:"id"`
	KBID       string `json:"kb_id"`
	Filename   string `json:"filename"`
	FileSize   int64  `json:"file_size"`
	MimeType   string `json:"mime_type"`
	Status     string `json:"status"`
	ChunkCount int    `json:"chunk_count"`
	ChunkDone  int    `json:"chunk_done"`
	ChunkTotal int    `json:"chunk_total"`
	Error      string `json:"error,omitempty"`
	RawPath    string `json:"raw_path,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type chunkDTO struct {
	ID         string `json:"id"`
	DocumentID string `json:"document_id"`
	KBID       string `json:"kb_id"`
	Ordinal    int    `json:"ordinal"`
	Text       string `json:"text"`
	TokenCount int    `json:"token_count"`
	Metadata   string `json:"metadata,omitempty"`
}

type retrieveHitDTO struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Filename   string  `json:"filename"`
	Ordinal    int     `json:"ordinal"`
	Text       string  `json:"text"`
	Similarity float32 `json:"similarity"`
}

func (h *KBHandler) list(w http.ResponseWriter, r *http.Request) {
	kbs, err := h.DB.ListKBs()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts, _ := h.DB.MapKBStatusCounts()
	out := make([]kbDTO, len(kbs))
	for i, k := range kbs {
		out[i] = kbToDTO(k, counts[k.ID])
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *KBHandler) create(w http.ResponseWriter, r *http.Request) {
	var dto kbDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	kb := store.KnowledgeBase{
		ID: "kb_" + ulid.Make().String(), Name: dto.Name, Description: dto.Description,
		EmbedProviderID: dto.EmbedProvider, ChatProviderID: dto.ChatProvider,
		RerankProviderID: dto.RerankProvider,
		IndexMode:        dto.IndexMode,
		ChunkSize:        dto.ChunkSize, ChunkOverlap: dto.ChunkOverlap,
		CreatedAt: now,
	}
	if err := h.DB.CreateKB(kb); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts, _ := h.DB.MapKBStatusCounts()
	writeJSON(w, http.StatusCreated, kbToDTO(kb, counts[kb.ID]))
}

func (h *KBHandler) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	current, err := h.DB.GetKB(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	var dto kbDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if dto.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	current.Name = dto.Name
	current.Description = dto.Description
	current.EmbedProviderID = dto.EmbedProvider
	current.ChatProviderID = dto.ChatProvider
	current.RerankProviderID = dto.RerankProvider
	current.IndexMode = dto.IndexMode
	current.ChunkSize = dto.ChunkSize
	current.ChunkOverlap = dto.ChunkOverlap
	if err := h.DB.UpdateKB(current); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := h.DB.GetKB(id)
	counts, _ := h.DB.MapKBStatusCounts()
	writeJSON(w, http.StatusOK, kbToDTO(updated, counts[updated.ID]))
}

func (h *KBHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_ = h.DB.DropVecTable(store.SanitizeTableName(id))
	_ = h.DB.DropFTSTable(store.FTSTableName(id))
	if err := h.DB.DeleteKB(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *KBHandler) upload(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "id")
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing file")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	docID := "doc_" + ulid.Make().String()
	doc := store.Document{
		ID: docID, KBID: kbID, Filename: header.Filename, FileSize: header.Size,
		MimeType: header.Header.Get("Content-Type"), Status: "processing", CreatedAt: now,
	}
	rawPath, err := h.saveUpload(kbID, docID, header.Filename, raw)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	doc.RawPath = rawPath
	if err := h.DB.CreateDocument(doc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	go h.ingestDocument(kbID, doc, raw, 0)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"document_id": docID, "status": "processing",
	})
}

func (h *KBHandler) listDocs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	docs, err := h.DB.ListDocuments(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]documentDTO, len(docs))
	for i, d := range docs {
		out[i] = toDocumentDTO(d)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *KBHandler) deleteDoc(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "doc_id")
	doc, _ := h.DB.GetDocument(docID)
	_ = h.clearDocIndex(doc.KBID, docID)
	if err := h.DB.DeleteDocument(docID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if doc.RawPath != "" {
		_ = os.Remove(doc.RawPath)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *KBHandler) retryDoc(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "id")
	docID := chi.URLParam(r, "doc_id")
	doc, err := h.DB.GetDocument(docID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	raw, err := os.ReadFile(doc.RawPath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "raw file unavailable: "+err.Error())
		return
	}
	_ = h.DB.SetDocumentStatus(docID, "processing", 0, "")
	_ = h.DB.SetDocumentProgress(docID, 0, 0)
	go h.ingestDocument(kbID, doc, raw, 0)
	writeJSON(w, http.StatusAccepted, map[string]any{"document_id": docID, "status": "processing"})
}

// pauseDoc 暂停入库：标记 paused + 取消正在跑的 ingest goroutine（保留已写进度）。
func (h *KBHandler) pauseDoc(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "doc_id")
	_ = h.DB.SetDocumentStatus(docID, "paused", 0, "")
	if c, ok := h.ingestCancel.Load(docID); ok {
		if cancel, ok := c.(context.CancelFunc); ok {
			cancel()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"document_id": docID, "status": "paused"})
}

// resumeDoc 继续入库：从已写进度(chunk_done)续传。
func (h *KBHandler) resumeDoc(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "id")
	docID := chi.URLParam(r, "doc_id")
	doc, err := h.DB.GetDocument(docID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	raw, err := os.ReadFile(doc.RawPath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "raw file unavailable: "+err.Error())
		return
	}
	_ = h.DB.SetDocumentStatus(docID, "processing", 0, "")
	go h.ingestDocument(kbID, doc, raw, doc.ChunkDone)
	writeJSON(w, http.StatusOK, map[string]any{"document_id": docID, "status": "processing"})
}

func (h *KBHandler) listChunks(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "doc_id")
	chunks, err := h.DB.ListChunksByDoc(docID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]chunkDTO, len(chunks))
	for i, c := range chunks {
		out[i] = toChunkDTO(c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *KBHandler) docStatus(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "doc_id")
	doc, err := h.DB.GetDocument(docID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": doc.Status, "chunk_count": doc.ChunkCount, "error": doc.Error,
	})
}

func (h *KBHandler) chunkPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text         string `json:"text"`
		ChunkSize    int    `json:"chunk_size"`
		ChunkOverlap int    `json:"chunk_overlap"`
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		req.Text = r.FormValue("text")
		if req.Text == "" {
			text, err := parsePreviewFile(r)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "missing text or file")
				return
			}
			req.Text = text
		}
		req.ChunkSize, _ = strconv.Atoi(r.FormValue("chunk_size"))
		req.ChunkOverlap, _ = strconv.Atoi(r.FormValue("chunk_overlap"))
	} else {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	chunks := rag.Chunk(req.Text, req.ChunkSize, req.ChunkOverlap)
	out := make([]chunkDTO, len(chunks))
	for i, text := range chunks {
		out[i] = chunkDTO{Ordinal: i, Text: text}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *KBHandler) retrieve(w http.ResponseWriter, r *http.Request) {
	kbID := chi.URLParam(r, "id")
	var req struct {
		Query string `json:"query"`
		TopK  int    `json:"top_k"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	hits, err := h.search(ctx, kbID, req.Query, req.TopK)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func pickParser(filename, mime string) parser.Parser {
	candidates := []parser.Parser{parser.DOCX{}, parser.XLSX{}, parser.PDF{}, parser.Markdown{}, parser.Txt{}}
	for _, c := range candidates {
		if c.Supports(mime, filename) {
			return c
		}
	}
	return parser.Txt{}
}

func (h *KBHandler) ingestDocument(kbID string, doc store.Document, raw []byte, resumeFrom int) {
	// ingest runs in a goroutine; a panic here must still flip the document
	// to "failed", otherwise it stays "processing" forever with no clue.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ingest: doc=%s PANIC: %v", doc.ID, r)
			_ = h.DB.SetDocumentStatus(doc.ID, "failed", 0, fmt.Sprintf("panic: %v", r))
		}
	}()
	unlock := h.lockDoc(doc.ID)
	defer unlock()

	// 恢复场景：调用方传的 resumeFrom 可能是过期快照（retry/并发已改 chunk_done），
	// 锁内重读最新值，避免续传跳写丢叶子（TOCTOU）。
	if resumeFrom > 0 {
		if fresh, err := h.DB.GetDocument(doc.ID); err == nil {
			resumeFrom = fresh.ChunkDone
		}
	}

	// 全新/retry(resumeFrom==0)：清残留 + 内容去重；恢复(resumeFrom>0)跳过（文档已过这关）。
	if resumeFrom == 0 {
		_ = h.clearDocIndex(kbID, doc.ID)
		hash := sha256Hex(raw)
		_ = h.DB.SetDocumentHash(doc.ID, hash)
		if dupID, _ := h.DB.FindDuplicateDoc(kbID, hash, doc.ID); dupID != "" {
			log.Printf("ingest: doc=%s skipped: duplicate of %s", doc.ID, dupID)
			_ = h.DB.SetDocumentStatus(doc.ID, "duplicate", 0, "内容与已有文档重复，已跳过入库")
			return
		}
	}
	kb, _ := h.DB.GetKB(kbID)
	embedClient, embedModel := h.embedClientForKB(kb)
	if embedClient == nil {
		log.Printf("ingest: doc=%s skipped: no embed provider configured", doc.ID)
		_ = h.DB.SetDocumentStatus(doc.ID, "failed", 0, "no embed provider configured")
		return
	}
	chatClient, chatModel := h.visionClientForKB(kb) // KB chat provider：VLM 图片描述 + QA/摘要生成
	ing := &rag.Ingestor{
		DB: h.DB, Embed: embedClient, KBID: kbID, EmbedModel: embedModel,
		Chat: chatClient, IndexMode: kb.IndexMode,
		ChunkSz: kb.ChunkSize, Overlap: kb.ChunkOverlap,
		Parser: pickParser(doc.Filename, doc.MimeType),
		Vision: chatClient, VisionModel: chatModel,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	h.ingestCancel.Store(doc.ID, context.CancelFunc(cancel))
	defer func() {
		h.ingestCancel.Delete(doc.ID)
		cancel()
	}()
	if err := ing.IngestFile(ctx, doc.ID, doc.Filename, doc.MimeType, raw, resumeFrom); err != nil {
		log.Printf("ingest: doc=%s ingest failed: %v", doc.ID, err)
	}
}

// lockDoc 返回解锁函数；同一 docID 的 ingestDocument 串行执行（upload/恢复/retry 互斥）。
func (h *KBHandler) lockDoc(docID string) func() {
	actual, _ := h.ingestMu.LoadOrStore(docID, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// RecoverProcessing 恢复所有 status=processing 的文档入库（core 启动时调用）。
// 读 raw_path 续传；raw 缺失则标 failed。最多 4 并发。
func (h *KBHandler) RecoverProcessing() {
	docs, err := h.DB.ListProcessingDocuments()
	if err != nil {
		log.Printf("recover: list processing docs failed: %v", err)
		return
	}
	if len(docs) == 0 {
		return
	}
	log.Printf("recover: resuming %d processing documents", len(docs))
	sem := make(chan struct{}, 4)
	for _, doc := range docs {
		if doc.RawPath == "" {
			_ = h.DB.SetDocumentStatus(doc.ID, "failed", 0, "raw_path 缺失，无法续传")
			continue
		}
		raw, err := os.ReadFile(doc.RawPath)
		if err != nil {
			_ = h.DB.SetDocumentStatus(doc.ID, "failed", 0, "读取 raw 失败: "+err.Error())
			continue
		}
		doc := doc
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			h.ingestDocument(doc.KBID, doc, raw, doc.ChunkDone)
		}()
	}
}

func (h *KBHandler) embedClientForKB(kb store.KnowledgeBase) (llm.LLMClient, string) {
	if kb.EmbedProviderID != "" {
		if p, err := h.DB.GetProvider(kb.EmbedProviderID); err == nil && p.EmbedModel != "" {
			return llm.NewOpenAIClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.EmbedModel,
			}), p.EmbedModel
		}
	}
	return h.EmbedClient, h.EmbedModel
}

// visionClientForKB 返回 KB 绑定的 chat（含 VL）模型客户端 + 模型名，用于把文档图片
// 描述成文字（VLM）。没配 chat_provider 时返回 (nil, "")（图片跳过）。
func (h *KBHandler) visionClientForKB(kb store.KnowledgeBase) (llm.LLMClient, string) {
	if kb.ChatProviderID != "" {
		if p, err := h.DB.GetProvider(kb.ChatProviderID); err == nil && p.ChatModel != "" {
			return llm.NewOpenAIClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.ChatModel,
			}), p.ChatModel
		}
	}
	return nil, ""
}

// sha256Hex 返回字节切片的 SHA-256 十六进制摘要，用于文档内容去重。
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (h *KBHandler) saveUpload(kbID, docID, filename string, raw []byte) (string, error) {
	dir := h.UploadDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "agent-rust", "uploads")
	}
	dir = filepath.Join(dir, kbID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := docID + "-" + filepath.Base(filename)
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, raw, 0o644)
}

func (h *KBHandler) clearDocIndex(kbID, docID string) error {
	chunks, err := h.DB.ListChunksByDoc(docID)
	if err != nil {
		return err
	}
	ids := make([]string, len(chunks))
	for i, c := range chunks {
		ids[i] = c.ID
	}
	_ = h.DB.DeleteVectorsByChunkIDs(store.SanitizeTableName(kbID), ids)
	_ = h.DB.DeleteFTSByChunkIDs(store.FTSTableName(kbID), ids)
	return h.DB.DeleteChunksByDoc(docID)
}

func (h *KBHandler) search(ctx context.Context, kbID, query string, topK int) ([]retrieveHitDTO, error) {
	type searcher interface {
		Search(context.Context, string, string, int) ([]rag.SearchHit, error)
	}
	if s, ok := h.RAG.(searcher); ok {
		hits, err := s.Search(ctx, kbID, query, topK)
		if err != nil {
			return nil, err
		}
		out := make([]retrieveHitDTO, len(hits))
		for i, h := range hits {
			out[i] = retrieveHitDTO{
				ChunkID: h.ChunkID, DocumentID: h.DocumentID, Filename: h.Filename,
				Ordinal: h.Ordinal, Text: h.Text, Similarity: h.Score,
			}
		}
		return out, nil
	}
	if h.RAG != nil {
		chunks, err := h.RAG.Retrieve(ctx, kbID, query, topK)
		if err != nil {
			return nil, err
		}
		out := make([]retrieveHitDTO, len(chunks))
		for i, c := range chunks {
			ordinal := 0
			if stored, err := h.DB.GetChunksByIDs([]string{c.ID}); err == nil && len(stored) > 0 {
				ordinal = stored[0].Ordinal
			}
			out[i] = retrieveHitDTO{
				ChunkID: c.ID, DocumentID: c.DocID, Filename: c.Filename,
				Ordinal: ordinal, Text: c.Text, Similarity: rag.CosineSimilarity(0),
			}
		}
		return out, nil
	}
	return nil, nil
}

func toDocumentDTO(d store.Document) documentDTO {
	return documentDTO{
		ID: d.ID, KBID: d.KBID, Filename: d.Filename, FileSize: d.FileSize,
		MimeType: d.MimeType, Status: d.Status, ChunkCount: d.ChunkCount,
		ChunkDone: d.ChunkDone, ChunkTotal: d.ChunkTotal,
		Error: d.Error, RawPath: d.RawPath, CreatedAt: d.CreatedAt,
	}
}

func toChunkDTO(c store.Chunk) chunkDTO {
	return chunkDTO{
		ID: c.ID, DocumentID: c.DocID, KBID: c.KBID, Ordinal: c.Ordinal,
		Text: c.Text, TokenCount: c.TokenCount, Metadata: c.Metadata,
	}
}

func parsePreviewFile(r *http.Request) (string, error) {
	file, _, err := r.FormFile("file")
	if err != nil {
		return "", err
	}
	defer file.Close()
	var buf bytes.Buffer
	_, err = io.Copy(&buf, file)
	return buf.String(), err
}
