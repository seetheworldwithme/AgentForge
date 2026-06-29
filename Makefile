# Build tags:
# - sqlite_load_extension: REQUIRED to enable vec0 loadable extension support in
#   mattn/go-sqlite3. Without it, vector ops fail at runtime with "no such
#   module: vec0".
# - fts5: REQUIRED to enable FTS5 (full-text search) for RAG hybrid retrieval.
#   Without it, FTS5 table creation fails with "no such module: fts5".
# Always run builds/tests through these targets.

BUILD_TAGS = sqlite_load_extension fts5

.PHONY: build run dev test test-vet tidy

build:
	go build -tags "$(BUILD_TAGS)" ./...

run:
	go run -tags "$(BUILD_TAGS)" ./cmd/core

# `make dev` launches the Wails desktop client in dev mode: the Go backend
# hot-reloads, the frontend runs through vite HMR, and a real WebView window
# opens. No exe is produced. Requires the wails CLI
# (go install github.com/wailsapp/wails/v2/cmd/wails@latest).
dev:
	wails dev -tags "$(BUILD_TAGS)"

test:
	go test -tags "$(BUILD_TAGS)" ./...

tidy:
	go mod tidy
