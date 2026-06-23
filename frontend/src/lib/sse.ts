import { baseUrl } from './port';
import type { ChatEvent, ChatEventName } from '../types';

// The chat endpoint is POST + SSE. EventSource only supports GET, so we use
// fetch with a streaming reader and parse the SSE frames ourselves.
export async function streamChat(
  sessionId: string,
  message: string,
  opts: {
    tools_enabled?: boolean;
    use_rag?: boolean;
    provider_id?: string;
    plan_mode?: boolean;
    skill_ids?: string[];
    mcp_server_ids?: string[];
    attachments?: string[];
  },
  onEvent: (e: ChatEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const b = await baseUrl();
  const r = await fetch(`${b}/api/sessions/${sessionId}/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, ...opts }),
    signal,
  });
  if (!r.ok || !r.body) throw new Error(`chat ${r.status}`);

  const reader = r.body.getReader();
  const dec = new TextDecoder();
  let buf = '';
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    // SSE frames are separated by a blank line.
    let idx;
    while ((idx = buf.indexOf('\n\n')) >= 0) {
      const block = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      const ev = parseSSEBlock(block);
      if (ev) onEvent(ev);
    }
  }
}

function parseSSEBlock(block: string): ChatEvent | null {
  let event: ChatEventName = 'message' as ChatEventName;
  let data = '';
  for (const line of block.split('\n')) {
    if (line.startsWith('event: ')) {
      event = line.slice(7) as ChatEventName;
    } else if (line.startsWith('data: ')) {
      data += line.slice(6);
    }
  }
  if (!data) return null;
  try {
    return { event, data: JSON.parse(data) };
  } catch {
    return null;
  }
}
