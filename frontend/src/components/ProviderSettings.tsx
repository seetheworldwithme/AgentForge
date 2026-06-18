import { useEffect, useState } from 'react';
import { useConfigStore } from '../stores/configStore';
import { api } from '../lib/api';
import type { Provider } from '../types';

type Status =
  | { kind: 'idle' }
  | { kind: 'testing' }
  | { kind: 'error'; message: string }
  | { kind: 'success'; message: string };

// 预置厂商：选择后自动填充 base_url / 默认模型
const VENDORS: {
  key: string;
  label: string;
  base_url: string;
  chat_model: string;
  embed_model: string;
}[] = [
  {
    key: 'openai',
    label: 'OpenAI',
    base_url: 'https://api.openai.com/v1',
    chat_model: 'gpt-4o-mini',
    embed_model: 'text-embedding-3-small',
  },
  {
    key: 'deepseek',
    label: 'DeepSeek',
    base_url: 'https://api.deepseek.com/v1',
    chat_model: 'deepseek-chat',
    embed_model: '',
  },
  {
    key: 'anthropic',
    label: 'Anthropic (Claude)',
    base_url: 'https://api.anthropic.com/v1',
    chat_model: 'claude-3-5-sonnet-20241022',
    embed_model: '',
  },
  {
    key: 'qwen',
    label: '通义千问 (DashScope)',
    base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    chat_model: 'qwen-plus',
    embed_model: 'text-embedding-v2',
  },
  {
    key: 'custom',
    label: '自定义',
    base_url: '',
    chat_model: '',
    embed_model: '',
  },
];

const EMPTY = {
  name: '',
  base_url: 'https://api.openai.com/v1',
  api_key: '',
  chat_model: 'gpt-4o-mini',
  embed_model: 'text-embedding-3-small',
  vision_model: '',
  is_default: true,
};

type Form = typeof EMPTY;
type ModelCategory = 'chat' | 'embed';

export function ProviderSettings() {
  const providers = useConfigStore((s) => s.providers);
  const loaded = useConfigStore((s) => s.loaded);
  const load = useConfigStore((s) => s.load);
  const create = useConfigStore((s) => s.create);
  const update = useConfigStore((s) => s.update);
  const remove = useConfigStore((s) => s.remove);

  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<Form>(EMPTY);
  const [status, setStatus] = useState<Status>({ kind: 'idle' });
  // 控制弹窗（添加/编辑）的显隐
  const [modalOpen, setModalOpen] = useState(false);
  // 厂商下拉
  const [vendorKey, setVendorKey] = useState<string>('openai');
  // 模型类别：chat / embed
  const [category, setCategory] = useState<ModelCategory>('chat');
  const [titleProviderId, setTitleProviderId] = useState('');

  useEffect(() => {
    if (!loaded) load();
    api
      .getTitleProvider()
      .then((r) => setTitleProviderId(r.provider_id || ''))
      .catch(() => {});
  }, [loaded, load]);

  const resetForm = () => {
    setEditingId(null);
    setForm(EMPTY);
    setStatus({ kind: 'idle' });
    setVendorKey('openai');
    setCategory('chat');
  };

  const openAdd = () => {
    resetForm();
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    resetForm();
  };

  const startEdit = (p: Provider) => {
    setEditingId(p.id);
    setForm({
      name: p.name,
      base_url: p.base_url,
      api_key: p.api_key,
      chat_model: p.chat_model,
      embed_model: p.embed_model ?? '',
      vision_model: p.vision_model ?? '',
      is_default: p.is_default,
    });
    setStatus({ kind: 'idle' });
    // 尝试匹配已有厂商
    const matched = VENDORS.find((v) => v.base_url === p.base_url);
    setVendorKey(matched ? matched.key : 'custom');
    setCategory(p.embed_model ? 'embed' : 'chat');
    setModalOpen(true);
  };

  const setField = <K extends keyof Form>(key: K, value: Form[K]) => {
    setForm((f) => ({ ...f, [key]: value }));
    setStatus({ kind: 'idle' });
  };

  // 选择厂商时自动填充
  const onSelectVendor = (key: string) => {
    setVendorKey(key);
    const v = VENDORS.find((x) => x.key === key);
    if (!v) return;
    setForm((f) => ({
      ...f,
      name: f.name || v.label,
      base_url: v.base_url || f.base_url,
      chat_model: v.chat_model || f.chat_model,
      embed_model: v.embed_model || f.embed_model,
    }));
    setStatus({ kind: 'idle' });
  };

  const save = async () => {
    if (status.kind === 'testing') return;
    if (!form.name.trim()) {
      setStatus({ kind: 'error', message: '请填写名称' });
      return;
    }
    if (category === 'chat' && !form.chat_model.trim()) {
      setStatus({ kind: 'error', message: '请填写 Chat 模型' });
      return;
    }
    if (category === 'embed' && !form.embed_model.trim()) {
      setStatus({ kind: 'error', message: '请填写 Embed 模型' });
      return;
    }
    setStatus({ kind: 'testing' });
    // 保存前先测试连通性
    try {
      const res = await api.testProvider({
        base_url: form.base_url,
        api_key: form.api_key,
        chat_model: form.chat_model,
        embed_model: form.embed_model,
        kind: category,
      });
      if (!res.ok) {
        setStatus({ kind: 'error', message: res.error ?? '连通性测试失败' });
        return;
      }
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      return;
    }
    try {
      // 根据类别清洗：embed 模式下清空 chat_model？这里仍保留 chat_model
      // 以便兼容后端（后端 chat_model 必填），仅是 UI 引导不同
      const payload: Form = { ...form };
      if (editingId) {
        await update(editingId, payload);
        setStatus({ kind: 'success', message: '测试通过，已更新' });
      } else {
        await create(payload);
        setStatus({ kind: 'success', message: '测试通过，已保存' });
      }
      // 保存成功后关闭弹窗
      closeModal();
    } catch (e) {
      setStatus({ kind: 'error', message: '保存失败：' + (e instanceof Error ? e.message : String(e)) });
    }
  };

  const onDelete = async (id: string) => {
    if (id === editingId) resetForm();
    await remove(id);
  };

  return (
    <div className="space-y-5">
      <div>
        {/* 标题右侧增加"添加"按钮 */}
        <div className="flex items-center justify-between mb-2">
          <h2 className="text-lg font-semibold">已配置的模型</h2>
          <button
            className="bg-blue-600 text-white rounded px-3 py-1.5 text-sm hover:bg-blue-700"
            onClick={openAdd}
          >
            + 添加
          </button>
        </div>
        {providers.length === 0 ? (
          <p className="text-sm text-gray-500">还没有模型，点击右上角"添加"配置一个。</p>
        ) : (
          <div className="space-y-2">
            {providers.map((p) => (
              <div
                key={p.id}
                className="flex items-center gap-2 rounded border px-3 py-2 text-sm hover:bg-gray-50"
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium truncate">{p.name}</span>
                    {p.is_default && (
                      <span className="text-xs bg-blue-100 text-blue-700 rounded px-1.5 py-0.5">
                        默认
                      </span>
                    )}
                  </div>
                  <div className="text-xs text-gray-500 truncate">
                    {p.chat_model} · {p.base_url}
                  </div>
                </div>
                <button
                  className="text-xs border rounded px-2 py-1 hover:bg-gray-100"
                  onClick={() => startEdit(p)}
                >
                  编辑
                </button>
                <button
                  className="text-xs text-red-600 border border-red-200 rounded px-2 py-1 hover:bg-red-50"
                  onClick={() => onDelete(p.id)}
                >
                  删除
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* 标题生成模型：独立 provider，与主对话并行、互不卡顿 */}
      <div className="rounded border p-3 text-sm">
        <label className="block text-xs text-gray-600 mb-1">标题生成模型</label>
        <select
          className="border rounded p-1.5 text-sm w-full"
          value={titleProviderId}
          onChange={async (e) => {
            const v = e.target.value;
            setTitleProviderId(v);
            try {
              await api.setTitleProvider(v);
              setStatus({ kind: 'success', message: '标题模型已更新' });
            } catch {
              setStatus({ kind: 'error', message: '标题模型保存失败' });
            }
          }}
        >
          <option value="">未设置（跟随各会话的主对话模型）</option>
          {providers.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name} · {p.chat_model}
            </option>
          ))}
        </select>
        <p className="text-xs text-gray-400 mt-1">
          用独立的模型生成会话标题，与主对话并行、互不卡顿。建议选一个快的小模型。
        </p>
      </div>

      {/* 添加 / 编辑 弹窗 */}
      {modalOpen && (
        <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg w-[520px] max-w-[92vw] flex flex-col overflow-hidden">
            <div className="flex items-center justify-between px-5 py-3 border-b">
              <h3 className="font-semibold">
                {editingId ? '编辑模型（保存前自动测试连通性）' : '添加模型（保存前自动测试连通性）'}
              </h3>
              <button
                className="text-gray-500 hover:text-gray-800 text-2xl leading-none"
                onClick={closeModal}
                aria-label="关闭"
              >
                ×
              </button>
            </div>

            <div className="p-5 space-y-3">
              {/* 厂商下拉 */}
              <div>
                <label className="block text-xs text-gray-600 mb-1">模型厂商</label>
                <select
                  className="border rounded p-1.5 text-sm w-full"
                  value={vendorKey}
                  onChange={(e) => onSelectVendor(e.target.value)}
                >
                  {VENDORS.map((v) => (
                    <option key={v.key} value={v.key}>
                      {v.label}
                    </option>
                  ))}
                </select>
              </div>

              {/* 模型类别：chat / embed */}
              <div>
                <label className="block text-xs text-gray-600 mb-1">模型类别</label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    className={
                      'flex-1 border rounded py-1.5 text-sm ' +
                      (category === 'chat'
                        ? 'bg-blue-50 border-blue-500 text-blue-700'
                        : 'hover:bg-gray-50')
                    }
                    onClick={() => setCategory('chat')}
                  >
                    Chat（对话）
                  </button>
                  <button
                    type="button"
                    className={
                      'flex-1 border rounded py-1.5 text-sm ' +
                      (category === 'embed'
                        ? 'bg-blue-50 border-blue-500 text-blue-700'
                        : 'hover:bg-gray-50')
                    }
                    onClick={() => setCategory('embed')}
                  >
                    Embed（向量）
                  </button>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-2">
                <input
                  className="border rounded p-1.5 text-sm col-span-2"
                  placeholder="名称（如 OpenAI）"
                  value={form.name}
                  onChange={(e) => setField('name', e.target.value)}
                />
                <input
                  className="border rounded p-1.5 text-sm col-span-2"
                  placeholder="Base URL"
                  value={form.base_url}
                  onChange={(e) => setField('base_url', e.target.value)}
                />
                <input
                  className="border rounded p-1.5 text-sm col-span-2"
                  placeholder="API Key"
                  type="password"
                  value={form.api_key}
                  onChange={(e) => setField('api_key', e.target.value)}
                />
                {category === 'chat' ? (
                  <input
                    className="border rounded p-1.5 text-sm col-span-2"
                    placeholder="Chat model"
                    value={form.chat_model}
                    onChange={(e) => setField('chat_model', e.target.value)}
                  />
                ) : (
                  <input
                    className="border rounded p-1.5 text-sm col-span-2"
                    placeholder="Embed model"
                    value={form.embed_model}
                    onChange={(e) => setField('embed_model', e.target.value)}
                  />
                )}
              </div>

              <input
                className="border rounded p-1.5 text-sm"
                placeholder="Vision model（可选，知识库图片描述用，需支持 vision 如 gpt-4o）"
                value={form.vision_model}
                onChange={(e) => setField('vision_model', e.target.value)}
              />

              <label className="flex items-center gap-1.5 text-sm">
                <input
                  type="checkbox"
                  checked={form.is_default}
                  onChange={(e) => setField('is_default', e.target.checked)}
                />
                设为默认（启用 embedding / RAG）
              </label>

              {status.kind === 'error' && (
                <div className="rounded bg-red-50 border border-red-200 text-red-700 text-sm p-2 whitespace-pre-wrap break-all">
                  ✗ {status.message}
                </div>
              )}
              {status.kind === 'success' && (
                <div className="rounded bg-green-50 border border-green-200 text-green-700 text-sm p-2">
                  ✓ {status.message}
                </div>
              )}
            </div>

            <div className="flex gap-2 px-5 py-3 border-t justify-end">
              <button
                className="border rounded px-3 py-1.5 text-sm"
                onClick={closeModal}
              >
                取消
              </button>
              <button
                className="bg-blue-600 text-white rounded px-3 py-1.5 text-sm disabled:opacity-50"
                onClick={save}
                disabled={status.kind === 'testing'}
              >
                {status.kind === 'testing'
                  ? '测试中…'
                  : editingId
                    ? '测试并更新'
                    : '测试并保存'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
