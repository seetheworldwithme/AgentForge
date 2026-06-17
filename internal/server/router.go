package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
)

// Deps bundles everything the router needs.
type Deps struct {
	DB          *store.DB
	Gate        *tools.Gate
	Engine      *tools.Engine
	EmbedClient llm.LLMClient       // used by KB ingest; nil disables upload
	RAG         agent.RAGRetriever  // optional; nil disables chat RAG
	WorkDir     *tools.WorkDir      // optional; shared cwd for filesystem tools
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: false,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	r.Route("/api", func(r chi.Router) {
		(&ConfigHandler{DB: d.DB}).Routes(r)
		(&SessionHandler{DB: d.DB, WorkDir: d.WorkDir}).Routes(r)
		(&ChatHandler{DB: d.DB, Gate: d.Gate, Engine: d.Engine, RAG: d.RAG}).Routes(r)
		(&ToolsHandler{Gate: d.Gate}).Routes(r)
		(&KBHandler{DB: d.DB, EmbedClient: d.EmbedClient}).Routes(r)
		(&WorkDirHandler{WorkDir: d.WorkDir}).Routes(r)
	})
	return r
}
