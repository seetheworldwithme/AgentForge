package server

import (
	"net/http"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/mcp"
	"github.com/agent-rust/core/internal/skills"
	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// Deps bundles everything the router needs.
type Deps struct {
	DB          *store.DB
	Gate        *tools.Gate
	Engine      *tools.Engine
	EmbedClient llm.LLMClient      // used by KB ingest; nil disables upload
	RAG         agent.RAGRetriever // optional; nil disables chat RAG
	Skills      *skills.Manager    // optional; nil disables skill injection
	MCP         *mcp.Manager       // optional; nil disables MCP tools/settings
	WorkDir     *tools.WorkDir     // optional; shared cwd for filesystem tools
	UploadDir   string             // optional; defaults to OS temp dir
}

func NewRouter(d Deps) http.Handler {
	var skillProvider agent.SkillProvider
	if d.Skills != nil {
		skillProvider = d.Skills
	}
	mcpConfigPath := ""
	if d.MCP != nil {
		mcpConfigPath = d.MCP.ConfigPath()
	}

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
		(&ChatHandler{DB: d.DB, Gate: d.Gate, Engine: d.Engine, RAG: d.RAG, Skills: skillProvider, MCPConfigPath: mcpConfigPath}).Routes(r)
		(&ToolsHandler{Gate: d.Gate}).Routes(r)
		(&KBHandler{DB: d.DB, EmbedClient: d.EmbedClient, RAG: d.RAG, UploadDir: d.UploadDir}).Routes(r)
		(&WorkDirHandler{WorkDir: d.WorkDir}).Routes(r)
		(&SkillsHandler{Manager: d.Skills}).Routes(r)
		(&MCPHandler{Manager: d.MCP}).Routes(r)
	})
	return r
}
