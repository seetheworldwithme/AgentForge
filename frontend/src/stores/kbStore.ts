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
    // ingest runs async on the server; re-poll after a short delay.
    await get().loadDocs(kbId);
    setTimeout(() => get().loadDocs(kbId), 3000);
  },
  deleteDoc: async (kbId, docId) => {
    await api.deleteDocument(kbId, docId);
    await get().loadDocs(kbId);
    await get().load();
  },
  retryDoc: async (kbId, docId) => {
    await api.retryDocument(kbId, docId);
    await get().loadDocs(kbId);
    setTimeout(() => get().loadDocs(kbId), 3000);
  },
  loadChunks: async (kbId, docId) =>
    set({ chunksByDoc: { ...get().chunksByDoc, [docId]: await api.listChunks(kbId, docId) } }),
  previewChunks: (kbId, text, chunkSize, overlap) =>
    api.chunkPreview(kbId, { text, chunk_size: chunkSize, chunk_overlap: overlap }),
  retrieve: async (kbId, query, topK) => {
    set({ retrieveHits: await api.retrieve(kbId, { query, top_k: topK }) });
  },
}));
