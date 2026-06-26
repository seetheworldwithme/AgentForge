package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/mcp"
	"github.com/agent-rust/core/internal/rag"
	"github.com/agent-rust/core/internal/rules"
	"github.com/agent-rust/core/internal/server"
	"github.com/agent-rust/core/internal/skills"
	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
	"github.com/agent-rust/core/internal/tools/builtin"
)

func main() {
	dataDir := flag.String("data", defaultDataDir(), "data directory")
	addr := flag.String("addr", "127.0.0.1:7777", "listen address")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("mkdir data: %v", err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "app.db"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	gate := tools.NewGate()
	workDir := tools.NewWorkDir()
	mcpManager := mcp.NewManager(db)
	skillsManager := skills.NewManager(skills.Options{DB: db, WorkDir: workDir.Get})
	rulesStore := rules.New(rules.Options{WorkDir: workDir.Get, DB: db})
	registry := tools.NewRegistry(
		builtin.FileRead{}, builtin.FileWrite{}, builtin.FileEdit{},
		builtin.Grep{}, builtin.Bash{WorkDir: workDir},
		builtin.ReadSkill{Skills: skillsManager},
	)
	// 纯内置工具引擎；MCP 按请求在 ChatHandler 内 attach（支持临时限定 server）。
	baseEngine := tools.NewEngine(registry, gate)

	// Build an embed client + RAG retriever from the default embed provider,
	// if any. These enable KB ingest and chat-time RAG; absent a configured
	// embed model they stay nil and those features are disabled gracefully.
	var embedClient llm.LLMClient
	var ragRetriever agent.RAGRetriever
	if def, err := db.GetDefaultProviderByKind("embed"); err == nil && def.EmbedModel != "" {
		embedClient = llm.NewOpenAIClient(llm.Config{
			BaseURL: def.BaseURL, APIKey: def.APIKey, Model: def.EmbedModel,
		})
		ragRetriever = &rag.Retriever{DB: db, EmbedClient: embedClient}
	}

	router := server.NewRouter(server.Deps{
		DB: db, Gate: gate, Engine: baseEngine,
		EmbedClient: embedClient, RAG: ragRetriever, Skills: skillsManager, MCP: mcpManager, WorkDir: workDir,
		Rules: rulesStore,
		UploadDir: filepath.Join(*dataDir, "uploads"),
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(*dataDir, "port.lock"), []byte(itoa(port)), 0o644); err != nil {
		log.Printf("warn: write port.lock: %v", err)
	}
	log.Printf("core listening on %s (port %d)", ln.Addr(), port)
	log.Fatal(http.Serve(ln, router))
}

func defaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, "agent-rust")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
