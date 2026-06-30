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
  pauseDoc: (kbId: string, docId: string) => Promise<void>;
  resumeDoc: (kbId: string, docId: string) => Promise<void>;
  loadChunks: (kbId: string, docId: string) => Promise<void>;
  previewChunks: (kbId: string, text: string, chunkSize: number, overlap: number) => Promise<ChunkPreview[]>;
  retrieve: (kbId: string, query: string, topK: number) => Promise<void>;
}

// Poll a KB until none of its documents are "processing": 刷新文档表(loadDocs) +
// 侧边栏聚合统计(load)，让 UI 收敛到 ready/failed 而不是卡在过期的 processing。
// 5s 间隔，无硬上限——续传/重试可能合法地超过服务端 ingest 超时；只要没有文档还
// 处于 processing 就停止。
function pollUntilSettled(get: () => KBState, kbId: string) {
  const tick = async () => {
    await get().loadDocs(kbId);
    // 同步刷新 KB 列表聚合统计：侧边栏所有知识库的 ready/processing 计数
    // 才会随入库完成实时更新，而不只是当前激活的那个。
    await get().load();
    const docs = get().docsByKb[kbId] ?? [];
    // 续传后单文档可能跨多次 core 重启，累计远超 10min，故不设硬上限；间隔 5s。
    if (docs.some((d) => d.status === 'processing')) {
      setTimeout(tick, 5000);
    }
  };
  setTimeout(tick, 5000);
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
  pauseDoc: async (kbId, docId) => {
    await api.pauseDocument(kbId, docId);
    await get().loadDocs(kbId);
  },
  resumeDoc: async (kbId, docId) => {
    await api.resumeDocument(kbId, docId);
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
