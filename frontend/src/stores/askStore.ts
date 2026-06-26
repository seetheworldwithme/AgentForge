import { create } from 'zustand';
import { api } from '../lib/api';
import type { AskReq } from '../types';

interface AskState {
  pending: AskReq[];
  enqueue: (r: AskReq) => void;
  respond: (
    id: string,
    payload: { selection?: string; other?: string; canceled?: boolean },
  ) => Promise<void>;
}

// AskUserDialog 读 pending[0]；sessionStore 在 SSE 收到 ask_user_req 时 enqueue。
export const useAskStore = create<AskState>((set, get) => ({
  pending: [],
  enqueue: (r) =>
    set((st) =>
      st.pending.some((p) => p.request_id === r.request_id)
        ? st
        : { pending: [...st.pending, r] },
    ),
  respond: async (id, payload) => {
    await api.askUser(id, payload.selection ?? '', payload.other ?? '', payload.canceled ?? false);
    set({ pending: get().pending.filter((p) => p.request_id !== id) });
  },
}));
