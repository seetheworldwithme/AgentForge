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
  kbs: [],
  docsByKb: {},
  load: async () => set({ kbs: await api.listKBs() }),
  create: async (k) => {
    await api.createKB(k);
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
    setTimeout(() => get().loadDocs(kbId), 3000);
  },
}));
