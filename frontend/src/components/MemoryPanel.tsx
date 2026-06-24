import { useEffect, useMemo, useState } from 'react';
import { Icon } from './Icon';
import { useMemoryStore } from '../stores/memoryStore';
import type { MemoryEntry, MemoryType } from '../types';

const TYPE_LABEL: Record<MemoryType, string> = {
  user: '用户偏好',
  feedback: '工作指导',
  project: '项目约束',
  reference: '外部资源',
};

const TYPE_ORDER: MemoryType[] = ['user', 'feedback', 'project', 'reference'];

type Draft = { name: string; description: string; type: MemoryType; body: string };

const emptyDraft = (): Draft => ({ name: '', description: '', type: 'user', body: '' });

function toDraft(e: MemoryEntry): Draft {
  return { name: e.name, description: e.description, type: e.type, body: e.body };
}

export function MemoryPanel() {
  const { entries, loaded, load, save, remove } = useMemoryStore();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [isNew, setIsNew] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    load();
  }, [load]);

  const grouped = useMemo(() => {
    const m: Record<MemoryType, MemoryEntry[]> = { user: [], feedback: [], project: [], reference: [] };
    for (const e of entries) if (m[e.type]) m[e.type].push(e);
    return m;
  }, [entries]);

  const select = (e: MemoryEntry) => {
    setDraft(toDraft(e));
    setIsNew(false);
    setError('');
  };

  const startNew = () => {
    setDraft(emptyDraft());
    setIsNew(true);
    setError('');
  };

  const submit = async () => {
    if (!draft) return;
    setSaving(true);
    setError('');
    try {
      await save(draft.name, { description: draft.description, type: draft.type, body: draft.body });
      setDraft(null);
      setIsNew(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const del = async (name: string) => {
    if (!confirm(`删除记忆「${name}」？`)) return;
    await remove(name);
    if (draft?.name === name) setDraft(null);
  };

  if (!loaded) {
    return <div className="p-4 text-sm text-muted-foreground">加载中…</div>;
  }

  return (
    <div className="flex h-full min-h-0">
      {/* 左：按 type 分组的列表 */}
      <div className="flex w-56 shrink-0 flex-col overflow-y-auto border-r border-border pr-1">
        <button
          className="btn btn-primary mb-2 flex items-center justify-center gap-1.5 py-1.5 text-sm"
          onClick={startNew}
        >
          <Icon name="plus" size={14} /> 新建记忆
        </button>
        {entries.length === 0 && (
          <p className="px-1 py-2 text-xs text-muted-foreground">还没有记忆。Agent 会在对话中自动记录。</p>
        )}
        {TYPE_ORDER.map((t) =>
          grouped[t].length === 0 ? null : (
            <div key={t} className="mb-2">
              <div className="px-1 py-1 text-xs font-medium text-muted-foreground">{TYPE_LABEL[t]}</div>
              {grouped[t].map((e) => (
                <button
                  key={e.name}
                  className={
                    'group flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors ' +
                    (draft?.name === e.name && !isNew
                      ? 'bg-card font-medium text-foreground shadow-sm'
                      : 'text-foreground/80 hover:bg-muted')
                  }
                  onClick={() => select(e)}
                >
                  <span className="flex-1 truncate">{e.description || e.name}</span>
                  <span
                    className="opacity-0 transition-opacity group-hover:opacity-100"
                    onClick={(ev) => {
                      ev.stopPropagation();
                      del(e.name);
                    }}
                  >
                    <Icon name="trash" size={14} className="text-muted-foreground hover:text-destructive" />
                  </span>
                </button>
              ))}
            </div>
          ),
        )}
      </div>

      {/* 右：编辑区 */}
      <div className="flex min-w-0 flex-1 flex-col overflow-y-auto pl-4">
        {!draft ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
            选择左侧条目编辑，或点「新建记忆」。
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {error && (
              <div className="flex items-center gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                <Icon name="alert-circle" size={14} /> {error}
              </div>
            )}
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-muted-foreground">名称（kebab-case，唯一）</span>
              <input
                className="field"
                value={draft.name}
                disabled={!isNew}
                placeholder="如 go-env"
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-muted-foreground">摘要（一句话，召回相关性依据）</span>
              <input
                className="field"
                value={draft.description}
                onChange={(e) => setDraft({ ...draft, description: e.target.value })}
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-muted-foreground">类型</span>
              <select
                className="field"
                value={draft.type}
                onChange={(e) => setDraft({ ...draft, type: e.target.value as MemoryType })}
              >
                {TYPE_ORDER.map((t) => (
                  <option key={t} value={t}>
                    {TYPE_LABEL[t]}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-sm">
              <span className="text-muted-foreground">
                正文（markdown；feedback/project 类末尾带 **Why:** / **How to apply:**）
              </span>
              <textarea
                className="field min-h-[160px] resize-y font-mono text-xs"
                value={draft.body}
                onChange={(e) => setDraft({ ...draft, body: e.target.value })}
              />
            </label>
            <div className="flex gap-2">
              <button className="btn btn-primary py-1.5 text-sm" onClick={submit} disabled={saving}>
                {saving ? '保存中…' : '保存'}
              </button>
              <button className="btn btn-ghost py-1.5 text-sm" onClick={() => setDraft(null)}>
                取消
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
