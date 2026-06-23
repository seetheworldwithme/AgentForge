import { useEffect, useMemo, useState } from 'react';
import { api } from '../lib/api';
import type { Skill } from '../types';
import { Icon } from './Icon';

const SOURCE_LABEL: Record<string, string> = {
  global: '全局',
  workspace: '工作目录',
};

export function SkillsSettings() {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [sourceFilter, setSourceFilter] = useState('all');

  const sourceCounts = useMemo(() => {
    return skills.reduce<Record<string, number>>(
      (acc, skill) => {
        acc.all += 1;
        acc[skill.source] = (acc[skill.source] ?? 0) + 1;
        return acc;
      },
      { all: 0 },
    );
  }, [skills]);

  const sourceOptions = useMemo(() => {
    const order = ['all', 'global', 'workspace'];
    const dynamic = Object.keys(sourceCounts).filter((source) => !order.includes(source));
    return [...order, ...dynamic].filter((source) => sourceCounts[source] > 0);
  }, [sourceCounts]);

  const visibleSkills = useMemo(() => {
    if (sourceFilter === 'all') return skills;
    return skills.filter((skill) => skill.source === sourceFilter);
  }, [skills, sourceFilter]);

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      setSkills(await api.listSkills());
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const toggle = async (skill: Skill) => {
    const enabled = !skill.enabled;
    setSkills((items) => items.map((item) => (item.id === skill.id ? { ...item, enabled } : item)));
    try {
      await api.setSkillEnabled(skill.id, enabled);
    } catch (e) {
      setSkills((items) =>
        items.map((item) => (item.id === skill.id ? { ...item, enabled: skill.enabled } : item)),
      );
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Icon name="loader" size={16} className="animate-spin" />
        加载中
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="text-sm font-medium text-foreground">已发现 Skills</div>
          {sourceOptions.length > 1 && (
            <div className="flex rounded-md border border-border bg-muted/40 p-0.5">
              {sourceOptions.map((source) => (
                <button
                  key={source}
                  className={
                    'rounded px-2 py-1 text-xs transition-colors ' +
                    (sourceFilter === source
                      ? 'bg-card text-foreground shadow-sm'
                      : 'text-muted-foreground hover:text-foreground')
                  }
                  onClick={() => setSourceFilter(source)}
                >
                  {source === 'all' ? '全部' : SOURCE_LABEL[source] ?? source}
                  <span className="ml-1 text-[10px] opacity-70">{sourceCounts[source]}</span>
                </button>
              ))}
            </div>
          )}
        </div>
        <button className="btn-outline !px-2.5 !py-1.5 text-xs" onClick={load}>
          <Icon name="refresh-cw" size={13} />
          刷新
        </button>
      </div>

      {error && (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {error}
        </div>
      )}

      {skills.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border bg-muted/30 px-4 py-10 text-center text-sm text-muted-foreground">
          未发现 Skills
        </div>
      ) : visibleSkills.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border bg-muted/30 px-4 py-10 text-center text-sm text-muted-foreground">
          当前来源下没有 Skills
        </div>
      ) : (
        <div className="space-y-2">
          {visibleSkills.map((skill) => (
            <div
              key={skill.id}
              className="flex items-start gap-3 rounded-xl border border-border bg-card px-3 py-3"
            >
              <div className="mt-0.5 grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
                <Icon name="sparkles" size={18} />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium text-foreground">{skill.name}</span>
                  <span className="status-pill bg-muted text-muted-foreground">
                    {SOURCE_LABEL[skill.source] ?? skill.source}
                  </span>
                  <span
                    className={
                      'status-pill ' +
                      (skill.enabled ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground')
                    }
                  >
                    {skill.enabled ? '启用' : '停用'}
                  </span>
                </div>
                {skill.description && (
                  <div className="mt-1 text-xs text-muted-foreground">{skill.description}</div>
                )}
                <div className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
                  {skill.path}
                </div>
              </div>
              <button
                className={
                  'relative h-6 w-11 shrink-0 rounded-full transition-colors ' +
                  (skill.enabled ? 'bg-primary' : 'bg-muted-foreground/30')
                }
                onClick={() => toggle(skill)}
                aria-label={skill.enabled ? '停用 Skill' : '启用 Skill'}
              >
                <span
                  className={
                    'absolute left-1 top-1 h-4 w-4 rounded-full bg-white shadow transition-transform ' +
                    (skill.enabled ? 'translate-x-5' : 'translate-x-0')
                  }
                />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
