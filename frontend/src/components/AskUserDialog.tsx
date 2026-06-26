import { useState } from 'react';
import { useAskStore } from '../stores/askStore';
import { Icon } from './Icon';
import type { AskReq } from '../types';

// AskUserDialog 渲染 Agent 经 ask_user_req 发来的结构化提问：单选选项 + 「其他」自定义输入。
// 与 ConfirmDialog 对称——Confirm 处理工具权限的 允许/拒绝，这里处理业务决策的单选。
// 用 key={request_id} 让每个问题全新挂载，选项/输入状态自然重置，不串到下一题。
export function AskUserDialog() {
  const pending = useAskStore((s) => s.pending);
  if (pending.length === 0) return null;
  return <AskPanel key={pending[0].request_id} req={pending[0]} />;
}

function AskPanel({ req }: { req: AskReq }) {
  const respond = useAskStore((s) => s.respond);
  const [selected, setSelected] = useState<string | null>(null); // option label，或 '__other__'
  const [other, setOther] = useState('');
  const useOther = selected === '__other__';
  const canConfirm =
    selected !== null && (selected !== '__other__' || other.trim() !== '');

  const confirm = () => {
    if (selected === '__other__') respond(req.request_id, { other: other.trim() });
    else if (selected) respond(req.request_id, { selection: selected });
  };

  return (
    <div className="fixed inset-0 z-[100] flex animate-fade-in items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="w-[460px] max-w-[92vw] animate-scale-in rounded-2xl border border-border bg-card p-6 shadow-lg">
        <div className="mb-4 flex items-start gap-3">
          <div className="grid h-10 w-10 shrink-0 place-items-center rounded-full bg-primary/10 text-primary">
            <Icon name="message-square" size={18} />
          </div>
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-foreground">Agent 想确认一下</h2>
            <p className="mt-1 whitespace-pre-wrap break-words text-sm text-foreground/90">
              {req.question}
            </p>
          </div>
        </div>

        <div className="mb-4 flex flex-col gap-1.5">
          {req.options.map((opt) => (
            <OptionRow
              key={opt.label}
              active={selected === opt.label}
              label={opt.label}
              description={opt.description}
              onSelect={() => {
                setSelected(opt.label);
                setOther('');
              }}
            />
          ))}

          <OptionRow
            active={useOther}
            label="其他"
            onSelect={() => setSelected('__other__')}
          />
          {useOther && (
            <input
              autoFocus
              value={other}
              onChange={(e) => setOther(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && canConfirm) confirm();
              }}
              placeholder="输入你的选择…"
              className="ml-6 w-[calc(100%-1.5rem)] rounded-md border border-border bg-background px-2.5 py-1.5 text-sm text-foreground outline-none focus:border-primary"
            />
          )}
        </div>

        <div className="flex items-center justify-end gap-2">
          <button
            className="btn-outline gap-1.5 text-muted-foreground"
            onClick={() => respond(req.request_id, { canceled: true })}
          >
            <Icon name="x" size={15} />
            取消
          </button>
          <button
            className="btn-primary gap-1.5 disabled:opacity-40"
            onClick={confirm}
            disabled={!canConfirm}
          >
            <Icon name="check" size={15} strokeWidth={2.5} />
            确认
          </button>
        </div>
      </div>
    </div>
  );
}

// OptionRow 是一个单选项：圆形 radio 指示 + label + 可选 description 次行。
function OptionRow({
  active,
  label,
  description,
  onSelect,
}: {
  active: boolean;
  label: string;
  description?: string;
  onSelect: () => void;
}) {
  return (
    <button
      onClick={onSelect}
      className={
        'flex w-full items-start gap-2.5 rounded-lg border px-3 py-2 text-left transition-colors ' +
        (active
          ? 'border-primary bg-primary/5'
          : 'border-border hover:border-primary/50 hover:bg-muted/40')
      }
    >
      <span
        className={
          'mt-0.5 grid h-4 w-4 shrink-0 place-items-center rounded-full border ' +
          (active ? 'border-primary' : 'border-muted-foreground/40')
        }
      >
        {active && <span className="h-2 w-2 rounded-full bg-primary" />}
      </span>
      <span className="min-w-0">
        <span className="block text-sm font-medium text-foreground">{label}</span>
        {description && (
          <span className="mt-0.5 block text-xs text-muted-foreground">{description}</span>
        )}
      </span>
    </button>
  );
}
