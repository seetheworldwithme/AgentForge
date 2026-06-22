// DTOs mirroring the core service's HTTP JSON shapes.
// Field names use snake_case to match the Go JSON tags in internal/server.

export interface Provider {
  id: string;
  name: string;
  base_url: string;
  api_key: string;
  chat_model: string;
  embed_model?: string;
  kind?: 'chat' | 'embed'; // 后端持久化；空/缺省视为 chat（向后兼容老数据）
  is_default: boolean;
}

export interface Session {
  id: string;
  title: string;
  provider_id: string;
  kb_id?: string;
  tools_enabled: boolean;
  workdir?: string;
}

export interface Message {
  id: string;
  session_id: string;
  role: 'user' | 'assistant' | 'tool' | 'system';
  content: string;
  tool_calls?: string; // JSON
  tool_call_id?: string;
  citations?: string; // JSON
  tokens_in?: number;
  tokens_out?: number;
}

export interface KnowledgeBase {
  id: string;
  name: string;
  description?: string;
  embed_provider_id: string;
  chat_provider_id?: string;
  chunk_size: number;
  chunk_overlap: number;
  doc_count: number;
  created_at: string;
}

export interface Document {
  id: string;
  kb_id: string;
  filename: string;
  file_size: number;
  mime_type: string;
  status: 'processing' | 'ready' | 'failed' | string;
  chunk_count: number;
  error?: string;
  raw_path?: string;
  created_at?: string;
}

export interface ChunkPreview {
  id?: string;
  document_id?: string;
  kb_id?: string;
  ordinal: number;
  text: string;
  token_count?: number;
  metadata?: string;
}

export interface RetrieveHit {
  chunk_id: string;
  document_id: string;
  filename: string;
  ordinal: number;
  text: string;
  similarity: number;
}

export interface Skill {
  id: string;
  name: string;
  description: string;
  source: 'global' | 'project' | 'workspace' | string;
  path: string;
  enabled: boolean;
}

export interface MCPServer {
  id: string;
  name: string;
  transport: 'stdio' | 'sse' | string;
  command: string;
  args: string[];
  env: Record<string, string>;
  url: string;
  headers: Record<string, string>;
  enabled: boolean;
}

// Chat SSE event names emitted by the agent loop (internal/agent/agent.go).
export type ChatEventName =
  | 'started'
  | 'delta'
  | 'tool_call'
  | 'confirm_req'
  | 'tool_result'
  | 'title'
  | 'error'
  | 'done';

export interface ChatEvent {
  event: ChatEventName;
  data: any;
}

// Payload of a confirm_req event (routed to the confirm store).
export interface ConfirmReq {
  request_id: string;
  tool: string;
  input: any;
}
