import { useEffect } from 'react';
import { useConfirmModalStore } from '../stores/confirmModalStore';
import { Icon } from './Icon';

// 全局通用确认弹框：命令式触发，统一用于删除等不可逆操作的二次确认。
// 样式与 ConfirmDialog 保持一致；支持 ESC / 点击遮罩取消。
export function ConfirmModal() {
  const pending = useConfirmModalStore((s) => s.pending);
  const settle = useConfirmModalStore((s) => s.settle);

  useEffect(() => {
    if (!pending) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') settle(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [pending, settle]);

  if (!pending) return null;
  const danger = pending.danger !== false; // 默认危险样式

  return (
    <div
      className="fixed inset-0 z-[100] flex animate-fade-in items-center justify-center bg-black/50 backdrop-blur-sm"
      onClick={() => settle(false)}
    >
      <div
        className="w-[400px] max-w-[92vw] animate-scale-in rounded-2xl border border-border bg-card p-6 shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-start gap-3">
          <div
            className={
              'grid h-10 w-10 shrink-0 place-items-center rounded-full ' +
              (danger ? 'bg-destructive/10 text-destructive' : 'bg-primary/10 text-primary')
            }
          >
            <Icon name="alert-circle" size={18} />
          </div>
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-foreground">{pending.title}</h2>
            {pending.message && (
              <p className="mt-0.5 text-sm text-muted-foreground">{pending.message}</p>
            )}
          </div>
        </div>
        <div className="flex items-center justify-end gap-2">
          <button className="btn-outline gap-1.5" onClick={() => settle(false)}>
            {pending.cancelText ?? '取消'}
          </button>
          <button
            className={(danger ? 'btn-danger' : 'btn-primary') + ' gap-1.5'}
            onClick={() => settle(true)}
            autoFocus
          >
            <Icon name="check" size={15} strokeWidth={2.5} />
            {pending.confirmText ?? '删除'}
          </button>
        </div>
      </div>
    </div>
  );
}
