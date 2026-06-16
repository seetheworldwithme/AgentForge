import { useState } from 'react';
import type { Message } from '../types';

export function MessageBubble({ m }: { m: Message }) {
  const isUser = m.role === 'user';
  const isTool = m.role === 'tool';
  const [expanded, setExpanded] = useState(false);
  const content = m.role === 'assistant' ? m.content.trimStart() : m.content;

  // Split tool call + result when they're merged (separated by ─────────)
  const sepIdx = isTool ? content.indexOf('\n─────────\n') : -1;
  const hasResult = sepIdx >= 0;
  const callPart = hasResult ? content.slice(0, sepIdx) : content;
  const resultPart = hasResult ? content.slice(sepIdx + '\n─────────\n'.length) : '';

  return (
    <div className={`my-2 flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={
          'max-w-[80%] rounded-lg px-3 py-2 text-sm ' +
          (isUser
            ? 'bg-blue-600 text-white whitespace-pre-wrap'
            : isTool
              ? 'bg-gray-200 text-gray-800 font-mono text-xs'
              : 'bg-gray-100 text-gray-900 whitespace-pre-wrap')
        }
      >
        <div className="text-[10px] uppercase opacity-60 mb-1">
          {isTool ? `tool${m.tool_call_id ? ' ' + m.tool_call_id : ''}` : m.role}
        </div>
        {isTool && hasResult ? (
          <>
            <div className="flex items-start gap-1.5">
              <div className="whitespace-pre-wrap text-blue-700 flex-1">{callPart}</div>
              <button
                className="text-gray-400 hover:text-gray-600 flex-shrink-0 mt-0.5 leading-none"
                onClick={() => setExpanded(!expanded)}
                title={expanded ? '收起结果' : '展开结果'}
              >
                <span
                  className="inline-block transition-transform duration-200 text-[10px]"
                  style={{ transform: expanded ? 'rotate(90deg)' : '' }}
                >
                  ▶
                </span>
              </button>
            </div>
            {expanded && (
              <>
                <div className="border-t border-gray-300 my-1.5" />
                <div className="whitespace-pre-wrap text-gray-700">{resultPart}</div>
              </>
            )}
          </>
        ) : (
          <span className="whitespace-pre-wrap">{content}</span>
        )}
      </div>
    </div>
  );
}
