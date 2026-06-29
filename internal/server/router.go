package server

import (
	"net/http"
	"time"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/mcp"
	"github.com/agent-rust/core/internal/memory"
	"github.com/agent-rust/core/internal/rules"
	"github.com/agent-rust/core/internal/skills"
	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/todo"
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
	EmbedModel  string             // 全局默认 embedding 模型名（缓存 key）；KB 绑定的优先
	RAG         agent.RAGRetriever // optional; nil disables chat RAG
	Skills      *skills.Manager    // optional; nil disables skill injection
	MCP         *mcp.Manager       // optional; nil disables MCP tools/settings
	WorkDir     *tools.WorkDir     // optional; shared cwd for filesystem tools
	Memory      *memory.MemoryStore // optional; nil disables memory feature
	Rules       *rules.RulesStore   // optional; nil disables rules feature
	Todo        *todo.Store         // optional; nil disables todo feature
	Asker       *tools.Asker        // optional; nil disables ask_user
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
		// 注入 buildLLM 闭包：手动压缩时按 session 的 provider 现场构造 LLM 客户端，
		// 构造方式与 ChatHandler 保持一致（含 3 次重试）。
		(&SessionHandler{
			DB:      d.DB,
			WorkDir: d.WorkDir,
			buildLLM: func(p store.Provider) llm.LLMClient {
				return llm.NewRetry(llm.NewOpenAIClient(llm.Config{
					BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.ChatModel,
				}), 3, 500*time.Millisecond)
			},
		}).Routes(r)
		// 注意：Memory 是 agent.MemoryProvider 接口，直接传 *memory.MemoryStore 的 nil
		// 会得到「非 nil 接口包裹 nil 指针」，导致 agent.Run 误判已启用。故仅当具体值
		// 非 nil 时才赋值，保持 nil 接口语义。
		chat := &ChatHandler{DB: d.DB, Gate: d.Gate, Engine: d.Engine, MCP: d.MCP, RAG: d.RAG, Skills: skillProvider, MCPConfigPath: mcpConfigPath, WorkDir: d.WorkDir, Asker: d.Asker}
		if d.Memory != nil {
			chat.Memory = d.Memory
		}
		if d.Rules != nil {
			chat.Rules = d.Rules
		}
		if d.Todo != nil {
			chat.Todo = d.Todo
		}
		chat.Routes(r)
		(&ToolsHandler{Gate: d.Gate}).Routes(r)
		(&AskHandler{Asker: d.Asker}).Routes(r)
		(&KBHandler{DB: d.DB, EmbedClient: d.EmbedClient, EmbedModel: d.EmbedModel, RAG: d.RAG, UploadDir: d.UploadDir}).Routes(r)
		(&WorkDirHandler{WorkDir: d.WorkDir}).Routes(r)
		(&TerminalHandler{WorkDir: d.WorkDir}).Routes(r)
		(&SkillsHandler{Manager: d.Skills}).Routes(r)
		(&MCPHandler{Manager: d.MCP}).Routes(r)
		if d.Memory != nil {
			(&MemoryHandler{Store: d.Memory}).Routes(r)
		}
		if d.Rules != nil {
			(&RulesHandler{Store: d.Rules, DB: d.DB}).Routes(r)
		}
		if d.Todo != nil {
			(&TodoHandler{Store: d.Todo}).Routes(r)
		}
	})
	return r
}
