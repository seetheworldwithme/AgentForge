import type { ReactNode } from 'react';
import { useTodoStore } from '../stores/todoStore';
import { Icon } from './Icon';
import type { TodoItem } from '../types';

// 依赖关系渲染：阻塞 #3；被 #1 阻塞。
function depText(t: TodoItem): string {
  const parts: string[] = [];
  if (t.blocks?.length) parts.push(`阻塞 #${t.blocks.join(',#')}`);
  if (t.blocked_by?.length) parts.push(`被 #${t.blocked_by.join(',#')} 阻塞`);
  return parts.join('；');
}

function Row({ t }: { t: TodoItem }) {
  const isDone = t.status === 'completed';
  const isProg = t.status === 'in_progress';
  return (
    <div className="flex items-start gap-2 rounded-md px-2 py-1.5 text-sm">
      <Icon
        name={isDone ? 'check' : isProg ? 'loader' : 'square'}
        size={14}
        className={
          isProg
            ? 'mt-0.5 shrink-0 text-primary'
            : 'mt-0.5 shrink-0 text-muted-foreground'
        }
      />
      <div className="min-w-0 flex-1">
        <div className={isDone ? 'truncate text-muted-foreground line-through' : 'truncate'}>
          <span className="text-muted-foreground">#{t.id}</span> {t.subject}
        </div>
        {depText(t) && <div className="truncate text-xs text-muted-foreground">{depText(t)}</div>}
      </div>
    </div>
  );
}

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="mb-1.5">
      <div className="px-2 py-1 text-xs font-medium text-muted-foreground">{label}</div>
      {children}
    </div>
  );
}

// TodoPanel：实时显示当前会话的待办进度看板。状态由后端 SSE "todo" 事件推送。
export function TodoPanel() {
  const items = useTodoStore((s) => s.items);
  const inProg = items.filter((t) => t.status === 'in_progress');
  const pending = items.filter((t) => t.status === 'pending');
  const done = items.filter((t) => t.status === 'completed');

  return (
    <div className="flex h-full w-72 shrink-0 flex-col border-l border-border bg-card">
      <div className="flex items-center gap-1.5 border-b border-border px-3 py-2 text-sm font-medium">
        <Icon name="check" size={15} /> 待办清单
        <span className="ml-auto text-xs text-muted-foreground">
          {done.length}/{items.length}
        </span>
      </div>
      <div className="flex-1 overflow-y-auto p-1">
        {items.length === 0 ? (
          <p className="px-3 py-4 text-xs leading-5 text-muted-foreground">
            暂无待办。复杂任务时，Agent 会用 todo_create 列出步骤，并在此实时显示进度。
          </p>
        ) : (
          <>
            {inProg.length > 0 && (
              <Group label={`进行中（${inProg.length}）`}>
                {inProg.map((t) => <Row key={t.id} t={t} />)}
              </Group>
            )}
            {pending.length > 0 && (
              <Group label={`待办（${pending.length}）`}>
                {pending.map((t) => <Row key={t.id} t={t} />)}
              </Group>
            )}
            {done.length > 0 && (
              <Group label={`已完成（${done.length}）`}>
                {done.map((t) => <Row key={t.id} t={t} />)}
              </Group>
            )}
          </>
        )}
      </div>
    </div>
  );
}
