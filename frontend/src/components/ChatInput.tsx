import { useState } from 'react';
import { useSessionStore } from '../stores/sessionStore';

export function ChatInput({ sessionId }: { sessionId: string | null }) {
  const [text, setText] = useState('');
  const [tools, setTools] = useState(true);
  const [rag, setRag] = useState(false);
  const send = useSessionStore((s) => s.send);
  const streaming = useSessionStore((s) => s.streaming);

  const submit = () => {
    if (!text.trim() || !sessionId || streaming) return;
    send(text, { tools_enabled: tools, use_rag: rag });
    setText('');
  };

  return (
    <div className="border-t p-3">
      <div className="mb-2 flex gap-4 text-sm">
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={tools} onChange={(e) => setTools(e.target.checked)} />
          Tools
        </label>
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={rag} onChange={(e) => setRag(e.target.checked)} />
          RAG
        </label>
      </div>
      <div className="flex gap-2">
        <textarea
          className="flex-1 border rounded p-2 text-sm resize-none"
          rows={2}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          placeholder="Send a message…"
        />
        <button
          className="bg-blue-600 text-white px-4 rounded disabled:opacity-50"
          onClick={submit}
          disabled={!text.trim() || streaming || !sessionId}
        >
          Send
        </button>
      </div>
    </div>
  );
}
