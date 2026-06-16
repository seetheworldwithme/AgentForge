import { useEffect, useState } from 'react';
import { useConfigStore } from '../stores/configStore';
import { api } from '../lib/api';

type Status =
  | { kind: 'idle' }
  | { kind: 'testing' }
  | { kind: 'error'; message: string }
  | { kind: 'success'; message: string };

export function ProviderSettings({ onSaved }: { onSaved?: () => void }) {
  const providers = useConfigStore((s) => s.providers);
  const loaded = useConfigStore((s) => s.loaded);
  const load = useConfigStore((s) => s.load);
  const create = useConfigStore((s) => s.create);
  const remove = useConfigStore((s) => s.remove);
  const [form, setForm] = useState({
    name: '',
    base_url: 'https://api.openai.com/v1',
    api_key: '',
    chat_model: 'gpt-4o-mini',
    embed_model: 'text-embedding-3-small',
    is_default: true,
  });
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  useEffect(() => {
    if (!loaded) load();
  }, [loaded, load]);

  const save = async () => {
    if (status.kind === 'testing') return;
    setStatus({ kind: 'testing' });
    // Validate connectivity first so the user gets immediate feedback and we
    // never persist a broken provider. The test endpoint sends one chat
    // completion; it returns 200 with {ok,error?} for both pass and fail.
    try {
      const res = await api.testProvider({
        base_url: form.base_url,
        api_key: form.api_key,
        chat_model: form.chat_model,
      });
      if (!res.ok) {
        setStatus({ kind: 'error', message: res.error ?? '连通性测试失败' });
        return;
      }
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      return;
    }
    // test passed → persist, then auto-close the modal
    try {
      await create(form);
      setStatus({ kind: 'success', message: '测试通过，已保存' });
      onSaved?.();
    } catch (e) {
      setStatus({ kind: 'error', message: '保存失败：' + (e instanceof Error ? e.message : String(e)) });
    }
  };

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">Providers</h2>
      <div className="space-y-1">
        {providers.map((p) => (
          <div key={p.id} className="flex items-center gap-2 text-sm">
            <span className="flex-1">
              {p.name} — {p.chat_model}
            </span>
            <button className="text-red-500" onClick={() => remove(p.id)}>
              Delete
            </button>
          </div>
        ))}
      </div>
      <div className="border-t pt-3 space-y-2">
        <h3 className="font-medium">添加供应商 (保存前自动测试连通性)</h3>
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="Name"
          value={form.name}
          onChange={(e) => {
            setForm({ ...form, name: e.target.value });
            setStatus({ kind: 'idle' });
          }}
        />
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="Base URL"
          value={form.base_url}
          onChange={(e) => {
            setForm({ ...form, base_url: e.target.value });
            setStatus({ kind: 'idle' });
          }}
        />
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="API Key"
          type="password"
          value={form.api_key}
          onChange={(e) => {
            setForm({ ...form, api_key: e.target.value });
            setStatus({ kind: 'idle' });
          }}
        />
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="Chat model"
          value={form.chat_model}
          onChange={(e) => {
            setForm({ ...form, chat_model: e.target.value });
            setStatus({ kind: 'idle' });
          }}
        />
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="Embed model (optional)"
          value={form.embed_model}
          onChange={(e) => {
            setForm({ ...form, embed_model: e.target.value });
            setStatus({ kind: 'idle' });
          }}
        />
        <label className="flex items-center gap-1 text-sm">
          <input
            type="checkbox"
            checked={form.is_default}
            onChange={(e) => setForm({ ...form, is_default: e.target.checked })}
          />
          设为默认 (启用 embedding/RAG)
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

        <button
          className="bg-blue-600 text-white rounded px-3 py-1 text-sm disabled:opacity-50"
          onClick={save}
          disabled={status.kind === 'testing'}
        >
          {status.kind === 'testing' ? '测试中…' : '测试并保存'}
        </button>
      </div>
    </div>
  );
}
