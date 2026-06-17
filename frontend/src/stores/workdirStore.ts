import { create } from 'zustand';
import { api } from '../lib/api';

// Shared current working directory. Lifted out of ChatInput so the Sidebar can
// group sessions under it and react to changes. Mirrors configStore's `loaded`
// guard so any component can trigger a one-time load.
interface WorkDirState {
  workdir: string;
  loaded: boolean;
  load: () => Promise<void>;
  setWorkDir: (dir: string) => Promise<void>;
}

export const useWorkDirStore = create<WorkDirState>((set) => ({
  workdir: '',
  loaded: false,
  load: async () => {
    try {
      const r = await api.getWorkDir();
      set({ workdir: r.workdir || '', loaded: true });
    } catch {
      set({ loaded: true });
    }
  },
  setWorkDir: async (dir) => {
    await api.setWorkDir(dir);
    set({ workdir: dir });
  },
}));
