import { create } from 'zustand';
import { api } from '../lib/api';
import type { TodoItem } from '../types';

// todo 面板状态：items 由后端 SSE "todo" 事件实时推送（setItems），
// 切会话时用 load(sid) 拉一次该会话的清单，clear 用于切走/新建时清空。
interface TodoState {
  items: TodoItem[];
  load: (sessionId: string) => Promise<void>;
  setItems: (items: TodoItem[]) => void;
  clear: () => void;
}

export const useTodoStore = create<TodoState>((set) => ({
  items: [],
  load: async (sessionId) => {
    try {
      const { items } = await api.listTodo(sessionId);
      set({ items: items ?? [] });
    } catch {
      // 拉取失败（如后端未启用 todo）静默置空，不阻塞对话。
      set({ items: [] });
    }
  },
  setItems: (items) => set({ items: items ?? [] }),
  clear: () => set({ items: [] }),
}));
