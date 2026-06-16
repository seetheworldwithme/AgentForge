import { useEffect, useRef } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { MessageBubble } from './MessageBubble';
import { ChatInput } from './ChatInput';

export function ChatView() {
  const currentId = useSessionStore((s) => s.currentId);
  const messages = useSessionStore((s) => s.messages);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

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
        <div ref={bottomRef} />
      </div>
      <ChatInput sessionId={currentId} />
    </div>
  );
}
