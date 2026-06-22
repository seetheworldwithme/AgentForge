import { useState } from 'react';
import { Icon } from './Icon';
import { MarkdownMessage } from './MarkdownMessage';
import type { Message } from '../types';

// 角色 Avatar：用户=primary 圆头像，助手=描边卡片 + bot 图标。供消息气泡与输入指示器复用。
export function Avatar({
  role,
  size = 28,
  className = '',
}: {
  role: 'user' | 'assistant' | string;
  size?: number;
  className?: string;
}) {
  const isUser = role === 'user';
  return (
    <div
      className={
        'grid shrink-0 place-items-center rounded-full ' +
        (isUser ? 'bg-primary text-primary-foreground' : 'border border-border bg-card text-primary') +
        ' ' +
        className
      }
      style={{ width: size, height: size }}
    >
      <Icon name={isUser ? 'user' : 'bot'} size={Math.round(size * 0.58)} strokeWidth={2} />
    </div>
  );
}

export function MessageBubble({ m }: { m: Message }) {
  const isUser = m.role === 'user';
  const isTool = m.role === 'tool';
  const [expanded, setExpanded] = useState(false);
  const content = m.role === 'assistant' ? m.content.trimStart() : m.content;

  // 工具调用与结果以 ───────── 分隔，合并展示在一条 tool 消息里
  const sepIdx = isTool ? content.indexOf('\n─────────\n') : -1;
  const hasResult = sepIdx >= 0;
  const callPart = hasResult ? content.slice(0, sepIdx) : content;
  const resultPart = hasResult ? content.slice(sepIdx + '\n─────────\n'.length) : '';

  if (isTool) {
    return (
      <div className="my-2">
        <div className="overflow-hidden rounded-xl border border-border bg-card text-sm shadow-sm">
          <button
            className="flex w-full items-start gap-2 px-3 py-2.5 text-left transition-colors hover:bg-muted/40"
            onClick={() => hasResult && setExpanded(!expanded)}
          >
            <Icon name="wrench" size={14} className="mt-0.5 shrink-0 text-primary" />
            <pre className="flex-1 whitespace-pre-wrap break-all font-mono text-xs leading-5 text-foreground/90">
              {callPart}
            </pre>
            {hasResult && (
              <Icon
                name="chevron-right"
                size={14}
                className={
                  'mt-0.5 shrink-0 text-muted-foreground transition-transform duration-150 ' +
                  (expanded ? 'rotate-90' : '')
                }
              />
            )}
          </button>
          {hasResult && expanded && (
            <div className="border-t border-border bg-muted/40 px-3 py-2.5">
              <div className="mb-1 text-[10px] uppercase tracking-wide text-muted-foreground">
                结果{m.tool_call_id ? ` · #${m.tool_call_id}` : ''}
              </div>
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all font-mono text-xs leading-5 text-foreground/70">
                {resultPart}
              </pre>
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className={'my-2.5 flex items-start gap-3 ' + (isUser ? 'flex-row-reverse' : '')}>
      <Avatar role={m.role} />
      <div className={'max-w-[80%] ' + (isUser ? 'flex flex-col items-end' : '')}>
        <div className="mb-1 px-1 text-[11px] font-medium text-muted-foreground">
          {isUser ? '你' : 'Assistant'}
        </div>
        <div
          className={
            'break-words rounded-2xl px-4 py-2.5 text-sm leading-6 shadow-sm ' +
            (isUser
              ? 'whitespace-pre-wrap rounded-tr-sm bg-primary text-primary-foreground'
              : 'markdown-body rounded-tl-sm border border-border bg-card text-foreground')
          }
        >
          {isUser ? content : <MarkdownMessage content={content} />}
        </div>
      </div>
    </div>
  );
}
