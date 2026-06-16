package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/rag"
	"github.com/agent-rust/core/internal/rag/parser"
	"github.com/agent-rust/core/internal/store"
)

type KBHandler struct {
	DB          *store.DB
	EmbedClient llm.LLMClient // for ingest + retrieve (nil until configured)
}

func (h *KBHandler) Routes(r chi.Router) {
	r.Get("/kb", h.list)
	r.Post("/kb", h.create)
	r.Delete("/kb/{id}", h.delete)
	r.Post("/kb/{id}/documents", h.upload)
	r.Get("/kb/{id}/documents", h.listDocs)
	r.Get("/kb/{id}/documents/{doc_id}/status", h.docStatus)
}

type kbDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	EmbedProvider string `json:"embed_provider_id"`
	ChunkSize     int    `json:"chunk_size"`
	ChunkOverlap  int    `json:"chunk_overlap"`
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
			EmbedProvider: k.EmbedProviderID, ChunkSize: k.ChunkSize, ChunkOverlap: k.ChunkOverlap,
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
		EmbedProviderID: dto.EmbedProvider, ChunkSize: dto.ChunkSize, ChunkOverlap: dto.ChunkOverlap,
		CreatedAt: now,
	}
	if err := h.DB.CreateKB(kb); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, kbDTO{
		ID: kb.ID, Name: kb.Name, Description: kb.Description,
		EmbedProvider: kb.EmbedProviderID, ChunkSize: kb.ChunkSize, ChunkOverlap: kb.ChunkOverlap,
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
	kb, _ := h.DB.GetKB(kbID)
	doc := store.Document{
		ID: docID, KBID: kbID, Filename: header.Filename, FileSize: header.Size,
		MimeType: header.Header.Get("Content-Type"), Status: "processing", CreatedAt: now,
	}
	if err := h.DB.CreateDocument(doc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// async ingest (no embed client -> mark failed)
	go func() {
		if h.EmbedClient == nil {
			_ = h.DB.SetDocumentStatus(docID, "failed", 0, "no embed provider configured")
			return
		}
		ing := &rag.Ingestor{
			DB: h.DB, Embed: h.EmbedClient, KBID: kbID,
			ChunkSz: kb.ChunkSize, Overlap: kb.ChunkOverlap,
			Parser: pickParser(doc.Filename, doc.MimeType),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_ = ing.IngestFile(ctx, docID, doc.Filename, doc.MimeType, raw)
	}()

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
	writeJSON(w, http.StatusOK, docs)
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

func pickParser(filename, mime string) parser.Parser {
	candidates := []parser.Parser{parser.Markdown{}, parser.Txt{}}
	for _, c := range candidates {
		if c.Supports(mime, filename) {
			return c
		}
	}
	return parser.Txt{}
}
