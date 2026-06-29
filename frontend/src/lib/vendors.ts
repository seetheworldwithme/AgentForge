// 预置厂商：选择后自动填充 base_url / 默认模型。
// 统一管理，供 ProviderSettings 表单和各下拉框（RAG、对话等）复用，
// 避免不同组件各自硬编码导致厂商名展示不一致。
export const VENDORS: {
  key: string;
  label: string;
  base_url: string;
  chat_model: string;
  embed_model: string;
  context_window: number;
}[] = [
  {
    key: 'openai',
    label: 'OpenAI',
    base_url: 'https://api.openai.com/v1',
    chat_model: 'gpt-4o-mini',
    embed_model: 'text-embedding-3-small',
    context_window: 128,
  },
  {
    key: 'deepseek',
    label: 'DeepSeek',
    base_url: 'https://api.deepseek.com/v1',
    chat_model: 'deepseek-chat',
    embed_model: '',
    context_window: 64,
  },
  {
    key: 'anthropic',
    label: 'Anthropic (Claude)',
    base_url: 'https://api.anthropic.com/v1',
    chat_model: 'claude-3-5-sonnet-20241022',
    embed_model: '',
    context_window: 200,
  },
  {
    key: 'siliconflow',
    label: '硅基流动 (SiliconFlow)',
    base_url: 'https://api.siliconflow.cn/v1',
    chat_model: 'Qwen/Qwen2.5-72B-Instruct',
    embed_model: 'BAAI/bge-m3',
    context_window: 131,
  },
  {
    key: 'zhipu-zai',
    label: '智谱 (z.ai)',
    base_url: 'https://api.z.ai/api/paas/v4',
    chat_model: 'glm-4-flash',
    embed_model: 'embedding-3',
    context_window: 128,
  },
  {
    key: 'zhipu-bigmodel',
    label: '智谱 (BigModel)',
    base_url: 'https://open.bigmodel.cn/api/paas/v4',
    chat_model: 'glm-4-flash',
    embed_model: 'embedding-3',
    context_window: 128,
  },
  {
    key: 'volcengine',
    label: '火山引擎 (豆包)',
    base_url: 'https://ark.cn-beijing.volces.com/api/v3',
    chat_model: 'doubao-1.5-pro-32k',
    embed_model: '',
    context_window: 32,
  },
  {
    key: 'qwen',
    label: '阿里云 (DashScope / 通义千问)',
    base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    chat_model: 'qwen-plus',
    embed_model: 'text-embedding-v2',
    context_window: 131,
  },
  {
    key: 'tencent-hunyuan',
    label: '腾讯云 (混元)',
    base_url: 'https://api.hunyuan.cloud.tencent.com/v1',
    chat_model: 'hunyuan-pro',
    embed_model: '',
    context_window: 32,
  },
  {
    key: 'minimax',
    label: 'MiniMax',
    base_url: 'https://api.minimaxi.com/v1',
    chat_model: 'MiniMax-M3',
    embed_model: '',
    context_window: 1000,
  },
  {
    key: 'xiaomi-mimo',
    label: '小米 MiMo',
    base_url: 'https://api.xiaomimimo.com/v1',
    chat_model: 'mimo-v2.5-pro',
    embed_model: '',
    context_window: 128,
  },
  {
    key: 'ollama',
    label: 'Ollama (本地)',
    base_url: 'http://localhost:11434/v1',
    chat_model: 'llama3.1',
    embed_model: 'nomic-embed-text',
    context_window: 8,
  },
  {
    key: 'custom',
    label: '自定义',
    base_url: '',
    chat_model: '',
    embed_model: '',
    context_window: 0,
  },
];

// vendorLabel 按 base_url 反查厂商展示名，匹配不到时回退 '自定义'。
// 统一所有组件的厂商名展示，不依赖 provider.name 字段（历史数据可能存的是模型名）。
export function vendorLabel(baseUrl: string): string {
  return VENDORS.find((v) => v.base_url === baseUrl)?.label ?? '自定义';
}
