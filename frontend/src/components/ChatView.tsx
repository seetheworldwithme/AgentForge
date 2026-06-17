import { useEffect, useRef } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { MessageBubble } from './MessageBubble';
import { ChatInput } from './ChatInput';

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
    <div className="flex flex-col flex-1 h-screen">
      <div className="flex-1 overflow-y-auto px-4">
        {messages.length === 0 && (
          <div className="text-center text-gray-400 mt-20">
            {currentId ? 'Start the conversation' : 'Select or create a session'}
          </div>
        )}
        {messages
          .filter((m) => m.role !== 'assistant' || m.content.trim().length > 0)
          .map((m) => (
            <MessageBubble key={m.id} m={m} />
          ))}
        {waiting && <TypingIndicator />}
        <div ref={bottomRef} />
      </div>
      <ChatInput sessionId={currentId} />
    </div>
  );
}

// 大模型思考中的动画指示器：三个错开节奏的弹跳小圆点，样式与 assistant 气泡一致。
function TypingIndicator() {
  return (
    <div className="my-2 flex justify-start">
      <div className="flex items-center gap-1.5 rounded-lg bg-gray-100 px-3 py-3">
        {[0, 150, 300].map((ms) => (
          <span
            key={ms}
            className="inline-block h-1.5 w-1.5 rounded-full bg-gray-400 animate-bounce"
            style={{ animationDelay: `${ms}ms` }}
          />
        ))}
      </div>
    </div>
  );
}
