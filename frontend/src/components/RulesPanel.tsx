import { useEffect, useState } from 'react';
import { Icon } from './Icon';
import { useRulesStore, type RuleScope } from '../stores/rulesStore';
import { api } from '../lib/api';

export function RulesPanel() {
  const { global, globalExists, project, imports, loaded, load, save, clear, setImports } =
    useRulesStore();
  const [scope, setScope] = useState<RuleScope>('global');
  const [draft, setDraft] = useState('');
  const [workdir, setWorkdir] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    load();
    api.getWorkDir().then((r) => setWorkdir(r.workdir)).catch(() => {});
  }, [load]);

  // 切换 scope 或后端内容刷新时，把 draft 同步到对应 scope 的最新内容。
  useEffect(() => {
    setDraft(scope === 'global' ? global : project);
  }, [scope, global, project]);

  const path =
    scope === 'global'
      ? '~/.agentforge/AGENTFORGE.md'
      : `${workdir || '(未设置工作目录)'}/AGENTFORGE.md`;
  const projectDisabled = scope === 'project' && !workdir;

  const submit = async () => {
    setSaving(true);
    setError('');
    try {
      await save(scope, draft);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const onClear = async () => {
    if (!confirm(`清空${scope === 'global' ? '全局' : '项目'}规则？`)) return;
    setError('');
    try {
      await clear(scope);
      setDraft('');
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  if (!loaded) {
    return <div className="p-4 text-sm text-muted-foreground">加载中…</div>;
  }

  return (
    <div className="flex flex-col gap-5">
      {/* 导入设置：兼容读取其它工具的规则文件 */}
      <section className="card p-4">
        <div className="mb-3">
          <h3 className="text-sm font-semibold text-foreground">导入设置</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">
            开启后，额外读取当前工作目录下对应文件（与 AGENTFORGE.md 叠加生效）。
          </p>
        </div>
        <div className="flex flex-col gap-1">
          <ImportToggle
            label="CLAUDE.md"
            desc="读取 CLAUDE.md 并将其添加到上下文中"
            on={imports.claude}
            onToggle={(v) => setImports({ ...imports, claude: v })}
          />
          <ImportToggle
            label="AGENTS.md"
            desc="读取 AGENTS.md 并将其添加到上下文中"
            on={imports.agents}
            onToggle={(v) => setImports({ ...imports, agents: v })}
          />
        </div>
      </section>

      {/* 规则编辑器：全局 / 项目 单文件 */}
      <section className="flex min-h-0 flex-col gap-3">
        <div className="flex items-center gap-1">
          {(['global', 'project'] as RuleScope[]).map((s) => (
            <button
              key={s}
              className={
                'relative flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm transition-colors ' +
                (s === scope
                  ? 'bg-card font-medium text-foreground shadow-sm'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground')
              }
              onClick={() => setScope(s)}
            >
              {s === scope && (
                <span className="absolute bottom-1 left-0 top-1 w-0.5 rounded-full bg-primary" />
              )}
              <Icon name="file-text" size={14} />
              {s === 'global' ? '全局' : '项目'}
            </button>
          ))}
        </div>

        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Icon name="file-text" size={12} />
          <span className="truncate font-mono">{path}</span>
          {scope === 'global' && !globalExists && (
            <span className="text-muted-foreground/70">（尚未创建）</span>
          )}
        </div>

        {projectDisabled ? (
          <div className="flex items-center gap-2 rounded-md border border-dashed border-border px-3 py-6 text-sm text-muted-foreground">
            <Icon name="alert-circle" size={14} /> 项目规则需要先设置工作目录
          </div>
        ) : (
          <>
            {error && (
              <div className="flex items-center gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                <Icon name="alert-circle" size={14} /> {error}
              </div>
            )}
            <textarea
              className="field min-h-[280px] resize-y font-mono text-xs leading-relaxed"
              placeholder={'# 规则标题\n\n- 使用中文注释\n- 提交前跑测试'}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
            <div className="flex gap-2">
              <button
                className="btn btn-primary flex items-center gap-1.5 py-1.5 text-sm"
                onClick={submit}
                disabled={saving}
              >
                <Icon name="check" size={14} /> {saving ? '保存中…' : '保存'}
              </button>
              <button
                className="btn btn-danger flex items-center gap-1.5 py-1.5 text-sm"
                onClick={onClear}
              >
                <Icon name="trash" size={14} /> 清空
              </button>
            </div>
          </>
        )}
      </section>
    </div>
  );
}

// ImportToggle 一行开关：左侧标题+描述，右侧语义化 switch。
function ImportToggle({
  label,
  desc,
  on,
  onToggle,
}: {
  label: string;
  desc: string;
  on: boolean;
  onToggle: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md px-2 py-2 transition-colors hover:bg-muted/40">
      <div className="min-w-0">
        <div className="flex items-center gap-1.5 text-sm font-medium text-foreground">
          <Icon name="file-text" size={13} className="text-muted-foreground" />
          {label}
        </div>
        <p className="mt-0.5 pl-5 text-xs text-muted-foreground">{desc}</p>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={on}
        className={
          'relative h-6 w-11 shrink-0 rounded-full transition-colors ' +
          (on ? 'bg-primary' : 'bg-muted-foreground/30')
        }
        onClick={() => onToggle(!on)}
      >
        <span
          className={
            'absolute left-1 top-1 h-4 w-4 rounded-full bg-white shadow transition-transform ' +
            (on ? 'translate-x-5' : 'translate-x-0')
          }
        />
      </button>
    </div>
  );
}
