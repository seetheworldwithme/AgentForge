import { create } from 'zustand';
import { api } from '../lib/api';
import type { Provider } from '../types';

interface ConfigState {
  providers: Provider[];
  loaded: boolean;
  load: () => Promise<void>;
  create: (p: Omit<Provider, 'id'>) => Promise<void>;
  remove: (id: string) => Promise<void>;
}

export const useConfigStore = create<ConfigState>((set, get) => ({
  providers: [],
  loaded: false,
  load: async () => set({ providers: await api.listProviders(), loaded: true }),
  create: async (p) => {
    await api.createProvider(p);
    await get().load();
  },
  remove: async (id) => {
    await api.deleteProvider(id);
    await get().load();
  },
}));
