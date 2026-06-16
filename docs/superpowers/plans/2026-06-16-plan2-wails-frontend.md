# Plan 2: Wails Desktop Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Wails desktop application that embeds the Go core service and provides a React UI for chat, RAG knowledge bases, tool confirmation, and provider configuration.

**Architecture:** A Wails v2 app where `cmd/desktop/main.go` (1) starts the Go core HTTP server in-process on a random port, (2) writes the port for itself, then (3) loads the React frontend (built into `frontend/dist`) in a WebView. The frontend is a plain React SPA that talks to the core over `http://127.0.0.1:<port>` + SSE — it does **not** use Wails Bindings, keeping the frontend portable and dev-friendly (`vite dev` can point at a running core).

**Tech Stack:** Wails v2, React 18 + Vite + TypeScript, zustand (state), Tailwind CSS + shadcn/ui (UI), native EventSource (SSE).

**Prerequisite:** Plan 1 complete — core service runs and exposes `/api/*`.

**Reference spec:** `docs/superpowers/specs/2026-06-16-agent-client-design.md`

---

## File Structure

```
agent-rust/
├── cmd/
│   └── desktop/
│       └── main.go                 # Wails app: start core, load WebView
├── wails.json
├── frontend/
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   ├── tailwind.config.js
│   ├── postcss.config.js
│   ├── index.html
│   └── src/
│       ├── main.tsx
│       ├── App.tsx
│       ├── lib/
│       │   ├── api.ts              # fetch wrappers for /api/*
│       │   ├── sse.ts              # SSE client for chat + /events
│       │   └── port.ts             # read core port (dev: env; prod: injected)
│       ├── stores/
│       │   ├── sessionStore.ts     # zustand: sessions + messages
│       │   ├── configStore.ts      # providers + settings
│       │   ├── kbStore.ts          # knowledge bases + documents
│       │   └── confirmStore.ts     # pending tool confirmations
│       ├── components/
│       │   ├── Sidebar.tsx         # session list + new chat
│       │   ├── ChatView.tsx        # message list + input
│       │   ├── MessageBubble.tsx   # renders user/assistant/tool msgs
│       │   ├── ChatInput.tsx       # textarea + send + tool/rag toggles
│       │   ├── ConfirmDialog.tsx   # modal for tool confirm_req
│       │   ├── ProviderSettings.tsx# provider CRUD form
│       │   ├── KBManager.tsx       # KB list + upload
│       │   └── SettingsModal.tsx   # wraps ProviderSettings
│       └── types.ts                # DTOs matching core
└── (Plan 1's internal/ stays untouched)
```

**Boundaries:** `lib/` owns all network I/O; `stores/` own state and call `lib/`; `components/` are pure presentation that read/write stores. No fetch calls inside components.

---

## Task 1: Scaffold Wails App

**Files:**
- Create: `wails.json`
- Create: `cmd/desktop/main.go`
- Modify: `go.mod` (add wails)

- [ ] **Step 1: Install Wails CLI**

Run:
```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```
Verify: `wails version`. (Requires Node.js + a C compiler — already needed for Plan 1's CGO.)

- [ ] **Step 2: Initialize the frontend scaffold manually (do NOT use `wails init` to avoid clobbering go.mod)**

`frontend/package.json`:
```json
{
  "name": "agent-rust-frontend",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "zustand": "^4.5.5"
  },
  "devDependencies": {
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.49",
    "tailwindcss": "^3.4.17",
    "typescript": "^5.6.3",
    "vite": "^5.4.11"
  }
}
```

`frontend/vite.config.ts`:
```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // dev proxy to a running core (CORE_PORT env)
    proxy: {
      '/api': { target: `http://127.0.0.1:${process.env.CORE_PORT ?? 0}`, changeOrigin: true },
      '/events': { target: `http://127.0.0.1:${process.env.CORE_PORT ?? 0}`, changeOrigin: true },
      '/healthz': { target: `http://127.0.0.1:${process.env.CORE_PORT ?? 0}`, changeOrigin: true },
    },
  },
  build: { outDir: 'dist' },
})
```

`frontend/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true
  },
  "include": ["src"]
}
```

`frontend/index.html`:
```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Agent</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 3: Tailwind setup**

`frontend/tailwind.config.js`:
```js
/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: { extend: {} },
  plugins: [],
}
```

`frontend/postcss.config.js`:
```js
export default {
  plugins: { tailwindcss: {}, autoprefixer: {} },
}
```

`frontend/src/index.css`:
```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

- [ ] **Step 4: Wails config**

`wails.json`:
```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "agent-rust",
  "outputfilename": "agent-rust",
  "frontend:install": "npm install",
  "frontend:build": "npm run build",
  "frontend:dev:watcher": "npm run dev",
  "frontend:dev:serverUrl": "auto",
  "author": { "name": "" },
  "info": { "productName": "Agent", "productVersion": "0.1.0" }
}
```

- [ ] **Step 5: Minimal entry stubs**

`frontend/src/main.tsx`:
```tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode><App /></React.StrictMode>,
)
```

`frontend/src/App.tsx` (placeholder, expanded later):
```tsx
export default function App() {
  return <div className="p-4">Agent — loading</div>
}
```

- [ ] **Step 6: Install deps + verify build**

Run:
```bash
cd frontend
npm install
npm run build
cd ..
```
Expected: `frontend/dist/index.html` produced.

- [ ] **Step 7: Commit**

```bash
git add frontend wails.json
git commit -m "feat(frontend): scaffold React+Vite+Tailwind frontend"
```

---

## Task 2: Wails main.go — Start Core, Serve Frontend

**Files:**
- Create: `cmd/desktop/main.go`

- [ ] **Step 1: Add Wails dependency**

Run:
```bash
go get github.com/wailsapp/wails/v2@latest
```

- [ ] **Step 2: Implement desktop main.go**

This reuses Plan 1's `server.NewRouter` + `store.Open` + `tools` setup, starts the HTTP listener on a random port, and loads the built frontend assets.

`cmd/desktop/main.go`:
```go
package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/yourname/agent-rust/internal/llm"
	"github.com/yourname/agent-rust/internal/server"
	"github.com/yourname/agent-rust/internal/store"
	"github.com/yourname/agent-rust/internal/tools"
	"github.com/yourname/agent-rust/internal/tools/builtin"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func main() {
	dataDir := flag.String("data", defaultDataDir(), "data directory")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "app.db"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	gate := tools.NewGate()
	registry := tools.NewRegistry(
		builtin.FileRead{}, builtin.FileWrite{}, builtin.FileEdit{},
		builtin.Grep{}, builtin.Bash{},
	)
	engine := tools.NewEngine(registry, gate)

	// embed client built from default provider (lazy; nil until configured)
	var embedClient llm.LLMClient
	if def, err := db.GetDefaultProvider(); err == nil && def.EmbedModel != "" {
		embedClient = llm.NewOpenAIClient(llm.Config{
			BaseURL: def.BaseURL, APIKey: def.APIKey, Model: def.EmbedModel,
		})
	}

	router := server.NewRouter(server.Deps{
		DB: db, Gate: gate, Engine: engine, EmbedClient: embedClient,
	})

	// start core on random port, save it for the frontend to read
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = os.WriteFile(filepath.Join(*dataDir, "port.lock"), []byte(itoa(port)), 0o644)
	go func() { _ = http.Serve(ln, router) }()

	// pass port into the frontend via startup hook (injected as window.__CORE_PORT)
	err = wails.Run(&options.App{
		Options: options.Options{
			AssetServer: &assetserver.Options{Assets: http.FS(frontendAssets)},
			OnStartup: func(ctx context.Context) {
				// store port where JS can read via a binding; simplest: env-like global
				_ = ctx
			},
			Bind: []interface{}{&PortBinder{Port: port}},
		},
	})
	if err != nil {
		log.Fatalf("wails: %v", err)
	}
}

// PortBinder exposes the core port to JS via Wails binding.
type PortBinder struct {
	Port int `json:"port"`
}

func (p *PortBinder) GetPort() int { return p.Port }

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
```

> NOTE: The frontend reads the port via the Wails binding `PortBinder.GetPort()` (called through `window.go.main.PortBinder.GetPort()`). In `vite dev` mode, the port comes from `process.env.CORE_PORT` and the proxy handles routing. This dual-mode is implemented in `lib/port.ts` (Task 3).

- [ ] **Step 3: Build the desktop app**

Run:
```bash
wails build
```
Expected: a binary in `build/bin/` (e.g., `agent-rust.exe`). Launching it shows the WebView with "Agent — loading".

- [ ] **Step 4: Commit**

```bash
git add cmd/desktop go.mod go.sum
git commit -m "feat(desktop): Wails entrypoint starts core and serves frontend"
```

---

## Task 3: Frontend Core Lib — Port, API, SSE

**Files:**
- Create: `frontend/src/lib/port.ts`
- Create: `frontend/src/lib/api.ts`
- Create: `frontend/src/lib/sse.ts`
- Create: `frontend/src/types.ts`

- [ ] **Step 1: Define DTO types**

`frontend/src/types.ts`:
```ts
export interface Provider {
  id: string; name: string; base_url: string; api_key: string;
  chat_model: string; embed_model?: string; is_default: boolean;
}
export interface Session {
  id: string; title: string; provider_id: string;
  kb_id?: string; tools_enabled: boolean;
}
export interface Message {
  id: string; session_id: string; role: 'user'|'assistant'|'tool'|'system';
  content: string; tool_calls?: string; tool_call_id?: string;
  citations?: string; tokens_in?: number; tokens_out?: number;
}
export interface KnowledgeBase {
  id: string; name: string; description?: string;
  embed_provider_id: string; chunk_size: number; chunk_overlap: number;
}
export interface Document {
  id: string; kb_id: string; filename: string; status: string;
  chunk_count: number; error?: string;
}
export interface ChatEvent {
  event: 'started'|'delta'|'tool_call'|'confirm_req'|'tool_result'|'error'|'done';
  data: any;
}
```

- [ ] **Step 2: Port resolver**

`frontend/src/lib/port.ts`:
```ts
let cached: number | null = null;

export async function getCorePort(): Promise<number> {
  if (cached) return cached;
  // Production: Wails binding injected on window.go
  const w = window as any;
  if (w.go?.main?.PortBinder?.GetPort) {
    cached = await w.go.main.PortBinder.GetPort();
    return cached!;
  }
  // Dev: from vite env
  cached = Number(import.meta.env.CORE_PORT) || 0;
  return cached;
}

export async function baseUrl(): Promise<string> {
  const port = await getCorePort();
  // In dev with vite proxy, requests go through same origin (no port needed)
  if (!port) return '';
  return `http://127.0.0.1:${port}`;
}
```

- [ ] **Step 3: API wrappers**

`frontend/src/lib/api.ts`:
```ts
import { baseUrl } from './port';
import type { Provider, Session, Message, KnowledgeBase, Document } from '../types';

async function jget(path: string) {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`);
  if (!r.ok) throw new Error(`${path} ${r.status}`);
  return r.json();
}
async function jpost(path: string, body: any) {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(`${path} ${r.status}`);
  return r.json();
}
async function jdel(path: string) {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`, { method: 'DELETE' });
  if (!r.ok) throw new Error(`${path} ${r.status}`);
}

export const api = {
  listProviders: () => jget('/api/providers') as Promise<Provider[]>,
  createProvider: (p: Omit<Provider,'id'>) => jpost('/api/providers', p),
  deleteProvider: (id: string) => jdel(`/api/providers/${id}`),

  listSessions: () => jget('/api/sessions') as Promise<Session[]>,
  createSession: (s: Partial<Session>) => jpost('/api/sessions', s),
  deleteSession: (id: string) => jdel(`/api/sessions/${id}`),
  getSession: (id: string) => jget(`/api/sessions/${id}`),
  getMessages: (id: string) => jget(`/api/sessions/${id}/messages`) as Promise<Message[]>,

  listKBs: () => jget('/api/kb') as Promise<KnowledgeBase[]>,
  createKB: (k: Partial<KnowledgeBase>) => jpost('/api/kb', k),
  deleteKB: (id: string) => jdel(`/api/kb/${id}`),
  listDocuments: (kbId: string) => jget(`/api/kb/${kbId}/documents`) as Promise<Document[]>,
  docStatus: (kbId: string, docId: string) =>
    jget(`/api/kb/${kbId}/documents/${docId}/status`),
  uploadDocument: async (kbId: string, file: File) => {
    const b = await baseUrl();
    const fd = new FormData();
    fd.append('file', file);
    const r = await fetch(`${b}/api/kb/${kbId}/documents`, { method: 'POST', body: fd });
    if (!r.ok) throw new Error('upload failed');
    return r.json();
  },

  confirmTool: (request_id: string, decision: 'allow'|'deny', remember: string) =>
    jpost('/api/tools/confirm', { request_id, decision, remember }),
};
```

- [ ] **Step 4: SSE client for chat**

`frontend/src/lib/sse.ts`:
```ts
import { baseUrl } from './port';
import type { ChatEvent } from '../types';

// POST + SSE is not native to EventSource (GET only), so we use fetch
// with a streaming reader.
export async function streamChat(
  sessionId: string,
  message: string,
  opts: { tools_enabled?: boolean; use_rag?: boolean },
  onEvent: (e: ChatEvent) => void,
): Promise<void> {
  const b = await baseUrl();
  const r = await fetch(`${b}/api/sessions/${sessionId}/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, ...opts }),
  });
  if (!r.ok || !r.body) throw new Error(`chat ${r.status}`);

  const reader = r.body.getReader();
  const dec = new TextDecoder();
  let buf = '';
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let idx;
    while ((idx = buf.indexOf('\n\n')) >= 0) {
      const block = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      const ev = parseSSEBlock(block);
      if (ev) onEvent(ev);
    }
  }
}

function parseSSEBlock(block: string): ChatEvent | null {
  let event = 'message';
  let data = '';
  for (const line of block.split('\n')) {
    if (line.startsWith('event: ')) event = line.slice(7);
    else if (line.startsWith('data: ')) data += line.slice(6);
  }
  if (!data) return null;
  try { return { event: event as any, data: JSON.parse(data) }; }
  catch { return null; }
}
```

- [ ] **Step 5: Verify it type-checks**

Run:
```bash
cd frontend && npx tsc --noEmit
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src
git commit -m "feat(frontend): port resolver, API wrappers, SSE chat client"
```

---

## Task 4: Stores (zustand)

**Files:**
- Create: `frontend/src/stores/sessionStore.ts`
- Create: `frontend/src/stores/configStore.ts`
- Create: `frontend/src/stores/kbStore.ts`
- Create: `frontend/src/stores/confirmStore.ts`

- [ ] **Step 1: Session store**

`frontend/src/stores/sessionStore.ts`:
```ts
import { create } from 'zustand';
import { api } from '../lib/api';
import { streamChat } from '../lib/sse';
import type { Session, Message, ChatEvent } from '../types';

interface SessionState {
  sessions: Session[];
  currentId: string | null;
  messages: Message[];
  streaming: boolean;
  loadSessions: () => Promise<void>;
  select: (id: string) => Promise<void>;
  create: (s: Partial<Session>) => Promise<Session>;
  remove: (id: string) => Promise<void>;
  send: (text: string, opts: { tools_enabled?: boolean; use_rag?: boolean }) => Promise<void>;
}

export const useSessionStore = create<SessionState>((set, get) => ({
  sessions: [], currentId: null, messages: [], streaming: false,

  loadSessions: async () => set({ sessions: await api.listSessions() }),

  select: async (id) => {
    set({ currentId: id });
    const res = await api.getSession(id);
    set({ messages: res.messages ?? [] });
  },

  create: async (s) => {
    const sess = await api.createSession(s);
    set({ sessions: [sess, ...get().sessions], currentId: sess.id, messages: [] });
    return sess;
  },

  remove: async (id) => {
    await api.deleteSession(id);
    set({ sessions: get().sessions.filter(x => x.id !== id) });
    if (get().currentId === id) set({ currentId: null, messages: [] });
  },

  send: async (text, opts) => {
    const id = get().currentId;
    if (!id) return;
    set({ streaming: true });
    // optimistic user message
    const userMsg: Message = {
      id: 'pending-' + Date.now(), session_id: id, role: 'user', content: text,
    };
    const asstMsg: Message = {
      id: 'pending-a-' + Date.now(), session_id: id, role: 'assistant', content: '',
    };
    set({ messages: [...get().messages, userMsg, asstMsg] });

    const handle = (e: ChatEvent) => {
      set((st) => {
        const msgs = [...st.messages];
        if (e.event === 'delta') {
          const last = msgs[msgs.length - 1];
          msgs[msgs.length - 1] = { ...last, content: last.content + e.data.text };
        } else if (e.event === 'tool_call') {
          msgs.push({
            id: 'tool-' + e.data.call_id, session_id: id, role: 'tool',
            content: `→ ${e.data.tool}(${JSON.stringify(e.data.input)})`,
            tool_call_id: e.data.call_id,
          });
        } else if (e.event === 'tool_result') {
          msgs.push({
            id: 'res-' + e.data.call_id, session_id: id, role: 'tool',
            content: e.data.content, tool_call_id: e.data.call_id,
          });
        }
        return { messages: msgs };
      });
      // confirm_req is routed to the confirm store via a global hook in App
      if (e.event === 'confirm_req') {
        useConfirmStore.getState().enqueue(e.data);
      }
    };

    try {
      await streamChat(id, text, opts, handle);
    } finally {
      set({ streaming: false });
    }
  },
}));

// cross-store import kept lazy to avoid cycle
import { useConfirmStore } from './confirmStore';
```

- [ ] **Step 2: Config store**

`frontend/src/stores/configStore.ts`:
```ts
import { create } from 'zustand';
import { api } from '../lib/api';
import type { Provider } from '../types';

interface ConfigState {
  providers: Provider[];
  loaded: boolean;
  load: () => Promise<void>;
  create: (p: Omit<Provider,'id'>) => Promise<void>;
  remove: (id: string) => Promise<void>;
}

export const useConfigStore = create<ConfigState>((set, get) => ({
  providers: [], loaded: false,
  load: async () => set({ providers: await api.listProviders(), loaded: true }),
  create: async (p) => { await api.createProvider(p); await get().load(); },
  remove: async (id) => { await api.deleteProvider(id); await get().load(); },
}));
```

- [ ] **Step 3: KB store**

`frontend/src/stores/kbStore.ts`:
```ts
import { create } from 'zustand';
import { api } from '../lib/api';
import type { KnowledgeBase, Document } from '../types';

interface KBState {
  kbs: KnowledgeBase[];
  docsByKb: Record<string, Document[]>;
  load: () => Promise<void>;
  create: (k: Partial<KnowledgeBase>) => Promise<void>;
  remove: (id: string) => Promise<void>;
  loadDocs: (kbId: string) => Promise<void>;
  upload: (kbId: string, file: File) => Promise<void>;
}

export const useKBStore = create<KBState>((set, get) => ({
  kbs: [], docsByKb: {},
  load: async () => set({ kbs: await api.listKBs() }),
  create: async (k) => { await api.createKB(k); await get().load(); },
  remove: async (id) => { await api.deleteKB(id); await get().load(); },
  loadDocs: async (kbId) =>
    set({ docsByKb: { ...get().docsByKb, [kbId]: await api.listDocuments(kbId) } }),
  upload: async (kbId, file) => {
    await api.uploadDocument(kbId, file);
    // poll for completion (simple)
    setTimeout(() => get().loadDocs(kbId), 3000);
  },
}));
```

- [ ] **Step 4: Confirm store**

`frontend/src/stores/confirmStore.ts`:
```ts
import { create } from 'zustand';
import { api } from '../lib/api';

interface ConfirmReq {
  request_id: string; tool: string; input: any;
}
interface ConfirmState {
  pending: ConfirmReq[];
  enqueue: (r: ConfirmReq) => void;
  respond: (id: string, decision: 'allow'|'deny', remember: string) => Promise<void>;
}
export const useConfirmStore = create<ConfirmState>((set, get) => ({
  pending: [],
  enqueue: (r) => set({ pending: [...get().pending, r] }),
  respond: async (id, decision, remember) => {
    await api.confirmTool(id, decision, remember);
    set({ pending: get().pending.filter(p => p.request_id !== id) });
  },
}));
```

- [ ] **Step 5: Type-check**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/stores
git commit -m "feat(frontend): zustand stores for sessions, config, kb, confirm"
```

---

## Task 5: Chat UI Components

**Files:**
- Create: `frontend/src/components/MessageBubble.tsx`
- Create: `frontend/src/components/ChatInput.tsx`
- Create: `frontend/src/components/ChatView.tsx`

- [ ] **Step 1: MessageBubble**

`frontend/src/components/MessageBubble.tsx`:
```tsx
import type { Message } from '../types';

export function MessageBubble({ m }: { m: Message }) {
  const isUser = m.role === 'user';
  const isTool = m.role === 'tool';
  return (
    <div className={`my-2 flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div className={
        'max-w-[80%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap ' +
        (isUser ? 'bg-blue-600 text-white'
         : isTool ? 'bg-gray-200 text-gray-800 font-mono text-xs'
         : 'bg-gray-100 text-gray-900')
      }>
        <div className="text-[10px] uppercase opacity-60 mb-1">
          {isTool ? `tool${m.tool_call_id ? ' '+m.tool_call_id : ''}` : m.role}
        </div>
        {m.content}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: ChatInput**

`frontend/src/components/ChatInput.tsx`:
```tsx
import { useState } from 'react';
import { useSessionStore } from '../stores/sessionStore';

export function ChatInput({ sessionId }: { sessionId: string | null }) {
  const [text, setText] = useState('');
  const [tools, setTools] = useState(true);
  const [rag, setRag] = useState(false);
  const send = useSessionStore(s => s.send);
  const streaming = useSessionStore(s => s.streaming);

  const submit = () => {
    if (!text.trim() || !sessionId || streaming) return;
    send(text, { tools_enabled: tools, use_rag: rag });
    setText('');
  };

  return (
    <div className="border-t p-3">
      <div className="mb-2 flex gap-4 text-sm">
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={tools} onChange={e => setTools(e.target.checked)} />
          Tools
        </label>
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={rag} onChange={e => setRag(e.target.checked)} />
          RAG
        </label>
      </div>
      <div className="flex gap-2">
        <textarea
          className="flex-1 border rounded p-2 text-sm resize-none"
          rows={2}
          value={text}
          onChange={e => setText(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submit(); } }}
          placeholder="Send a message…"
        />
        <button
          className="bg-blue-600 text-white px-4 rounded disabled:opacity-50"
          onClick={submit}
          disabled={!text.trim() || streaming || !sessionId}
        >Send</button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: ChatView**

`frontend/src/components/ChatView.tsx`:
```tsx
import { useEffect, useRef } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { MessageBubble } from './MessageBubble';
import { ChatInput } from './ChatInput';

export function ChatView() {
  const currentId = useSessionStore(s => s.currentId);
  const messages = useSessionStore(s => s.messages);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  return (
    <div className="flex flex-col flex-1 h-screen">
      <div className="flex-1 overflow-y-auto px-4">
        {messages.length === 0 && (
          <div className="text-center text-gray-400 mt-20">
            {currentId ? 'Start the conversation' : 'Select or create a session'}
          </div>
        )}
        {messages.map(m => <MessageBubble key={m.id} m={m} />)}
        <div ref={bottomRef} />
      </div>
      <ChatInput sessionId={currentId} />
    </div>
  );
}
```

- [ ] **Step 4: Type-check + build**

Run:
```bash
cd frontend && npx tsc --noEmit && npm run build
```
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components
git commit -m "feat(frontend): chat view, message bubble, input with tool/rag toggles"
```

---

## Task 6: Sidebar, Settings, KB Manager, Confirm Dialog

**Files:**
- Create: `frontend/src/components/Sidebar.tsx`
- Create: `frontend/src/components/ProviderSettings.tsx`
- Create: `frontend/src/components/SettingsModal.tsx`
- Create: `frontend/src/components/KBManager.tsx`
- Create: `frontend/src/components/ConfirmDialog.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Sidebar**

`frontend/src/components/Sidebar.tsx`:
```tsx
import { useEffect, useState } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { useConfigStore } from '../stores/configStore';

export function Sidebar({ onOpenSettings, onOpenKB }: {
  onOpenSettings: () => void; onOpenKB: () => void;
}) {
  const sessions = useSessionStore(s => s.sessions);
  const currentId = useSessionStore(s => s.currentId);
  const loadSessions = useSessionStore(s => s.loadSessions);
  const select = useSessionStore(s => s.select);
  const create = useSessionStore(s => s.create);
  const remove = useSessionStore(s => s.remove);
  const providers = useConfigStore(s => s.providers);
  const [providerId, setProviderId] = useState<string>('');

  useEffect(() => { loadSessions(); }, [loadSessions]);
  useEffect(() => { if (providers[0]) setProviderId(providers[0].id); }, [providers]);

  const newChat = async () => {
    if (!providerId) { onOpenSettings(); return; }
    await create({ title: '新对话', provider_id: providerId, tools_enabled: true });
  };

  return (
    <div className="w-64 border-r flex flex-col bg-gray-50">
      <div className="p-3 flex gap-2">
        <button className="flex-1 bg-blue-600 text-white rounded py-2 text-sm"
          onClick={newChat}>+ New Chat</button>
      </div>
      <select className="mx-3 mb-2 border rounded p-1 text-sm"
        value={providerId} onChange={e => setProviderId(e.target.value)}>
        {providers.length === 0 && <option value="">No provider — configure</option>}
        {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
      </select>
      <div className="flex-1 overflow-y-auto px-2">
        {sessions.map(s => (
          <div key={s.id}
            className={'group flex items-center px-2 py-2 rounded cursor-pointer text-sm ' +
              (s.id === currentId ? 'bg-blue-100' : 'hover:bg-gray-200')}
            onClick={() => select(s.id)}>
            <span className="flex-1 truncate">{s.title}</span>
            <button className="opacity-0 group-hover:opacity-100 text-red-500 text-xs"
              onClick={e => { e.stopPropagation(); remove(s.id); }}>×</button>
          </div>
        ))}
      </div>
      <div className="border-t p-2 flex gap-2">
        <button className="flex-1 text-sm border rounded py-1" onClick={onOpenKB}>Knowledge</button>
        <button className="flex-1 text-sm border rounded py-1" onClick={onOpenSettings}>Settings</button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: ProviderSettings**

`frontend/src/components/ProviderSettings.tsx`:
```tsx
import { useEffect, useState } from 'react';
import { useConfigStore } from '../stores/configStore';

export function ProviderSettings() {
  const providers = useConfigStore(s => s.providers);
  const loaded = useConfigStore(s => s.loaded);
  const load = useConfigStore(s => s.load);
  const create = useConfigStore(s => s.create);
  const remove = useConfigStore(s => s.remove);
  const [form, setForm] = useState({
    name: '', base_url: 'https://api.openai.com/v1', api_key: '',
    chat_model: 'gpt-4o-mini', embed_model: 'text-embedding-3-small', is_default: true,
  });

  useEffect(() => { if (!loaded) load(); }, [loaded, load]);

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">Providers</h2>
      <div className="space-y-1">
        {providers.map(p => (
          <div key={p.id} className="flex items-center gap-2 text-sm">
            <span className="flex-1">{p.name} — {p.chat_model}</span>
            <button className="text-red-500" onClick={() => remove(p.id)}>Delete</button>
          </div>
        ))}
      </div>
      <div className="border-t pt-3 space-y-2">
        <h3 className="font-medium">Add provider</h3>
        <input className="w-full border rounded p-1 text-sm" placeholder="Name"
          value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
        <input className="w-full border rounded p-1 text-sm" placeholder="Base URL"
          value={form.base_url} onChange={e => setForm({ ...form, base_url: e.target.value })} />
        <input className="w-full border rounded p-1 text-sm" placeholder="API Key" type="password"
          value={form.api_key} onChange={e => setForm({ ...form, api_key: e.target.value })} />
        <input className="w-full border rounded p-1 text-sm" placeholder="Chat model"
          value={form.chat_model} onChange={e => setForm({ ...form, chat_model: e.target.value })} />
        <input className="w-full border rounded p-1 text-sm" placeholder="Embed model (optional)"
          value={form.embed_model} onChange={e => setForm({ ...form, embed_model: e.target.value })} />
        <button className="bg-blue-600 text-white rounded px-3 py-1 text-sm"
          onClick={() => create(form)}>Save</button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: SettingsModal + KBManager + ConfirmDialog**

`frontend/src/components/SettingsModal.tsx`:
```tsx
import { ProviderSettings } from './ProviderSettings';

export function SettingsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg p-6 w-[500px] max-h-[80vh] overflow-y-auto">
        <div className="flex justify-between mb-4">
          <h1 className="text-xl font-bold">Settings</h1>
          <button onClick={onClose}>×</button>
        </div>
        <ProviderSettings />
      </div>
    </div>
  );
}
```

`frontend/src/components/KBManager.tsx`:
```tsx
import { useEffect, useRef, useState } from 'react';
import { useKBStore } from '../stores/kbStore';

export function KBManager({ open, onClose }: { open: boolean; onClose: () => void }) {
  const kbs = useKBStore(s => s.kbs);
  const docsByKb = useKBStore(s => s.docsByKb);
  const load = useKBStore(s => s.load);
  const create = useKBStore(s => s.create);
  const remove = useKBStore(s => s.remove);
  const loadDocs = useKBStore(s => s.loadDocs);
  const upload = useKBStore(s => s.upload);
  const [name, setName] = useState('');
  const fileRef = useRef<HTMLInputElement>(null);
  const [activeKb, setActiveKb] = useState<string | null>(null);

  useEffect(() => { if (open) load(); }, [open, load]);
  useEffect(() => { if (activeKb) loadDocs(activeKb); }, [activeKb, loadDocs]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg p-6 w-[600px] max-h-[80vh] overflow-y-auto">
        <div className="flex justify-between mb-4">
          <h1 className="text-xl font-bold">Knowledge Bases</h1>
          <button onClick={onClose}>×</button>
        </div>
        <div className="flex gap-2 mb-4">
          <input className="flex-1 border rounded p-1 text-sm" placeholder="New KB name"
            value={name} onChange={e => setName(e.target.value)} />
          <button className="bg-blue-600 text-white rounded px-3 text-sm"
            onClick={async () => { if (name) { await create({ name }); setName(''); } }}>
            Create</button>
        </div>
        <div className="space-y-2">
          {kbs.map(kb => (
            <div key={kb.id} className="border rounded p-2">
              <div className="flex items-center">
                <span className="flex-1 font-medium cursor-pointer"
                  onClick={() => setActiveKb(activeKb === kb.id ? null : kb.id)}>{kb.name}</span>
                <button className="text-red-500 text-sm" onClick={() => remove(kb.id)}>Delete</button>
              </div>
              {activeKb === kb.id && (
                <div className="mt-2 ml-4 space-y-1">
                  <input ref={fileRef} type="file" className="text-xs" />
                  <button className="ml-2 text-xs bg-gray-200 rounded px-2 py-1"
                    onClick={() => fileRef.current?.files?.[0] && upload(kb.id, fileRef.current.files[0])}>
                    Upload</button>
                  {(docsByKb[kb.id] ?? []).map(d => (
                    <div key={d.id} className="text-xs flex justify-between">
                      <span>{d.filename}</span>
                      <span className={d.status === 'ready' ? 'text-green-600'
                        : d.status === 'failed' ? 'text-red-600' : 'text-gray-500'}>
                        {d.status}{d.chunk_count ? ` (${d.chunk_count})` : ''}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
```

`frontend/src/components/ConfirmDialog.tsx`:
```tsx
import { useConfirmStore } from '../stores/confirmStore';

export function ConfirmDialog() {
  const pending = useConfirmStore(s => s.pending);
  const respond = useConfirmStore(s => s.respond);
  if (pending.length === 0) return null;
  const req = pending[0];
  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-[100]">
      <div className="bg-white rounded-lg p-6 w-[420px]">
        <h2 className="text-lg font-bold mb-2">Allow tool execution?</h2>
        <div className="bg-gray-100 rounded p-2 mb-4 font-mono text-sm">
          <div className="font-semibold">{req.tool}</div>
          <pre className="whitespace-pre-wrap">{JSON.stringify(req.input, null, 2)}</pre>
        </div>
        <div className="flex gap-2 justify-end">
          <button className="border rounded px-3 py-1 text-sm"
            onClick={() => respond(req.request_id, 'deny', 'never')}>Deny</button>
          <button className="border rounded px-3 py-1 text-sm"
            onClick={() => respond(req.request_id, 'allow', 'session')}>Allow (session)</button>
          <button className="bg-blue-600 text-white rounded px-3 py-1 text-sm"
            onClick={() => respond(req.request_id, 'allow', 'never')}>Allow once</button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Wire App.tsx**

`frontend/src/App.tsx` (replace placeholder):
```tsx
import { useEffect, useState } from 'react';
import { Sidebar } from './components/Sidebar';
import { ChatView } from './components/ChatView';
import { SettingsModal } from './components/SettingsModal';
import { KBManager } from './components/KBManager';
import { ConfirmDialog } from './components/ConfirmDialog';
import { useConfigStore } from './stores/configStore';

export default function App() {
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [kbOpen, setKbOpen] = useState(false);
  const loadConfig = useConfigStore(s => s.load);
  useEffect(() => { loadConfig(); }, [loadConfig]);

  return (
    <div className="flex h-screen">
      <Sidebar onOpenSettings={() => setSettingsOpen(true)} onOpenKB={() => setKbOpen(true)} />
      <ChatView />
      <SettingsModal open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      <KBManager open={kbOpen} onClose={() => setKbOpen(false)} />
      <ConfirmDialog />
    </div>
  );
}
```

- [ ] **Step 5: Build + run**

Run:
```bash
cd frontend && npm run build && cd ..
wails dev
```
Expected: window opens, sidebar + chat render. Configure a provider, create a session, send a message → see streamed response.

- [ ] **Step 6: Commit**

```bash
git add frontend/src
git commit -m "feat(frontend): sidebar, settings, KB manager, confirm dialog, App wiring"
```

---

## Task 7: Manual E2E Walkthrough

- [ ] **Step 1: Happy-path verification**

With `wails dev` running:
1. Open Settings → add a real OpenAI-compatible provider (name, base URL, key, chat + embed models).
2. New Chat → send "say hi" → confirm streamed text appears.
3. Enable Tools → send "run `go version`" → confirm dialog pops → allow → see tool result + final answer.
4. Knowledge → create KB → upload a .md or .txt → wait for status `ready`.
5. New Chat with a provider that has embed model, enable RAG — *(RAG session-KB linkage needs a session.kb_id; the core supports it but the UI doesn't yet expose KB selection at session creation. Add a KB dropdown to `Sidebar.newChat` as a follow-up, or create the session via API with kb_id for this test.)*

- [ ] **Step 2: Commit any UI fixes**

```bash
git add -A
git commit -m "polish(frontend): E2E fixes from walkthrough"
```

---

## Self-Review

**Spec coverage (frontend-relevant):**
- §2 client layer → Wails app + React SPA ✓
- §5 API consumption → `lib/api.ts` covers all endpoints ✓
- §5.2 chat SSE → `lib/sse.ts` streams all event types ✓
- §5.5 confirm flow → `confirmStore` + `ConfirmDialog` ✓
- §4.5 gate UX → confirm dialog with allow/deny/remember ✓
- §6 DTO shapes → `types.ts` mirrors core structs ✓

**Known gaps (flagged for follow-up):**
1. Session → KB linkage in UI (RAG requires `session.kb_id`; Sidebar's `newChat` doesn't offer KB selection yet). Add a KB picker before RAG is fully usable end-to-end.
2. No global `/events` SSE subscription; confirm_req currently rides the chat stream (sufficient for MVP since confirmations only happen mid-chat).
3. No error toasts; failed fetches throw silently. Add a tiny toast layer.
4. Message persistence reload: after `send`, the optimistic pending IDs aren't reconciled with server-assigned IDs on reload. Acceptable for MVP.

---

## Execution Handoff

Plan 2 complete and saved to `docs/superpowers/plans/2026-06-16-plan2-wails-frontend.md`. The same two execution options apply (subagent-driven or inline). Recommend writing Plan 3 first, then executing all three in order.
