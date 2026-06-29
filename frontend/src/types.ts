// DTOs mirroring the core service's HTTP JSON shapes.
// Field names use snake_case to match the Go JSON tags in internal/server.

export interface Provider {
  id: string;
  name: string;
  base_url: string;
  api_key: string;
  chat_model: string;
  embed_model?: string;
  kind?: 'chat' | 'embed' | 'rerank'; // 后端持久化；空/缺省视为 chat（向后兼容老数据）
  vision?: boolean; // 视觉(VL)模型：允许在对话框粘贴图片
  context_window?: number; // 上下文窗口大小 tokens，0=未知用全局默认
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
  role: 'user' | 'assistant' | 'tool' | 'system' | 'summary';
  content: string;
  thinking?: string; // 推理模型的思考过程（reasoning_content），仅展示，不回传模型
  images?: string[]; // 用户消息附带的图片 dataURL（多模态）
  tool_calls?: string; // JSON
  tool_call_id?: string;
  citations?: string; // JSON
  tokens_in?: number;
  tokens_out?: number;
  tps?: number; // 本轮精确平均生成速率 tokens/s（done 事件携带，仅展示）
  variant?: 'warning'; // 非对话内容型消息：warning = 居中警告提示气泡
  created_at?: string; // 消息时间（RFC3339，后端 messageDTO.created_at；乐观消息用前端时间）
}

export interface KnowledgeBase {
  id: string;
  name: string;
  description?: string;
  embed_provider_id: string;
  chat_provider_id?: string;
  rerank_provider_id?: string;
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

// 工作目录下的文件/文件夹项(@ 文件 mention 菜单使用)。
export interface TreeItem {
  name: string;
  is_dir: boolean;
  path: string; // 相对工作目录的路径
}

// Chat SSE event names emitted by the agent loop (internal/agent/agent.go).
export type ChatEventName =
  | 'started'
  | 'user_saved'
  | 'delta'
  | 'thinking'
  | 'tool_call'
  | 'confirm_req'
  | 'ask_user_req'
  | 'tool_result'
  | 'status'
  | 'title'
  | 'error'
  | 'done'
  | 'todo';

export interface ChatEvent {
  event: ChatEventName;
  data: any;
}

// Payload of a confirm_req event (routed to the confirm store).
export interface ConfirmReq {
  request_id: string;
  tool: string;
  input: any;
  match_key_hint?: string;
}

// Payload of an ask_user_req event（routed to the ask store）：Agent 拿不准、需用户
// 拍板时发出的结构化提问，前端 AskUserDialog 让用户单选某项或填「其他」。
export interface AskReq {
  request_id: string;
  question: string;
  options: { label: string; description?: string }[];
}

// 跨会话记忆条目（镜像 internal/memory Entry 的 JSON）。
export type MemoryType = 'user' | 'feedback' | 'project' | 'reference';

export interface MemoryEntry {
  name: string;
  description: string;
  type: MemoryType;
  body: string;
  updated_at: string;
}

// 待办任务（镜像 internal/todo Task 的 JSON）。
export type TodoStatus = 'pending' | 'in_progress' | 'completed';

export interface TodoItem {
  id: number;
  subject: string;
  description?: string;
  active_form?: string;
  status: TodoStatus;
  blocks?: number[];
  blocked_by?: number[];
}
