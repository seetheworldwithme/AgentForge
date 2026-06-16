import { useEffect, useState } from 'react';
import { useConfigStore } from '../stores/configStore';

export function ProviderSettings() {
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

  useEffect(() => {
    if (!loaded) load();
  }, [loaded, load]);

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
        <h3 className="font-medium">Add provider</h3>
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="Name"
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
        />
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="Base URL"
          value={form.base_url}
          onChange={(e) => setForm({ ...form, base_url: e.target.value })}
        />
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="API Key"
          type="password"
          value={form.api_key}
          onChange={(e) => setForm({ ...form, api_key: e.target.value })}
        />
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="Chat model"
          value={form.chat_model}
          onChange={(e) => setForm({ ...form, chat_model: e.target.value })}
        />
        <input
          className="w-full border rounded p-1 text-sm"
          placeholder="Embed model (optional)"
          value={form.embed_model}
          onChange={(e) => setForm({ ...form, embed_model: e.target.value })}
        />
        <label className="flex items-center gap-1 text-sm">
          <input
            type="checkbox"
            checked={form.is_default}
            onChange={(e) => setForm({ ...form, is_default: e.target.checked })}
          />
          Set as default (enables embedding/RAG)
        </label>
        <button
          className="bg-blue-600 text-white rounded px-3 py-1 text-sm"
          onClick={() => create(form)}
        >
          Save
        </button>
      </div>
    </div>
  );
}
