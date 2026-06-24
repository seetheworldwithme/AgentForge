import { useEffect, useRef, useState } from 'react';
import { useSessionStore } from '../stores/sessionStore';
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

export function MessageBubble({
  m,
  onRetry,
  showActions,
}: {
  m: Message;
  onRetry?: () => void;
  showActions?: boolean;
}) {
  const isUser = m.role === 'user';
  const isTool = m.role === 'tool';
  const isWarning = m.variant === 'warning';
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const content = m.role === 'assistant' ? m.content.trimStart() : m.content;

  // 思考过程折叠：仅 assistant 且有 thinking 时显示。流式思考中（正文还没出）默认展开并带
  // 三点动效；正文一开始即自动折叠。用户手动展开/收起后以用户操作为准，不再被自动覆盖。
  const streaming = useSessionStore((s) => s.streaming);
  const lastMsgId = useSessionStore((s) => s.messages[s.messages.length - 1]?.id);
  const isLive = streaming && lastMsgId === m.id;
  const isThinkingLive = isLive && m.role === 'assistant' && m.content.trim() === '' && !!m.thinking;
  const [thinkOpen, setThinkOpen] = useState(false);
  const userToggled = useRef(false);
  useEffect(() => {
    if (!m.thinking) return;
    if (userToggled.current) return; // 用户已手动操作，不再自动折叠/展开
    setThinkOpen(m.content.trim() === '');
  }, [m.thinking, m.content]);
  const toggleThink = () => {
    userToggled.current = true;
    setThinkOpen((v) => !v);
  };

  // 工具调用与结果以 ───────── 分隔，合并展示在一条 tool 消息里
  const sepIdx = isTool ? content.indexOf('\n─────────\n') : -1;
  const hasResult = sepIdx >= 0;
  const callPart = hasResult ? content.slice(0, sepIdx) : content;
  const resultPart = hasResult ? content.slice(sepIdx + '\n─────────\n'.length) : '';
  const collapsedCall = callPart.replace(/\s+/g, ' ').trim();

  if (isTool) {
    return (
      <div className="my-2">
        <div className="overflow-hidden rounded-xl border border-border bg-card text-sm shadow-sm">
          <button
            className="flex w-full items-start gap-2 px-3 py-2.5 text-left transition-colors hover:bg-muted/40"
            onClick={() => setExpanded(!expanded)}
          >
            <Icon name="wrench" size={14} className="mt-0.5 shrink-0 text-primary" />
            <pre className="min-w-0 flex-1 truncate font-mono text-xs leading-5 text-foreground/90">
              {collapsedCall}
            </pre>
            <Icon
              name="chevron-right"
              size={14}
              className={
                'mt-0.5 shrink-0 text-muted-foreground transition-transform duration-150 ' +
                (expanded ? 'rotate-90' : '')
              }
            />
          </button>
          {expanded && (
            <div className="border-t border-border bg-muted/40 px-3 py-2.5">
              <div className="mb-1 text-[10px] uppercase tracking-wide text-muted-foreground">
                命令{m.tool_call_id ? ` · #${m.tool_call_id}` : ''}
              </div>
              <pre className="mb-3 max-h-40 overflow-auto whitespace-pre-wrap break-all font-mono text-xs leading-5 text-foreground/80">
                {callPart}
              </pre>
              <div className="mb-1 text-[10px] uppercase tracking-wide text-muted-foreground">
                结果
              </div>
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all font-mono text-xs leading-5 text-foreground/70">
                {hasResult ? resultPart : '等待结果...'}
              </pre>
            </div>
          )}
        </div>
      </div>
    );
  }

  // 居中警告提示（如工具调用上限）：区别于普通对话气泡，用 amber 描边 + 图标。
  if (isWarning) {
    return (
      <div className="my-2.5 flex justify-center">
        <div className="flex max-w-[90%] items-start gap-2 rounded-xl border border-amber-400/50 bg-amber-400/10 px-3.5 py-2.5 text-sm text-amber-700 dark:text-amber-300">
          <Icon name="alert-circle" size={16} className="mt-0.5 shrink-0" />
          <span className="leading-6">{m.content}</span>
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
        {/* 思考过程折叠区（仅 assistant 且有 thinking）：思考中默认展开带三点，正文一到自动折叠 */}
        {!isUser && m.thinking && (
          <ThinkingBlock
            thinking={m.thinking}
            open={thinkOpen}
            live={isThinkingLive}
            onToggle={toggleThink}
          />
        )}
        {/* 正文气泡：assistant 思考中（正文为空）不显示空气泡，仅展示思考区 */}
        {!(m.role === 'assistant' && content.trim() === '') && (
          <div
            className={
              'break-words rounded-2xl px-4 py-2.5 text-sm leading-6 shadow-sm ' +
              (isUser
                ? 'whitespace-pre-wrap rounded-tr-sm bg-muted text-foreground selection:bg-sky-200 selection:text-sky-900'
                : 'markdown-body rounded-tl-sm border border-border bg-card text-foreground')
            }
          >
            {isUser ? content : <MarkdownMessage content={content} />}
          </div>
        )}
        {/* 用户消息附带的图片缩略图（Array.isArray 防御：历史数据 images 可能是字符串） */}
        {isUser && Array.isArray(m.images) && m.images.length > 0 && (
          <div className="mt-1.5 flex flex-wrap justify-end gap-1.5">
            {m.images.map((src, idx) => (
              <img
                key={idx}
                src={src}
                alt={`图片 ${idx + 1}`}
                loading="lazy"
                className="h-24 w-24 rounded-lg border border-border object-cover"
              />
            ))}
          </div>
        )}
        {!isUser && showActions && (
          <div className="mt-1.5 flex items-center justify-end gap-1 px-1">
            <button
              className="rounded px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(content);
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1200);
                } catch {
                  /* 剪贴板不可用时静默失败 */
                }
              }}
            >
              {copied ? 'Copied' : 'Copy'}
            </button>
            <button
              className="rounded px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              onClick={onRetry}
            >
              Retry
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

// 思考过程折叠块：标题（brain 图标 + 「思考过程」+ 思考中三点）+ 可展开的原始思考文本。
// 思考链是原始文本，用等宽 <pre> 展示，不走 markdown（避免渲染混乱）。
function ThinkingBlock({
  thinking,
  open,
  live,
  onToggle,
}: {
  thinking: string;
  open: boolean;
  live: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="mb-1.5 overflow-hidden rounded-xl border border-border bg-card text-sm shadow-sm">
      <button
        className="flex w-full items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-muted/40"
        onClick={onToggle}
      >
        <Icon name="brain" size={14} className="shrink-0 text-primary" />
        <span className="text-xs font-medium text-muted-foreground">思考过程</span>
        {live && <ThinkingDots />}
        <Icon
          name="chevron-right"
          size={14}
          className={
            'ml-auto shrink-0 text-muted-foreground transition-transform duration-150 ' +
            (open ? 'rotate-90' : '')
          }
        />
      </button>
      {open && (
        <div className="border-t border-border bg-muted/40 px-3 py-2.5">
          <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words font-mono text-xs leading-5 text-foreground/70">
            {thinking}
          </pre>
        </div>
      )}
    </div>
  );
}

// 思考中的三点动效（与 ChatView 的 TypingIndicator 同样式，但无 Avatar），
// 用于思考折叠标题尾部，提示「正在思考」。
function ThinkingDots() {
  return (
    <span className="ml-1 inline-flex items-center gap-1">
      {[0, 150, 300].map((ms) => (
        <span
          key={ms}
          className="h-1 w-1 animate-bounce rounded-full bg-muted-foreground/50"
          style={{ animationDelay: `${ms}ms` }}
        />
      ))}
    </span>
  );
}
