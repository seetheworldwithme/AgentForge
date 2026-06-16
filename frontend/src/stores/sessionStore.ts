import { create } from 'zustand';
import { api } from '../lib/api';
import { streamChat } from '../lib/sse';
import { useConfirmStore } from './confirmStore';
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
  sessions: [],
  currentId: null,
  messages: [],
  streaming: false,

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
    set({ sessions: get().sessions.filter((x) => x.id !== id) });
    if (get().currentId === id) set({ currentId: null, messages: [] });
  },

  send: async (text, opts) => {
    const id = get().currentId;
    if (!id) return;
    set({ streaming: true });

    // optimistic user + assistant message
    const now = Date.now();
    const userMsg: Message = { id: 'pending-' + now, session_id: id, role: 'user', content: text };
    const asstMsg: Message = { id: 'pending-a-' + now, session_id: id, role: 'assistant', content: '' };
    set({ messages: [...get().messages, userMsg, asstMsg] });

    const handle = (e: ChatEvent) => {
      // Tool confirmations are routed to the confirm store for the dialog.
      if (e.event === 'confirm_req') {
        useConfirmStore.getState().enqueue({
          request_id: e.data.request_id ?? e.data.id,
          tool: e.data.tool,
          input: e.data.input,
        });
        return;
      }

      set((st) => {
        const msgs = [...st.messages];
        if (e.event === 'delta') {
          // append onto the last assistant message (tools may interleave)
          for (let i = msgs.length - 1; i >= 0; i--) {
            if (msgs[i].role === 'assistant') {
              msgs[i] = { ...msgs[i], content: msgs[i].content + (e.data.text ?? '') };
              break;
            }
          }
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
    };

    try {
      await streamChat(id, text, opts, handle);
    } finally {
      set({ streaming: false });
    }
  },
}));
