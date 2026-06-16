import { useEffect, useState } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { useConfigStore } from '../stores/configStore';

export function Sidebar({
  onOpenSettings,
  onOpenKB,
}: {
  onOpenSettings: () => void;
  onOpenKB: () => void;
}) {
  const sessions = useSessionStore((s) => s.sessions);
  const currentId = useSessionStore((s) => s.currentId);
  const loadSessions = useSessionStore((s) => s.loadSessions);
  const select = useSessionStore((s) => s.select);
  const create = useSessionStore((s) => s.create);
  const remove = useSessionStore((s) => s.remove);
  const providers = useConfigStore((s) => s.providers);
  const [providerId, setProviderId] = useState<string>('');

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);
  useEffect(() => {
    if (providers[0]) setProviderId(providers[0].id);
  }, [providers]);

  const newChat = async () => {
    if (!providerId) {
      onOpenSettings();
      return;
    }
    await create({ title: '新对话', provider_id: providerId, tools_enabled: true });
  };

  return (
    <div className="w-64 border-r flex flex-col bg-gray-50">
      <div className="p-3 flex gap-2">
        <button
          className="flex-1 bg-blue-600 text-white rounded py-2 text-sm"
          onClick={newChat}
        >
          + New Chat
        </button>
      </div>
      <select
        className="mx-3 mb-2 border rounded p-1 text-sm"
        value={providerId}
        onChange={(e) => setProviderId(e.target.value)}
      >
        {providers.length === 0 && <option value="">No provider — configure</option>}
        {providers.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </select>
      <div className="flex-1 overflow-y-auto px-2">
        {sessions.map((s) => (
          <div
            key={s.id}
            className={
              'group flex items-center px-2 py-2 rounded cursor-pointer text-sm ' +
              (s.id === currentId ? 'bg-blue-100' : 'hover:bg-gray-200')
            }
            onClick={() => select(s.id)}
          >
            <span className="flex-1 truncate">{s.title}</span>
            <button
              className="opacity-0 group-hover:opacity-100 text-red-500 text-xs"
              onClick={(e) => {
                e.stopPropagation();
                remove(s.id);
              }}
            >
              ×
            </button>
          </div>
        ))}
      </div>
      <div className="border-t p-2 flex gap-2">
        <button className="flex-1 text-sm border rounded py-1" onClick={onOpenKB}>
          Knowledge
        </button>
        <button className="flex-1 text-sm border rounded py-1" onClick={onOpenSettings}>
          Settings
        </button>
      </div>
    </div>
  );
}
