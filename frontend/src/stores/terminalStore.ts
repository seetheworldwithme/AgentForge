// 终端面板状态：管理多个终端 tab（创建/切换/关闭/全部销毁）与抽屉开关、高度。
// term/ws 实例随 tab 一起持有；ChatView 卸载时调 disposeAll 释放，避免 term 活着但 DOM 没了。
import { create } from 'zustand';
import { createTerminalTab, disposeTab, type TerminalTab } from '../lib/terminal';

interface TerminalState {
  tabs: TerminalTab[];
  activeId: string | null;
  panelOpen: boolean;
  panelHeight: number;
  togglePanel: () => Promise<void>;
  createTab: () => Promise<void>;
  closeTab: (id: string) => void;
  setActive: (id: string) => void;
  setHeight: (h: number) => void;
  disposeAll: () => void;
}

export const useTerminalStore = create<TerminalState>((set, get) => ({
  tabs: [],
  activeId: null,
  panelOpen: false,
  panelHeight: 240,

  togglePanel: async () => {
    if (get().panelOpen) {
      set({ panelOpen: false });
      return;
    }
    set({ panelOpen: true });
    // 首次展开若无终端，自动新建一个
    if (get().tabs.length === 0) {
      await get().createTab();
    }
  },

  createTab: async () => {
    // 编号基于已有 tabs 的最大序号 + 1：首次必为「终端 1」，关闭后重开也不重复
    const nextSeq = get().tabs.reduce((m, t) => Math.max(m, t.seq ?? 0), 0) + 1;
    const id = `term-${Date.now()}-${nextSeq}`;
    const title = `终端 ${nextSeq}`;
    try {
      const tab = await createTerminalTab(id, title);
      set((s) => ({ tabs: [...s.tabs, { ...tab, seq: nextSeq }], activeId: id, panelOpen: true }));
    } catch (e) {
      console.error('创建终端失败', e);
    }
  },

  closeTab: (id) => {
    const { tabs, activeId } = get();
    const idx = tabs.findIndex((t) => t.id === id);
    if (idx < 0) return;
    disposeTab(tabs[idx]);
    const next = tabs.filter((t) => t.id !== id);
    // 关的是当前激活的，切到相邻 tab；全关则收起面板
    const newActive =
      activeId === id ? (next.length ? next[Math.min(idx, next.length - 1)].id : null) : activeId;
    set({ tabs: next, activeId: newActive, panelOpen: next.length > 0 && get().panelOpen });
  },

  setActive: (id) => set({ activeId: id }),

  setHeight: (h) => set({ panelHeight: h }),

  disposeAll: () => {
    get().tabs.forEach(disposeTab);
    set({ tabs: [], activeId: null, panelOpen: false });
  },
}));
