import { create } from 'zustand';
import { api } from '../lib/api';
import type { MemoryEntry, MemoryType } from '../types';

interface MemoryState {
  entries: MemoryEntry[];
  loaded: boolean;
  load: () => Promise<void>;
  save: (
    name: string,
    body: { description: string; type: MemoryType; body: string },
  ) => Promise<void>;
  remove: (name: string) => Promise<void>;
}

export const useMemoryStore = create<MemoryState>((set, get) => ({
  entries: [],
  loaded: false,
  load: async () => {
    const { entries } = await api.listMemory();
    set({ entries, loaded: true });
  },
  save: async (name, body) => {
    await api.saveMemory(name, body);
    await get().load();
  },
  remove: async (name) => {
    await api.deleteMemory(name);
    await get().load();
  },
}));
