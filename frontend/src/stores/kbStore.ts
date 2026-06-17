import { create } from 'zustand';
import { api } from '../lib/api';
import type { KnowledgeBase, Document, ChunkPreview, RetrieveHit } from '../types';

interface KBState {
  kbs: KnowledgeBase[];
  docsByKb: Record<string, Document[]>;
  chunksByDoc: Record<string, ChunkPreview[]>;
  retrieveHits: RetrieveHit[];
  load: () => Promise<void>;
  create: (k: Partial<KnowledgeBase>) => Promise<void>;
  update: (id: string, k: Partial<KnowledgeBase>) => Promise<void>;
  remove: (id: string) => Promise<void>;
  loadDocs: (kbId: string) => Promise<void>;
  upload: (kbId: string, file: File) => Promise<void>;
  deleteDoc: (kbId: string, docId: string) => Promise<void>;
  retryDoc: (kbId: string, docId: string) => Promise<void>;
  loadChunks: (kbId: string, docId: string) => Promise<void>;
  previewChunks: (kbId: string, text: string, chunkSize: number, overlap: number) => Promise<ChunkPreview[]>;
  retrieve: (kbId: string, query: string, topK: number) => Promise<void>;
}

// Poll a KB's documents every 2s until none are "processing" (capped at ~10
// min to match the server ingest timeout), so the UI refreshes to ready/failed
// instead of freezing on a stale "processing" badge.
function pollUntilSettled(get: () => KBState, kbId: string) {
  let tries = 0;
  const tick = async () => {
    await get().loadDocs(kbId);
    const docs = get().docsByKb[kbId] ?? [];
    if (docs.some((d) => d.status === 'processing') && ++tries < 300) {
      setTimeout(tick, 2000);
    }
  };
  setTimeout(tick, 2000);
}

export const useKBStore = create<KBState>((set, get) => ({
  kbs: [],
  docsByKb: {},
  chunksByDoc: {},
  retrieveHits: [],
  load: async () => set({ kbs: await api.listKBs() }),
  create: async (k) => {
    await api.createKB(k);
    await get().load();
  },
  update: async (id, k) => {
    await api.updateKB(id, k);
    await get().load();
  },
  remove: async (id) => {
    await api.deleteKB(id);
    await get().load();
  },
  loadDocs: async (kbId) =>
    set({ docsByKb: { ...get().docsByKb, [kbId]: await api.listDocuments(kbId) } }),
  upload: async (kbId, file) => {
    await api.uploadDocument(kbId, file);
    // ingest runs async on the server; poll until it settles.
    await get().loadDocs(kbId);
    pollUntilSettled(get, kbId);
  },
  deleteDoc: async (kbId, docId) => {
    await api.deleteDocument(kbId, docId);
    await get().loadDocs(kbId);
    await get().load();
  },
  retryDoc: async (kbId, docId) => {
    await api.retryDocument(kbId, docId);
    await get().loadDocs(kbId);
    pollUntilSettled(get, kbId);
  },
  loadChunks: async (kbId, docId) =>
    set({ chunksByDoc: { ...get().chunksByDoc, [docId]: await api.listChunks(kbId, docId) } }),
  previewChunks: (kbId, text, chunkSize, overlap) =>
    api.chunkPreview(kbId, { text, chunk_size: chunkSize, chunk_overlap: overlap }),
  retrieve: async (kbId, query, topK) => {
    set({ retrieveHits: await api.retrieve(kbId, { query, top_k: topK }) });
  },
}));
