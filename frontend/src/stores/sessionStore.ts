import { create, type StoreApi } from 'zustand';
import { api } from '../lib/api';
import { streamChat } from '../lib/sse';
import { useConfirmStore } from './confirmStore';
import type { Session, Message, ChatEvent } from '../types';

interface SessionState {
  sessions: Session[];
  currentId: string | null;
  messages: Message[];
  streaming: boolean;
  abortController: AbortController | null;
  loadSessions: () => Promise<void>;
  select: (id: string) => Promise<void>;
  create: (s: Partial<Session>) => Promise<Session>;
  newChat: () => void;
  remove: (id: string) => Promise<void>;
  rename: (id: string, title: string) => Promise<void>;
  send: (
    text: string,
    opts: {
      tools_enabled?: boolean;
      use_rag?: boolean;
      provider_id?: string;
      kb_id?: string;
      plan_mode?: boolean;
      skill_ids?: string[];
      attachments?: string[];
      images?: string[];
    },
  ) => Promise<void>;
  retry: (
    opts: {
      tools_enabled?: boolean;
      use_rag?: boolean;
      provider_id?: string;
      kb_id?: string;
      plan_mode?: boolean;
      skill_ids?: string[];
    },
  ) => Promise<void>;
  editAndResend: (msgId: string, newText: string, opts: { provider_id?: string }) => Promise<void>;
  stopStreaming: () => void;
}

export const useSessionStore = create<SessionState>((set, get) => ({
  sessions: [],
  currentId: null,
  messages: [],
  streaming: false,
  abortController: null,

  loadSessions: async () => set({ sessions: await api.listSessions() }),

  select: async (id) => {
    // 切换会话前终止可能仍在进行的流式输出，避免旧会话事件污染新会话、
    // 以及 streaming 状态卡住导致切回后界面异常（如白屏）。
    if (get().streaming) {
      get().abortController?.abort();
      set({ streaming: false, abortController: null });
    }
    set({ currentId: id });
    const res = await api.getSession(id);
    set({ messages: res.messages ?? [] });
  },

  create: async (s) => {
    const sess = await api.createSession(s);
    set({ sessions: [sess, ...get().sessions], currentId: sess.id, messages: [] });
    return sess;
  },

  // 进入空白草稿态：不立即创建 session，侧边栏列表不变。
  // 真正的 session 在用户发送第一条消息时由 send 惰性创建。
  newChat: () => set({ currentId: null, messages: [] }),

  remove: async (id) => {
    await api.deleteSession(id);
    set({ sessions: get().sessions.filter((x) => x.id !== id) });
    if (get().currentId === id) set({ currentId: null, messages: [] });
  },

  rename: async (id, title) => {
    const updated = await api.updateSession(id, { title });
    set({
      sessions: get().sessions.map((s) => (s.id === id ? { ...s, ...updated } : s)),
    });
  },

  send: async (text, opts) => {
    let id = get().currentId;
    // 草稿态：第一条消息时才真正创建 session，避免“新对话”空挂在列表里。
    if (!id) {
      const sess = await api.createSession({
        title: '新对话',
        provider_id: opts.provider_id,
        kb_id: opts.kb_id,
        tools_enabled: opts.tools_enabled,
      });
      id = sess.id;
      set({ sessions: [sess, ...get().sessions], currentId: id });
    } else if (opts.kb_id !== undefined) {
      const current = get().sessions.find((s) => s.id === id);
      if (current && (current.kb_id ?? '') !== opts.kb_id) {
        const updated = await api.updateSession(id, {
          title: current.title,
          provider_id: current.provider_id,
          tools_enabled: current.tools_enabled,
          kb_id: opts.kb_id,
        });
        set({ sessions: get().sessions.map((s) => (s.id === id ? { ...s, ...updated } : s)) });
      }
    }
    const abortController = new AbortController();
    set({ streaming: true, abortController });

    // optimistic user + assistant message
    const now = Date.now();
    const ts = new Date(now).toISOString();
    const userMsg: Message = { id: 'pending-' + now, session_id: id, role: 'user', content: text, images: opts.images, created_at: ts };
    const asstMsg: Message = { id: 'pending-a-' + now, session_id: id, role: 'assistant', content: '', created_at: ts };
    set({ messages: [...get().messages, userMsg, asstMsg] });

    const handle = buildChatHandler(set, get, id);

    try {
      await streamChat(id, text, opts, handle, abortController.signal);
    } catch (e) {
      if (!(e instanceof DOMException && e.name === 'AbortError')) {
        throw e;
      }
    } finally {
      set({ streaming: false, abortController: null });
    }
  },

  retry: async (opts) => {
    const id = get().currentId;
    if (!id || get().streaming) return;
    // 找到最后一条 user 消息，作为重新生成的起点
    const cur = get().messages;
    let userIdx = -1;
    for (let i = cur.length - 1; i >= 0; i--) {
      if (cur[i].role === 'user') {
        userIdx = i;
        break;
      }
    }
    if (userIdx < 0) return;
    // 乐观更新：截断该 user 之后的内容，并追加一条空 assistant 占位
    const kept = cur.slice(0, userIdx + 1);
    const asstMsg: Message = { id: 'pending-a-' + Date.now(), session_id: id, role: 'assistant', content: '', created_at: new Date().toISOString() };
    set({ messages: [...kept, asstMsg] });

    const abortController = new AbortController();
    set({ streaming: true, abortController });

    const handle = buildChatHandler(set, get, id);
    try {
      await streamChat(id, '', { ...opts, regenerate: true }, handle, abortController.signal);
    } catch (e) {
      if (!(e instanceof DOMException && e.name === 'AbortError')) {
        throw e;
      }
    } finally {
      set({ streaming: false, abortController: null });
    }
  },

  // 编辑重发：定位指定 user 消息，截断其后内容、改写其正文，再以新文本重新生成。
  // 与 retry 同构，区别在于按 msgId 精确定位（而非最后一条 user）并替换文本。
  editAndResend: async (msgId, newText, opts) => {
    const id = get().currentId;
    if (!id || get().streaming) return;
    const cur = get().messages;
    const userIdx = cur.findIndex((m) => m.id === msgId && m.role === 'user');
    if (userIdx < 0) return;
    // 乐观更新：截断该 user 之后的内容，改写其正文，并追加一条空 assistant 占位
    const kept = cur.slice(0, userIdx + 1);
    kept[userIdx] = { ...kept[userIdx], content: newText };
    const asstMsg: Message = { id: 'pending-a-' + Date.now(), session_id: id, role: 'assistant', content: '', created_at: new Date().toISOString() };
    set({ messages: [...kept, asstMsg] });

    const abortController = new AbortController();
    set({ streaming: true, abortController });

    const handle = buildChatHandler(set, get, id);
    try {
      // message 传新文本，后端用作 UpdateMessageContent 的 content 源；edit_message_id 用于定位目标 user
      await streamChat(id, newText, { provider_id: opts.provider_id, edit_message_id: msgId }, handle, abortController.signal);
    } catch (e) {
      if (!(e instanceof DOMException && e.name === 'AbortError')) {
        throw e;
      }
    } finally {
      set({ streaming: false, abortController: null });
    }
  },

  stopStreaming: () => {
    const controller = get().abortController;
    if (controller) {
      controller.abort();
    }
  },
}));

// buildChatHandler 构造一次会话的 SSE 事件处理器，send 与 retry 共用同一套
// 渲染逻辑（delta 累加、工具调用/结果、标题更新、错误与警告）。
function buildChatHandler(
  set: StoreApi<SessionState>['setState'],
  get: StoreApi<SessionState>['getState'],
  id: string,
) {
  return (e: ChatEvent) => {
    // 普通发送持久化 user 后回传真实 id：把乐观消息的 pending- id 替换为真实 id，
    // 使该消息后续可被「编辑重发」按 id 精确定位。
    if (e.event === 'user_saved') {
      const realId = e.data?.user_msg_id;
      if (typeof realId !== 'string' || !realId) return;
      set((st) => {
        const msgs = [...st.messages];
        for (let i = msgs.length - 1; i >= 0; i--) {
          if (msgs[i].role === 'user' && msgs[i].id.startsWith('pending-')) {
            msgs[i] = { ...msgs[i], id: realId };
            break;
          }
        }
        return { messages: msgs };
      });
      return;
    }
    // Tool confirmations are routed to the confirm store for the dialog.
    if (e.event === 'confirm_req') {
      useConfirmStore.getState().enqueue({
        request_id: e.data.request_id ?? e.data.id,
        tool: e.data.tool,
        input: e.data.input,
        match_key_hint: e.data.match_key_hint,
      });
      return;
    }

    // Auto-generated title for the first turn: update the sidebar entry.
    if (e.event === 'title' && e.data?.title) {
      const sid = e.data.session_id ?? id;
      set((st) => ({
        sessions: st.sessions.map((s) =>
          s.id === sid ? { ...s, title: e.data.title } : s,
        ),
      }));
      return;
    }

    set((st) => {
      const msgs = [...st.messages];
      if (e.event === 'delta') {
        let text = e.data.text ?? '';
        if (!text) return st; // skip empty deltas to reduce blank lines
        // append onto the last assistant message (tools may interleave)
        for (let i = msgs.length - 1; i >= 0; i--) {
          if (msgs[i].role === 'assistant') {
            if (msgs[i].content.trim().length === 0) {
              text = text.trimStart();
            }
            if (!text) return st;
            msgs[i] = { ...msgs[i], content: msgs[i].content + text };
            break;
          }
        }
      } else if (e.event === 'thinking') {
        // 推理模型的思考过程：追加到最后一条 assistant 的 thinking 字段（与 delta 同一目标消息，
        // tool_result 后新开的空 assistant 也会被命中，保证新一轮思考进新消息）。
        const text = e.data?.text ?? '';
        if (!text) return st;
        for (let i = msgs.length - 1; i >= 0; i--) {
          if (msgs[i].role === 'assistant') {
            msgs[i] = { ...msgs[i], thinking: (msgs[i].thinking ?? '') + text };
            break;
          }
        }
      } else if (e.event === 'tool_call') {
        // Parse raw args for a cleaner display format
        let input = e.data.input;
        let displayInput = input;
        if (input && typeof input.raw === 'string') {
          try {
            const parsed = JSON.parse(input.raw);
            displayInput = { ...parsed };
            // flatten nested objects for compact display
            for (const k of Object.keys(displayInput)) {
              if (typeof displayInput[k] === 'object' && displayInput[k] !== null) {
                displayInput[k] = JSON.stringify(displayInput[k]);
              }
            }
          } catch {
            displayInput = input.raw;
          }
        }
        msgs.push({
          id: 'tool-' + e.data.call_id, session_id: id, role: 'tool',
          content: `→ ${e.data.tool}(${JSON.stringify(displayInput)})`,
          tool_call_id: e.data.call_id,
        });
      } else if (e.event === 'tool_result') {
        // Append result to the matching tool_call message (same bubble)
        let found = false;
        for (let i = msgs.length - 1; i >= 0; i--) {
          if (msgs[i].tool_call_id === e.data.call_id && msgs[i].content.startsWith('→')) {
            const result = (e.data.content ?? '').trimEnd();
            msgs[i] = { ...msgs[i], content: msgs[i].content + '\n─────────\n' + result };
            found = true;
            break;
          }
        }
        if (!found) {
          msgs.push({
            id: 'res-' + e.data.call_id, session_id: id, role: 'tool',
            content: e.data.content, tool_call_id: e.data.call_id,
          });
        }
        // Start a new assistant message for subsequent LLM deltas
        msgs.push({
          id: 'asst-' + e.data.call_id + '-' + Date.now(),
          session_id: id, role: 'assistant', content: '',
        });
      } else if (e.event === 'status') {
        // 工具调用上限：插入一条居中警告气泡，其余 status 仅保活，不渲染。
        if (e.data?.kind === 'tool_limit_reached') {
          const msg = e.data?.message ?? '已达到工具调用上限，不再执行新的工具调用。';
          return { messages: [...msgs, { id: 'warn-' + Date.now(), session_id: id, role: 'assistant', content: msg, variant: 'warning' }] };
        }
        return st;
      } else if (e.event === 'done') {
        // 本轮结束：把后端 done 事件携带的精确 tokens/s 记到最后一条 assistant 消息，
        // 供 Retry 旁展示（provider 未返回 usage 时无此字段，前端不显示）。
        const tps = e.data?.tokens_per_sec;
        if (typeof tps === 'number' && tps > 0) {
          for (let i = msgs.length - 1; i >= 0; i--) {
            if (msgs[i].role === 'assistant') {
              msgs[i] = { ...msgs[i], tps };
              break;
            }
          }
        }
      } else if (e.event === 'error') {
        const text = e.data?.message ? `错误：${e.data.message}` : '错误：请求失败';
        let found = false;
        for (let i = msgs.length - 1; i >= 0; i--) {
          if (msgs[i].role === 'assistant') {
            msgs[i] = { ...msgs[i], content: msgs[i].content ? msgs[i].content + '\n\n' + text : text };
            found = true;
            break;
          }
        }
        if (!found) {
          msgs.push({ id: 'err-' + Date.now(), session_id: id, role: 'assistant', content: text });
        }
      }
      return { messages: msgs };
    });
  };
}
