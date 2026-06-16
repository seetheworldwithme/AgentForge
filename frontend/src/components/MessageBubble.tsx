import type { Message } from '../types';

export function MessageBubble({ m }: { m: Message }) {
  const isUser = m.role === 'user';
  const isTool = m.role === 'tool';
  return (
    <div className={`my-2 flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={
          'max-w-[80%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap ' +
          (isUser
            ? 'bg-blue-600 text-white'
            : isTool
              ? 'bg-gray-200 text-gray-800 font-mono text-xs'
              : 'bg-gray-100 text-gray-900')
        }
      >
        <div className="text-[10px] uppercase opacity-60 mb-1">
          {isTool ? `tool${m.tool_call_id ? ' ' + m.tool_call_id : ''}` : m.role}
        </div>
        {m.content}
      </div>
    </div>
  );
}
