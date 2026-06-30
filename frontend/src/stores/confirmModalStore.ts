import { create } from 'zustand';

// 通用确认弹框的入参：标题、可选说明与按钮文案。
// danger=true 时确认按钮显示为危险（红色）样式，默认用于删除等不可逆操作。
export interface ConfirmOptions {
  title: string;
  message?: string;
  confirmText?: string;
  cancelText?: string;
  danger?: boolean;
}

interface Pending extends ConfirmOptions {
  resolve: (ok: boolean) => void;
}

interface ConfirmModalState {
  pending: Pending | null;
  // 命令式调用：返回 Promise<boolean>，用户确认 resolve(true)，取消 resolve(false)。
  confirm: (opts: ConfirmOptions) => Promise<boolean>;
  // 内部：供 ConfirmModal 在用户交互后清理。
  settle: (ok: boolean) => void;
}

export const useConfirmModalStore = create<ConfirmModalState>((set, get) => ({
  pending: null,
  confirm: (opts) =>
    new Promise<boolean>((resolve) => {
      // 同一时刻只展示一个弹框；若已有挂起，先把旧的当作取消结算。
      const cur = get().pending;
      if (cur) cur.resolve(false);
      set({ pending: { ...opts, resolve } });
    }),
  settle: (ok) => {
    const cur = get().pending;
    if (!cur) return;
    set({ pending: null });
    cur.resolve(ok);
  },
}));
