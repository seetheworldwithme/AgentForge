import { create } from 'zustand';
import { api } from '../lib/api';
import type { ConfirmReq } from '../types';

interface ConfirmState {
  pending: ConfirmReq[];
  enqueue: (r: ConfirmReq) => void;
  respond: (id: string, decision: 'allow' | 'deny', remember: string) => Promise<void>;
}

// ConfirmDialog reads pending[0]; the sessionStore enqueues requests as they
// arrive on the chat SSE stream.
export const useConfirmStore = create<ConfirmState>((set, get) => ({
  pending: [],
  enqueue: (r) => set({ pending: [...get().pending, r] }),
  respond: async (id, decision, remember) => {
    await api.confirmTool(id, decision, remember);
    set({ pending: get().pending.filter((p) => p.request_id !== id) });
  },
}));
