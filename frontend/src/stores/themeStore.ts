import { create } from 'zustand';

// 主题状态：用户偏好（light/dark/system）+ 实际生效值（resolved）。
// 在 index.html 内联脚本里已提前应用一次，避免首屏闪烁；这里负责运行期管理与切换。
type ThemePref = 'light' | 'dark' | 'system';
type Resolved = 'light' | 'dark';

const STORAGE_KEY = 'af-theme';
const prefersDark = () =>
  typeof window !== 'undefined' &&
  window.matchMedia('(prefers-color-scheme: dark)').matches;

function resolve(pref: ThemePref): Resolved {
  if (pref === 'system') return prefersDark() ? 'dark' : 'light';
  return pref;
}

function applyClass(resolved: Resolved) {
  const root = document.documentElement;
  root.classList.toggle('dark', resolved === 'dark');
  root.style.colorScheme = resolved;
}

interface ThemeState {
  theme: ThemePref;
  resolved: Resolved;
  init: () => void;
  setTheme: (t: ThemePref) => void;
  toggle: () => void;
}

export const useThemeStore = create<ThemeState>((set, get) => ({
  theme: 'system',
  resolved: 'light',
  init: () => {
    let pref: ThemePref = 'system';
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored === 'light' || stored === 'dark' || stored === 'system') pref = stored;
    } catch {
      /* localStorage 不可用时退回 system */
    }
    const resolved = resolve(pref);
    applyClass(resolved);
    set({ theme: pref, resolved });
    // 系统主题变化时，若用户偏好为 system 则跟随
    window
      .matchMedia('(prefers-color-scheme: dark)')
      .addEventListener('change', () => {
        if (get().theme === 'system') {
          const r = resolve('system');
          applyClass(r);
          set({ resolved: r });
        }
      });
  },
  setTheme: (t) => {
    try {
      localStorage.setItem(STORAGE_KEY, t);
    } catch {
      /* ignore */
    }
    const resolved = resolve(t);
    applyClass(resolved);
    set({ theme: t, resolved });
  },
  // 在亮/暗之间快速切换（system 视当前生效值决定方向）
  toggle: () => {
    const next: Resolved = get().resolved === 'dark' ? 'light' : 'dark';
    get().setTheme(next);
  },
}));
