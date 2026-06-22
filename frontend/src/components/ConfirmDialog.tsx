import { useConfirmStore } from '../stores/confirmStore';
import { Icon } from './Icon';

export function ConfirmDialog() {
  const pending = useConfirmStore((s) => s.pending);
  const respond = useConfirmStore((s) => s.respond);
  if (pending.length === 0) return null;
  const req = pending[0];
  return (
    <div className="fixed inset-0 z-[100] flex animate-fade-in items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="w-[460px] max-w-[92vw] animate-scale-in rounded-2xl border border-border bg-card p-6 shadow-lg">
        <div className="mb-4 flex items-start gap-3">
          <div className="grid h-10 w-10 shrink-0 place-items-center rounded-full bg-primary/10 text-primary">
            <Icon name="wrench" size={18} />
          </div>
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-foreground">允许执行此工具？</h2>
            <p className="mt-0.5 text-sm text-muted-foreground">
              Agent 请求执行以下操作，请确认。
            </p>
          </div>
        </div>
        <div className="mb-5 overflow-hidden rounded-lg border border-border bg-muted/50">
          <div className="border-b border-border px-3 py-1.5 font-mono text-xs font-semibold text-foreground">
            {req.tool}
          </div>
          <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all px-3 py-2.5 font-mono text-xs leading-5 text-foreground/80">
            {JSON.stringify(req.input, null, 2)}
          </pre>
        </div>
        <div className="flex items-center justify-end gap-2">
          <button
            className="btn-outline gap-1.5 text-muted-foreground"
            onClick={() => respond(req.request_id, 'deny', 'never')}
          >
            <Icon name="x" size={15} />
            拒绝
          </button>
          <button
            className="btn-outline gap-1.5"
            onClick={() => respond(req.request_id, 'allow', 'session')}
            title={req.match_key_hint ? `本会话允许 ${req.match_key_hint}` : undefined}
          >
            {req.match_key_hint ? `本会话允许 ${req.match_key_hint}` : '本会话允许'}
          </button>
          <button
            className="btn-primary gap-1.5"
            onClick={() => respond(req.request_id, 'allow', 'never')}
          >
            <Icon name="check" size={15} strokeWidth={2.5} />
            仅本次允许
          </button>
        </div>
      </div>
    </div>
  );
}
