import { create } from 'zustand';
import { api } from '../lib/api';

export type RuleScope = 'global' | 'project';

interface RulesState {
  global: string;
  globalExists: boolean;
  project: string;
  projectExists: boolean;
  imports: { claude: boolean; agents: boolean };
  loaded: boolean;
  load: () => Promise<void>;
  save: (scope: RuleScope, body: string) => Promise<void>;
  clear: (scope: RuleScope) => Promise<void>;
  setImports: (v: { claude: boolean; agents: boolean }) => Promise<void>;
}

export const useRulesStore = create<RulesState>((set, get) => ({
  global: '',
  globalExists: false,
  project: '',
  projectExists: false,
  imports: { claude: false, agents: false },
  loaded: false,
  load: async () => {
    // 并发拉取全局、项目内容与导入开关。项目 scope 在 workdir 未设置时后端返回 400，
    // 视为不可用（置空 + exists=false），不抛错打断整个加载。
    const [g, p, im] = await Promise.all([
      api.getRulesContent('global'),
      api.getRulesContent('project').catch(() => ({ body: '', exists: false })),
      api.getRulesImports(),
    ]);
    set({
      global: g.body,
      globalExists: g.exists,
      project: p.body,
      projectExists: p.exists,
      imports: im,
      loaded: true,
    });
  },
  save: async (scope, body) => {
    await api.saveRulesContent(scope, body);
    await get().load();
  },
  clear: async (scope) => {
    await api.clearRulesContent(scope);
    await get().load();
  },
  setImports: async (v) => {
    await api.setRulesImports(v);
    set({ imports: v });
  },
}));
