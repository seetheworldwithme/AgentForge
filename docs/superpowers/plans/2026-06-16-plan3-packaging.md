# Plan 3: Packaging & Cross-Platform Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce distributable native installers for Windows, macOS, and Linux from the Wails app, with the sqlite-vec CGO extension correctly embedded per-platform, and document the build/release workflow.

**Architecture:** Wails v2's build pipeline drives Go compilation with `CGO_ENABLED=1`, embeds the built React assets, and wraps the binary into platform-native installers. The sqlite-vec loadable extension (`.dll`/`.dylib`/`.so`) ships next to the binary and is located via `internal/store.vecExtPath()` (from Plan 1, Task 3). A GitHub Actions matrix builds all three platforms on tag push.

**Tech Stack:** Wails v2 (`wails build`), platform C toolchains (MinGW/Xcode/gcc), GitHub Actions matrix, sqlite-vec prebuilt extensions.

**Prerequisite:** Plans 1 & 2 complete — app runs in `wails dev` and the CGO+sqlite-vec path is validated on at least one platform.

**Reference spec:** `docs/superpowers/specs/2026-06-16-agent-client-design.md` (§10 release, §11.1 CGO risk)

---

## File Structure

```
agent-rust/
├── ext/                             # sqlite-vec prebuilt extensions (committed)
│   ├── windows/sqlite-vec.dll
│   ├── darwin/sqlite-vec.dylib
│   └── linux/sqlite-vec.so
├── build/                           # wails build output (gitignored)
│   └── bin/
├── scripts/
│   ├── fetch-vec.sh                 # download platform extensions
│   └── package.sh                   # local packaging wrapper
├── .github/
│   └── workflows/
│       └── release.yml              # CI matrix build on tag
├── .gitignore                       # add build/, dist/
└── README.md                        # build instructions
```

---

## Task 1: Vendor sqlite-vec Extensions + Fetch Script

**Files:**
- Create: `scripts/fetch-vec.sh`
- Create: `ext/.gitkeep` (and the three platform subdirs)

- [ ] **Step 1: Create the fetch script**

`scripts/fetch-vec.sh`:
```bash
#!/usr/bin/env bash
# Downloads sqlite-vec loadable extensions for all three platforms.
# Run from repo root: bash scripts/fetch-vec.sh
set -euo pipefail

# Pin a version. Check https://github.com/asg0171/sqlite-vec/releases for latest.
VER="v0.1.7-alpha.2"
BASE="https://github.com/asg0171/sqlite-vec/releases/download/${VER}"

mkdir -p ext/windows ext/darwin ext/linux

echo "Downloading sqlite-vec ${VER}…"
# Filenames follow sqlite-vec's release naming; adjust if the pattern changes.
curl -L -o ext/windows/sqlite-vec.dll   "${BASE}/sqlite-vec-0.1.7-alpha.2-loadable-windows-x86_64.zip" && \
  unzip -o -j ext/windows/sqlite-vec.dll '*.dll' -d ext/windows/ && rm ext/windows/sqlite-vec.dll && \
  mv ext/windows/*.dll ext/windows/sqlite-vec.dll

curl -L -o /tmp/dy.zip "${BASE}/sqlite-vec-0.1.7-alpha.2-loadable-macos-universal.zip" && \
  unzip -o -j /tmp/dy.zip '*.dylib' -d ext/darwin/ && mv ext/darwin/*.dylib ext/darwin/sqlite-vec.dylib

curl -L -o /tmp/so.zip "${BASE}/sqlite-vec-0.1.7-alpha.2-loadable-linux-x86_64.zip" && \
  unzip -o -j /tmp/so.zip '*.so' -d ext/linux/ && mv ext/linux/*.so ext/linux/sqlite-vec.so

echo "Done. Files:"
ls -la ext/windows ext/darwin ext/linux
```

> NOTE: The exact release asset names and whether they're zipped vary by sqlite-vec version. Open the release page in a browser, confirm the filenames for the pinned `VER`, and adjust the curl/unzip lines. The principle is: end with `ext/<os>/sqlite-vec.<ext>`.

- [ ] **Step 2: Run the fetch and verify**

Run (requires curl + unzip; on Windows use Git Bash or WSL):
```bash
bash scripts/fetch-vec.sh
```
Expected: three files exist:
```
ext/windows/sqlite-vec.dll
ext/darwin/sqlite-vec.dylib
ext/linux/sqlite-vec.so
```

- [ ] **Step 3: Commit the extensions**

sqlite-vec is MIT-licensed; committing the binaries avoids runtime downloads in CI.
```bash
git add scripts/fetch-vec.sh ext
git commit -m "build: vendor sqlite-vec loadable extensions for win/mac/linux"
```

---

## Task 2: Copy Extensions Next to the Binary at Build Time

`internal/store.vecExtPath()` (Plan 1) looks for the extension at `<exe_dir>/ext/<os>/sqlite-vec.<ext>`. Wails builds the binary into `build/bin/`, so the extensions must be copied there post-build.

**Files:**
- Create: `scripts/package.sh`
- Modify: `wails.json` (post-build hook)

- [ ] **Step 1: Package script**

`scripts/package.sh`:
```bash
#!/usr/bin/env bash
# Wraps `wails build` and copies the platform's sqlite-vec extension next
# to the produced binary so vecExtPath() finds it at runtime.
set -euo pipefail

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  msys*|mingw*|cygwin*|windows*) EXT="windows"; LIB="sqlite-vec.dll" ;;
  darwin)                        EXT="darwin";  LIB="sqlite-vec.dylib" ;;
  linux)                         EXT="linux";   LIB="sqlite-vec.so" ;;
  *) echo "unsupported OS: $OS"; exit 1 ;;
esac

echo "Building for $EXT …"
wails build -clean

# Wails emits build/bin/<name>(.exe)?
BIN_DIR="build/bin"
mkdir -p "$BIN_DIR"
cp "ext/$EXT/$LIB" "$BIN_DIR/$LIB"
echo "Copied ext/$EXT/$LIB -> $BIN_DIR/$LIB"
ls -la "$BIN_DIR"
```

- [ ] **Step 2: Make it executable / runnable**

On Windows, run via Git Bash:
```bash
bash scripts/package.sh
```

- [ ] **Step 3: Run a local package + smoke test**

Run:
```bash
bash scripts/package.sh
```
Expected: binary + the platform `.dll`/`.dylib`/`.so` sit together in `build/bin/`. Launch the binary, create a KB, upload a document → status reaches `ready` (proves sqlite-vec loaded in the packaged binary, not just `go run`).

- [ ] **Step 4: Commit**

```bash
git add scripts/package.sh
git commit -m "build: package script copies sqlite-vec next to binary"
```

---

## Task 3: .gitignore + README Build Docs

**Files:**
- Create: `.gitignore`
- Create: `README.md` (or extend if exists)

- [ ] **Step 1: .gitignore**

`.gitignore`:
```
# build artifacts
/build/
/dist/
*.exe
*.dll
*.dylib
*.so

# but KEEP vendored sqlite-vec extensions
!ext/windows/*.dll
!ext/darwin/*.dylib
!ext/linux/*.so

# go
/vendor/

# node
/frontend/node_modules/
/frontend/dist/

# local data
*.db
*.db-journal
*.db-wal
*.db-shm
port.lock

# IDE
/.idea/
/.vscode/
```

- [ ] **Step 2: README build section**

`README.md`:
```markdown
# Agent

A local-first AI agent desktop app (LLM chat + RAG + local tool execution).

## Prerequisites
- Go 1.26+ with CGO enabled (a C compiler: MinGW on Windows, Xcode CLT on macOS, gcc on Linux)
- Node.js 18+
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

## Development
```bash
# terminal 1 — run the core
go run ./cmd/core

# terminal 2 — run the frontend with vite (proxy to core)
cd frontend
CORE_PORT=<port from terminal 1> npm run dev

# or run the full Wails app
wails dev
```

## Build a native package
```bash
bash scripts/fetch-vec.sh   # one-time: vendor sqlite-vec extensions
bash scripts/package.sh     # builds + copies extension next to binary
```
The installer/binary appears in `build/bin/`.

## Data location
- Windows: `%APPDATA%\agent-rust\`
- macOS:   `~/Library/Application Support/agent-rust/`
- Linux:   `~/.config/agent-rust/`

## Architecture
See `docs/superpowers/specs/2026-06-16-agent-client-design.md` for the full design.
```

- [ ] **Step 3: Commit**

```bash
git add .gitignore README.md
git commit -m "docs: build instructions and gitignore"
```

---

## Task 4: GitHub Actions Release Workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: The matrix workflow**

`.github/workflows/release.yml`:
```yaml
name: release

on:
  push:
    tags: ['v*']
  workflow_dispatch:

jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - os: windows-latest
            platform: windows
            lib: sqlite-vec.dll
            artifact: agent-rust.exe
          - os: macos-latest
            platform: darwin
            lib: sqlite-vec.dylib
            artifact: agent-rust.app
          - os: ubuntu-latest
            platform: linux
            lib: sqlite-vec.so
            artifact: agent-rust
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - uses: actions/setup-node@v4
        with:
          node-version: '20'

      - name: Install Wails CLI
        run: go install github.com/wailsapp/wails/v2/cmd/wails@latest

      - name: Install Linux deps
        if: matrix.platform == 'linux'
        run: |
          sudo apt-get update
          sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev libgcc1

      - name: Build frontend
        run: |
          cd frontend
          npm install
          npm run build

      - name: Wails build
        run: wails build -clean
        env:
          CGO_ENABLED: '1'

      - name: Stage sqlite-vec extension
        run: |
          mkdir -p staging
          cp build/bin/${{ matrix.artifact }} staging/ || true
          cp ext/${{ matrix.platform }}/${{ matrix.lib }} staging/${{ matrix.lib }}
        shell: bash

      - name: Zip (windows/linux)
        if: matrix.platform != 'darwin'
        run: |
          cd staging
          zip -r ../agent-rust-${{ matrix.platform }}.zip .

      - name: Zip (macos)
        if: matrix.platform == 'darwin'
        run: |
          cd build/bin
          zip -r ../../agent-rust-${{ matrix.platform }}.zip ${{ matrix.artifact }}
          cd ../../
          zip -g agent-rust-${{ matrix.platform }}.zip staging/${{ matrix.lib }}

      - uses: actions/upload-artifact@v4
        with:
          name: agent-rust-${{ matrix.platform }}
          path: agent-rust-${{ matrix.platform }}.zip

      - name: Release
        if: startsWith(github.ref, 'refs/tags/')
        uses: softprops/action-gh-release@v2
        with:
          files: agent-rust-${{ matrix.platform }}.zip
```

> NOTE: macOS code signing & notarization are intentionally omitted (MVP self-distribution). Users open the unsigned app via right-click → Open. Add a signing job before public release if distributing widely.

- [ ] **Step 2: Validate the workflow file**

Run (if `actionlint` available): `actionlint .github/workflows/release.yml`
Otherwise, push a tag to a test repo and watch the Actions tab, or trigger `workflow_dispatch`.

- [ ] **Step 3: Commit**

```bash
git add .github
git commit -m "ci: release workflow — build win/mac/linux on tag push"
```

---

## Task 5: Cross-Platform Validation Checklist + Smoke

- [ ] **Step 1: Windows validation**

On a Windows machine:
```bash
bash scripts/package.sh
build\bin\agent-rust.exe
```
Verify in the app:
- [ ] healthz responds (open devtools, fetch `/healthz`)
- [ ] add provider, create session, chat streams
- [ ] create KB, upload `.txt`, status → `ready`
- [ ] bash tool with confirmation works
- [ ] app data written to `%APPDATA%\agent-rust\app.db`

- [ ] **Step 2: macOS validation**

On a macOS machine (or CI artifact):
- [ ] right-click → Open (unsigned)
- [ ] same functional checks as Windows
- [ ] data at `~/Library/Application Support/agent-rust/`

- [ ] **Step 3: Linux validation**

On Ubuntu:
- [ ] install webkit deps if needed
- [ ] same functional checks
- [ ] data at `~/.config/agent-rust/`

- [ ] **Step 4: Record results**

Append a "Validated Platforms" section to the design doc §11.1, listing OS version, sqlite-vec version, and pass/fail per capability. If any platform's CGO build fails, activate the hnswlib fallback (per spec §11.1) for that platform and note it.

- [ ] **Step 5: Tag the first release**

```bash
git tag v0.1.0
git push origin v0.1.0
```
Expected: GitHub Actions produces three zipped artifacts on the Releases page.

---

## Self-Review

**Spec coverage (release-relevant):**
- §10 single binary + embedded frontend → Wails build ✓
- §10 static SQLite + sqlite-vec → vendored extensions copied next to binary ✓
- §10 no auto-update (MVP) → explicitly deferred ✓
- §11.1 CGO risk → cross-platform validation + hnswlib fallback documented ✓

**Known limitations (acceptable for v0.1):**
1. macOS unsigned — requires manual Open; signing is a future task.
2. Linux requires webkit2gtk system deps; not a true self-contained bundle (AppImage would help — future task).
3. No auto-update; users download new releases manually.
4. `scripts/fetch-vec.sh` filenames are version-pinned and may need adjustment when bumping sqlite-vec.

---

## Execution Handoff

Plan 3 complete and saved to `docs/superpowers/plans/2026-06-16-plan3-packaging.md`.

All three plans now exist:
- `docs/superpowers/plans/2026-06-16-plan1-go-core.md` (17 tasks)
- `docs/superpowers/plans/2026-06-16-plan2-wails-frontend.md` (7 tasks)
- `docs/superpowers/plans/2026-06-16-plan3-packaging.md` (5 tasks)

**Recommended execution order:** Plan 1 → Plan 2 → Plan 3. Each plan produces working, testable software.
