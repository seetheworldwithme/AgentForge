import { useEffect, useRef } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { MessageBubble, Avatar } from './MessageBubble';
import { ChatInput } from './ChatInput';
import { Icon } from './Icon';

export function ChatView() {
  const currentId = useSessionStore((s) => s.currentId);
  const messages = useSessionStore((s) => s.messages);
  const streaming = useSessionStore((s) => s.streaming);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // 等待首字：正在流式输出，但最后一条 assistant 消息仍为空（首字未到，
  // 或多步工具调用后又进入思考）。
  const last = messages[messages.length - 1];
  const waiting = streaming && !!last && last.role === 'assistant' && last.content.trim() === '';

  return (
    <div className="flex h-full flex-1 flex-col">
      <div className="flex-1 overflow-y-auto">
        {messages.length === 0 ? (
          <EmptyState hasSession={!!currentId} />
        ) : (
          <div className="mx-auto max-w-3xl px-4 py-6">
            {messages
              .filter((m) => m.role !== 'assistant' || m.content.trim().length > 0)
              .map((m) => (
                <MessageBubble key={m.id} m={m} />
              ))}
            {waiting && <TypingIndicator />}
            <div ref={bottomRef} />
          </div>
        )}
      </div>
      <ChatInput sessionId={currentId} />
    </div>
  );
}

function EmptyState({ hasSession }: { hasSession: boolean }) {
  return (
    <div className="flex h-full flex-col items-center justify-center px-6 text-center">
      <div className="mb-4 grid h-14 w-14 place-items-center rounded-2xl bg-accent text-accent-foreground">
        <Icon name="sparkles" size={26} strokeWidth={1.75} />
      </div>
      <h2 className="text-lg font-semibold text-foreground">
        {hasSession ? '开始对话' : '选择或新建会话'}
      </h2>
      <p className="mt-1.5 max-w-xs text-sm leading-6 text-muted-foreground">
        {hasSession
          ? '在下方输入框发送消息，开始与模型对话。'
          : '从左侧选择一个会话，或点击「新对话」创建。'}
      </p>
    </div>
  );
}

// 大模型思考中的动画指示器：三个错开节奏的弹跳小圆点，样式与 assistant 气泡一致。
function TypingIndicator() {
  return (
    <div className="my-2.5 flex items-start gap-3">
      <Avatar role="assistant" />
      <div>
        <div className="mb-1 px-1 text-[11px] font-medium text-muted-foreground">Assistant</div>
        <div className="flex items-center gap-1.5 rounded-2xl rounded-tl-sm border border-border bg-card px-4 py-3.5 shadow-sm">
          {[0, 150, 300].map((ms) => (
            <span
              key={ms}
              className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground/50"
              style={{ animationDelay: `${ms}ms` }}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
