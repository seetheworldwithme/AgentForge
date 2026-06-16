# Build tags: sqlite_load_extension is REQUIRED to enable vec0 loadable extension
# support in mattn/go-sqlite3. Without it, vector operations fail at runtime
# with "no such module: vec0". Always run builds/tests through these targets.

BUILD_TAGS = sqlite_load_extension

.PHONY: build run test test-vet tidy

build:
	go build -tags "$(BUILD_TAGS)" ./...

run:
	go run -tags "$(BUILD_TAGS)" ./cmd/core

test:
	go test -tags "$(BUILD_TAGS)" ./...

tidy:
	go mod tidy
