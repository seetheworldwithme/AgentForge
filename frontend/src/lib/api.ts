import { baseUrl } from './port';
import type { Provider, Session, Message, KnowledgeBase, Document } from '../types';

// All network I/O lives here. Components/stores never call fetch directly.

async function jget<T = any>(path: string): Promise<T> {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`);
  if (!r.ok) throw new Error(`${path} ${r.status}`);
  return r.json() as Promise<T>;
}
async function jpost<T = any>(path: string, body: any): Promise<T> {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(`${path} ${r.status}`);
  return r.json() as Promise<T>;
}
async function jdel(path: string): Promise<void> {
  const b = await baseUrl();
  const r = await fetch(`${b}${path}`, { method: 'DELETE' });
  if (!r.ok) throw new Error(`${path} ${r.status}`);
}

export const api = {
  // --- providers ---
  listProviders: () => jget<Provider[]>('/api/providers'),
  createProvider: (p: Omit<Provider, 'id'>) => jpost<Provider>('/api/providers', p),
  deleteProvider: (id: string) => jdel(`/api/providers/${id}`),

  // --- sessions ---
  listSessions: () => jget<Session[]>('/api/sessions'),
  createSession: (s: Partial<Session>) => jpost<Session>('/api/sessions', s),
  deleteSession: (id: string) => jdel(`/api/sessions/${id}`),
  getSession: (id: string) =>
    jget<{ session: Session; messages: Message[] }>(`/api/sessions/${id}`),
  getMessages: (id: string) => jget<Message[]>(`/api/sessions/${id}/messages`),

  // --- knowledge bases ---
  listKBs: () => jget<KnowledgeBase[]>('/api/kb'),
  createKB: (k: Partial<KnowledgeBase>) => jpost<KnowledgeBase>('/api/kb', k),
  deleteKB: (id: string) => jdel(`/api/kb/${id}`),
  listDocuments: (kbId: string) => jget<Document[]>(`/api/kb/${kbId}/documents`),
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
  confirmTool: (request_id: string, decision: 'allow' | 'deny', remember: string) =>
    jpost('/api/tools/confirm', { request_id, decision, remember }),
};
