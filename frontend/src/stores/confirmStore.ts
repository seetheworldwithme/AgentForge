import { create } from 'zustand';
import { api } from '../lib/api';
import type { ConfirmReq } from '../types';

interface ConfirmState {
  pending: ConfirmReq[];
  enqueue: (r: ConfirmReq) => void;
  syncPending: () => Promise<void>;
  respond: (id: string, decision: 'allow' | 'deny', remember: string) => Promise<void>;
}

// ConfirmDialog reads pending[0]; the sessionStore enqueues requests as they
// arrive on the chat SSE stream.
export const useConfirmStore = create<ConfirmState>((set, get) => ({
  pending: [],
  enqueue: (r) =>
    set((st) =>
      st.pending.some((p) => p.request_id === r.request_id)
        ? st
        : { pending: [...st.pending, r] },
    ),
  syncPending: async () => {
    const res = await api.listPendingTools();
    set((st) => {
      const next = [...st.pending];
      for (const req of res.pending ?? []) {
        if (!next.some((p) => p.request_id === req.request_id)) {
          next.push(req);
        }
      }
      const live = new Set((res.pending ?? []).map((p) => p.request_id));
      return { pending: next.filter((p) => live.has(p.request_id)) };
    });
  },
  respond: async (id, decision, remember) => {
    await api.confirmTool(id, decision, remember);
    set({ pending: get().pending.filter((p) => p.request_id !== id) });
  },
}));
