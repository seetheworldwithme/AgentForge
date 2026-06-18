package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	DB          *store.DB
	EmbedClient llm.LLMClient // for ingest + retrieve (nil until configured)
	RAG         agent.RAGRetriever
	UploadDir   string
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
	r.Get("/kb/{id}/documents/{doc_id}/chunks", h.listChunks)
	r.Get("/kb/{id}/documents/{doc_id}/status", h.docStatus)
	r.Post("/kb/{id}/chunk-preview", h.chunkPreview)
	r.Post("/kb/{id}/retrieve", h.retrieve)
}

type kbDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	EmbedProvider string `json:"embed_provider_id"`
	ChatProvider  string `json:"chat_provider_id"`
	ChunkSize     int    `json:"chunk_size"`
	ChunkOverlap  int    `json:"chunk_overlap"`
	DocCount      int    `json:"doc_count"`
	CreatedAt     string `json:"created_at"`
}

type documentDTO struct {
	ID         string `json:"id"`
	KBID       string `json:"kb_id"`
	Filename   string `json:"filename"`
	FileSize   int64  `json:"file_size"`
	MimeType   string `json:"mime_type"`
	Status     string `json:"status"`
	ChunkCount int    `json:"chunk_count"`
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
	out := make([]kbDTO, len(kbs))
	for i, k := range kbs {
		out[i] = kbDTO{
			ID: k.ID, Name: k.Name, Description: k.Description,
			EmbedProvider: k.EmbedProviderID, ChatProvider: k.ChatProviderID,
			ChunkSize: k.ChunkSize, ChunkOverlap: k.ChunkOverlap,
			DocCount: k.DocCount, CreatedAt: k.CreatedAt,
		}
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
		ChunkSize: dto.ChunkSize, ChunkOverlap: dto.ChunkOverlap,
		CreatedAt: now,
	}
	if err := h.DB.CreateKB(kb); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, kbDTO{
		ID: kb.ID, Name: kb.Name, Description: kb.Description,
		EmbedProvider: kb.EmbedProviderID, ChatProvider: kb.ChatProviderID,
		ChunkSize: kb.ChunkSize, ChunkOverlap: kb.ChunkOverlap,
		DocCount: kb.DocCount, CreatedAt: kb.CreatedAt,
	})
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
	current.ChunkSize = dto.ChunkSize
	current.ChunkOverlap = dto.ChunkOverlap
	if err := h.DB.UpdateKB(current); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := h.DB.GetKB(id)
	writeJSON(w, http.StatusOK, kbDTO{
		ID: updated.ID, Name: updated.Name, Description: updated.Description,
		EmbedProvider: updated.EmbedProviderID, ChatProvider: updated.ChatProviderID,
		ChunkSize: updated.ChunkSize, ChunkOverlap: updated.ChunkOverlap,
		DocCount: updated.DocCount, CreatedAt: updated.CreatedAt,
	})
}

func (h *KBHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_ = h.DB.DropVecTable(store.SanitizeTableName(id))
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

	go h.ingestDocument(kbID, doc, raw)

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
	_ = h.clearDocIndex(kbID, docID)
	_ = h.DB.SetDocumentStatus(docID, "processing", 0, "")
	go h.ingestDocument(kbID, doc, raw)
	writeJSON(w, http.StatusAccepted, map[string]any{"document_id": docID, "status": "processing"})
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

func (h *KBHandler) ingestDocument(kbID string, doc store.Document, raw []byte) {
	// ingest runs in a goroutine; a panic here must still flip the document
	// to "failed", otherwise it stays "processing" forever with no clue.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ingest: doc=%s PANIC: %v", doc.ID, r)
			_ = h.DB.SetDocumentStatus(doc.ID, "failed", 0, fmt.Sprintf("panic: %v", r))
		}
	}()
	kb, _ := h.DB.GetKB(kbID)
	embedClient := h.embedClientForKB(kb)
	if embedClient == nil {
		log.Printf("ingest: doc=%s skipped: no embed provider configured", doc.ID)
		_ = h.DB.SetDocumentStatus(doc.ID, "failed", 0, "no embed provider configured")
		return
	}
	ing := &rag.Ingestor{
		DB: h.DB, Embed: embedClient, KBID: kbID,
		ChunkSz: kb.ChunkSize, Overlap: kb.ChunkOverlap,
		Parser: pickParser(doc.Filename, doc.MimeType),
		Vision: h.visionClientForKB(kb),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := ing.IngestFile(ctx, doc.ID, doc.Filename, doc.MimeType, raw); err != nil {
		log.Printf("ingest: doc=%s ingest failed: %v", doc.ID, err)
	}
}

func (h *KBHandler) embedClientForKB(kb store.KnowledgeBase) llm.LLMClient {
	if kb.EmbedProviderID != "" {
		if p, err := h.DB.GetProvider(kb.EmbedProviderID); err == nil && p.EmbedModel != "" {
			return llm.NewOpenAIClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.EmbedModel,
			})
		}
	}
	return h.EmbedClient
}

// visionClientForKB 返回 KB 绑定的 chat（含 VL）模型客户端，用于把文档图片
// 描述成文字（VLM）。没配 chat_provider 时返回 nil（图片跳过）。
func (h *KBHandler) visionClientForKB(kb store.KnowledgeBase) llm.LLMClient {
	if kb.ChatProviderID != "" {
		if p, err := h.DB.GetProvider(kb.ChatProviderID); err == nil && p.ChatModel != "" {
			return llm.NewOpenAIClient(llm.Config{
				BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.ChatModel,
			})
		}
	}
	return nil
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
				Ordinal: h.Ordinal, Text: h.Text, Similarity: rag.CosineSimilarity(h.Distance),
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
