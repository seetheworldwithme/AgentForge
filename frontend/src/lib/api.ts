import { baseUrl } from './port';
import type {
  Provider,
  Session,
  Message,
  KnowledgeBase,
  Document,
  ChunkPreview,
  RetrieveHit,
  Skill,
  MCPServer,
} from '../types';

// All network I/O lives here. Components/stores never call fetch directly.

async function jget<T = any>(path: string): Promise<T> {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`);
  if (!r.ok) throw new Error(await responseError(path, r));
  return r.json() as Promise<T>;
}
async function jpost<T = any>(path: string, body: any): Promise<T> {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(await responseError(path, r));
  return r.json() as Promise<T>;
}
async function jdel(path: string): Promise<void> {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`, { method: 'DELETE' });
  if (!r.ok) throw new Error(await responseError(path, r));
}
async function jput<T = any>(path: string, body: any): Promise<T> {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(await responseError(path, r));
  return r.json() as Promise<T>;
}

async function responseError(path: string, r: Response): Promise<string> {
  try {
    const body = await r.json();
    const msg = body?.error?.message ?? body?.message;
    if (msg) return msg;
  } catch {
    /* fall back to status */
  }
  return `${path} ${r.status}`;
}

export const api = {
  // --- providers ---
  listProviders: () => jget<Provider[]>('/api/providers'),
  createProvider: (p: Omit<Provider, 'id'>) => jpost<Provider>('/api/providers', p),
  deleteProvider: (id: string) => jdel(`/api/providers/${id}`),
  updateProvider: (id: string, p: Omit<Provider, 'id'>) =>
    jput<Provider>(`/api/providers/${id}`, p),
  // The provider dedicated to conversation-title generation. Separate from the
  // chat provider so the title call runs on its own connection in parallel.
  getTitleProvider: () => jget<{ provider_id: string }>('/api/settings/title-provider'),
  setTitleProvider: (provider_id: string) =>
    jput<{ provider_id: string }>('/api/settings/title-provider', { provider_id }),
  // 单次对话的工具调用硬上限。0 = 不限制。
  getToolLimit: () => jget<{ limit: number }>('/api/settings/tool-limit'),
  setToolLimit: (limit: number) =>
    jput<{ limit: number }>('/api/settings/tool-limit', { limit }),
  // 工具确认规则：manual（逐次确认）/ auto（直接执行）。
  getConfirmMode: () => jget<{ mode: 'manual' | 'auto' }>('/api/settings/confirm-mode'),
  setConfirmMode: (mode: 'manual' | 'auto') =>
    jput<{ mode: string }>('/api/settings/confirm-mode', { mode }),
  // Validate connectivity before saving. `kind` picks the probe path:
  // "chat" (default) sends one chat completion; "embed" requests one
  // embedding. Returns ok/error; never throws on auth failure — the UI
  // branches on `ok`. Does NOT persist.
  testProvider: (p: {
    base_url: string;
    api_key: string;
    chat_model?: string;
    embed_model?: string;
    kind: 'chat' | 'embed';
  }) => jpost<{ ok: boolean; error?: string }>('/api/providers/test', p),

  // --- sessions ---
  listSessions: () => jget<Session[]>('/api/sessions'),
  createSession: (s: Partial<Session>) => jpost<Session>('/api/sessions', s),
  updateSession: (id: string, s: Partial<Session>) =>
    jput<Session>(`/api/sessions/${id}`, s),
  deleteSession: (id: string) => jdel(`/api/sessions/${id}`),
  getSession: (id: string) =>
    jget<{ session: Session; messages: Message[] }>(`/api/sessions/${id}`),
  getMessages: (id: string) => jget<Message[]>(`/api/sessions/${id}/messages`),

  // --- knowledge bases ---
  listKBs: () => jget<KnowledgeBase[]>('/api/kb'),
  createKB: (k: Partial<KnowledgeBase>) => jpost<KnowledgeBase>('/api/kb', k),
  updateKB: (id: string, k: Partial<KnowledgeBase>) => jput<KnowledgeBase>(`/api/kb/${id}`, k),
  deleteKB: (id: string) => jdel(`/api/kb/${id}`),
  listDocuments: (kbId: string) => jget<Document[]>(`/api/kb/${kbId}/documents`),
  deleteDocument: (kbId: string, docId: string) => jdel(`/api/kb/${kbId}/documents/${docId}`),
  retryDocument: (kbId: string, docId: string) =>
    jpost<{ document_id: string; status: string }>(`/api/kb/${kbId}/documents/${docId}/retry`, {}),
  listChunks: (kbId: string, docId: string) =>
    jget<ChunkPreview[]>(`/api/kb/${kbId}/documents/${docId}/chunks`),
  chunkPreview: (kbId: string, body: { text: string; chunk_size: number; chunk_overlap: number }) =>
    jpost<ChunkPreview[]>(`/api/kb/${kbId}/chunk-preview`, body),
  retrieve: (kbId: string, body: { query: string; top_k: number }) =>
    jpost<RetrieveHit[]>(`/api/kb/${kbId}/retrieve`, body),
  docStatus: (kbId: string, docId: string) =>
    jget<{ status: string; chunk_count: number; error?: string }>(
      `/api/kb/${kbId}/documents/${docId}/status`,
    ),
  uploadDocument: async (kbId: string, file: File) => {
    const b = await baseUrl();
    const fd = new FormData();
    fd.append('file', file);
    const r = await fetch(`${b}/api/kb/${kbId}/documents`, { method: 'POST', body: fd });
    if (!r.ok) throw new Error(`upload ${r.status}`);
    return r.json() as Promise<{ document_id: string; status: string }>;
  },

  // --- tool confirmation ---
  listPendingTools: () => jget<{ pending: Array<{ request_id: string; tool: string; input: any; match_key_hint?: string }> }>('/api/tools/pending'),
  confirmTool: (request_id: string, decision: 'allow' | 'deny', remember: string) =>
    jpost('/api/tools/confirm', { request_id, decision, remember }),

  // --- working directory ---
  getWorkDir: () => jget<{ workdir: string }>('/api/workdir'),
  setWorkDir: (dir: string) => jput<{ workdir: string }>('/api/workdir', { workdir: dir }),

  // --- skills ---
  listSkills: () => jget<Skill[]>('/api/skills'),
  setSkillEnabled: (id: string, enabled: boolean) =>
    jput<{ id: string; enabled: boolean }>(`/api/skills/${encodeURIComponent(id)}`, { enabled }),

  // --- MCP ---
  listMCPServers: () => jget<MCPServer[]>('/api/mcp/servers'),
  saveMCPServers: (servers: MCPServer[]) => jput<MCPServer[]>('/api/mcp/servers', servers),
  getMCPConfig: () => jget<Record<string, any>>('/api/mcp/config'),
  saveMCPConfig: (config: Record<string, any>) => jput<Record<string, any>>('/api/mcp/config', config),
  getMCPConfigPath: () => jget<{ path: string }>('/api/mcp/config-path'),
};
