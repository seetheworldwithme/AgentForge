// DTOs mirroring the core service's HTTP JSON shapes.
// Field names use snake_case to match the Go JSON tags in internal/server.

export interface Provider {
  id: string;
  name: string;
  base_url: string;
  api_key: string;
  chat_model: string;
  embed_model?: string;
  is_default: boolean;
}

export interface Session {
  id: string;
  title: string;
  provider_id: string;
  kb_id?: string;
  tools_enabled: boolean;
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
  chunk_size: number;
  chunk_overlap: number;
}

// Note: the core's GET /api/kb/{id}/documents returns store.Document structs
// *without* JSON tags, so the wire keys are the Go field names (PascalCase).
// The doc status endpoint (/status) uses explicit snake_case keys instead.
export interface Document {
  ID: string;        // from listDocs (un-tagged store.Document)
  KBID: string;
  Filename: string;
  FileSize: number;
  MimeType: string;
  Status: string;    // processing | ready | failed
  ChunkCount: number;
  Error?: string;
  CreatedAt?: string;
}

// Chat SSE event names emitted by the agent loop (internal/agent/agent.go).
export type ChatEventName =
  | 'started'
  | 'delta'
  | 'tool_call'
  | 'confirm_req'
  | 'tool_result'
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
