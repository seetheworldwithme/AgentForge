import { useEffect, useState } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { useConfigStore } from '../stores/configStore';
import type { Session } from '../types';

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
  const newChat = useSessionStore((s) => s.newChat);
  const providers = useConfigStore((s) => s.providers);
  const loaded = useConfigStore((s) => s.loaded);
  const loadProviders = useConfigStore((s) => s.load);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);
  useEffect(() => {
    if (!loaded) loadProviders();
  }, [loaded, loadProviders]);

  const handleNewChat = () => {
    // 没有 provider 时引导用户去设置；否则进入空白草稿态（不立即创建 session）。
    const def = providers.find((p) => p.is_default);
    const providerId = def ? def.id : providers[0]?.id;
    if (!providerId) {
      onOpenSettings();
      return;
    }
    newChat();
  };

  return (
    <div className="w-64 border-r flex flex-col bg-gray-50">
      <div className="p-3 flex gap-2">
        <button
          className="flex-1 bg-blue-600 text-white rounded py-2 text-sm"
          onClick={handleNewChat}
        >
          + 新对话
        </button>
      </div>
      <div className="flex-1 overflow-y-auto px-2">
        {sessions.map((s) => (
          <SessionRow key={s.id} session={s} active={s.id === currentId} />
        ))}
      </div>
      <div className="border-t p-2 flex gap-2">
        <button className="flex-1 text-sm border rounded py-1" onClick={onOpenKB}>
          Knowledge
        </button>
        <button className="flex-1 text-sm border rounded py-1" onClick={onOpenSettings}>
          设置
        </button>
      </div>
    </div>
  );
}

function SessionRow({ session, active }: { session: Session; active: boolean }) {
  const select = useSessionStore((s) => s.select);
  const remove = useSessionStore((s) => s.remove);
  const rename = useSessionStore((s) => s.rename);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(session.title);

  const startEdit = (e: React.MouseEvent) => {
    e.stopPropagation();
    setDraft(session.title);
    setEditing(true);
  };

  const commit = async () => {
    const t = draft.trim();
    setEditing(false);
    if (!t || t === session.title) return;
    try {
      await rename(session.id, t);
    } catch {
      /* keep current title on failure */
    }
  };

  if (editing) {
    return (
      <div className="flex items-center px-1 py-1 rounded text-sm bg-blue-50">
        <input
          autoFocus
          className="flex-1 border rounded px-1.5 py-1 text-sm outline-none"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              (e.target as HTMLInputElement).blur();
            } else if (e.key === 'Escape') {
              setEditing(false);
            }
          }}
          onClick={(e) => e.stopPropagation()}
        />
      </div>
    );
  }

  return (
    <div
      className={
        'group flex items-center px-2 py-2 rounded cursor-pointer text-sm ' +
        (active ? 'bg-blue-100' : 'hover:bg-gray-200')
      }
      onClick={() => select(session.id)}
    >
      <span className="flex-1 truncate">{session.title}</span>
      <button
        className="opacity-0 group-hover:opacity-100 text-gray-500 text-xs px-1"
        title="重命名"
        onClick={startEdit}
      >
        ✎
      </button>
      <button
        className="opacity-0 group-hover:opacity-100 text-red-500 text-xs px-1"
        title="删除"
        onClick={(e) => {
          e.stopPropagation();
          remove(session.id);
        }}
      >
        ×
      </button>
    </div>
  );
}
