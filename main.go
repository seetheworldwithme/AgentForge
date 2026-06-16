// Command desktop launches the agent-rust Wails application. It mirrors
// cmd/core's wiring (store, tools gate/engine, LLM + RAG from the default
// provider, full router) but starts the HTTP server on a random port and
// embeds the built React frontend into a Wails WebView. The frontend talks
// to the in-process core over http://127.0.0.1:<port> + SSE; it does not use
// Wails Bindings except to learn the core port.
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/agent-rust/core/internal/agent"
	"github.com/agent-rust/core/internal/llm"
	"github.com/agent-rust/core/internal/rag"
	"github.com/agent-rust/core/internal/server"
	"github.com/agent-rust/core/internal/store"
	"github.com/agent-rust/core/internal/tools"
	"github.com/agent-rust/core/internal/tools/builtin"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func main() {
	dataDir := flag.String("data", defaultDataDir(), "data directory")
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
	registry := tools.NewRegistry(
		builtin.FileRead{}, builtin.FileWrite{}, builtin.FileEdit{},
		builtin.Grep{}, builtin.Bash{WorkDir: workDir},
	)
	engine := tools.NewEngine(registry, gate)

	// Build an embed client + RAG retriever from the default provider, if any.
	// These enable KB ingest and chat-time RAG; absent a configured provider
	// they stay nil and those features are disabled gracefully.
	var embedClient llm.LLMClient
	var ragRetriever agent.RAGRetriever
	if def, err := db.GetDefaultProvider(); err == nil && def.EmbedModel != "" {
		embedClient = llm.NewOpenAIClient(llm.Config{
			BaseURL: def.BaseURL, APIKey: def.APIKey, Model: def.EmbedModel,
		})
		ragRetriever = &rag.Retriever{DB: db, EmbedClient: embedClient}
	}

	router := server.NewRouter(server.Deps{
		DB: db, Gate: gate, Engine: engine,
		EmbedClient: embedClient, RAG: ragRetriever, WorkDir: workDir,
	})

	// Start the core HTTP server on a random loopback port. The frontend
	// learns the port through the PortBinder binding below (or, in `vite dev`,
	// through the CORE_PORT env var + the vite proxy).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(*dataDir, "port.lock"), []byte(itoa(port)), 0o644); err != nil {
		log.Printf("warn: write port.lock: %v", err)
	}
	log.Printf("core listening on %s (port %d)", ln.Addr(), port)
	go func() { _ = http.Serve(ln, router) }()

	// Serve the embedded frontend. embed.FS is rooted at the repo root, so
	// sub to the dist directory to get an fs.FS the asset server expects.
	frontendFS, err := fs.Sub(frontendAssets, "frontend/dist")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}

	err = wails.Run(&options.App{
		Title:     "Agent",
		Width:     1100,
		Height:    750,
		MinWidth:  700,
		MinHeight: 500,
		AssetServer: &assetserver.Options{
			Assets: frontendFS,
		},
		OnStartup: func(ctx context.Context) {
			dialogBinder.ctx = ctx
		},
		Bind: []interface{}{
			&PortBinder{Port: port},
			dialogBinder,
		},
	})
	if err != nil {
		log.Fatalf("wails: %v", err)
	}
}

// PortBinder exposes the in-process core port to the frontend via a Wails
// binding: window.go.main.PortBinder.GetPort().
type PortBinder struct {
	Port int `json:"port"`
}

func (p *PortBinder) GetPort() int { return p.Port }

// dialogBinder exposes a native directory picker to the frontend via a Wails
// binding: window.go.main.DialogBinder.OpenDirectory(). Returns "" when the
// user cancels. Only available in the Wails (production) build; in dev mode
// the frontend falls back to a text input.
var dialogBinder = &DialogBinder{}

type DialogBinder struct {
	ctx context.Context
}

func (d *DialogBinder) OpenDirectory() (string, error) {
	if d.ctx == nil {
		return "", nil
	}
	return runtime.OpenDirectoryDialog(d.ctx, runtime.OpenDialogOptions{
		Title: "选择工作目录",
	})
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
