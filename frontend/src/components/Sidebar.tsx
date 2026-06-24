import { useEffect, useMemo, useState } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { useConfigStore } from '../stores/configStore';
import { useWorkDirStore } from '../stores/workdirStore';
import { useThemeStore } from '../stores/themeStore';
import { Icon, type IconName } from './Icon';
import type { Session } from '../types';

export function Sidebar({
  activeView,
  onViewChange,
  onOpenSettings,
}: {
  activeView: 'chat' | 'knowledge';
  onViewChange: (view: 'chat' | 'knowledge') => void;
  onOpenSettings: () => void;
}) {
  const sessions = useSessionStore((s) => s.sessions);
  const loadSessions = useSessionStore((s) => s.loadSessions);
  const newChat = useSessionStore((s) => s.newChat);
  const providers = useConfigStore((s) => s.providers);
  const loaded = useConfigStore((s) => s.loaded);
  const loadProviders = useConfigStore((s) => s.load);
  const workdir = useWorkDirStore((s) => s.workdir);
  const wdLoaded = useWorkDirStore((s) => s.loaded);
  const wdLoad = useWorkDirStore((s) => s.load);
  const toggleTheme = useThemeStore((s) => s.toggle);
  const themeResolved = useThemeStore((s) => s.resolved);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);
  useEffect(() => {
    if (!loaded) loadProviders();
  }, [loaded, loadProviders]);
  useEffect(() => {
    if (!wdLoaded) wdLoad();
  }, [wdLoaded, wdLoad]);

  const handleNewChat = () => {
    // 没有 provider 时引导用户去设置；否则进入空白草稿态（不立即创建 session）。
    // 新会话必须用对话模型：优先取默认的 chat 模型，避免误用向量（embed）模型。
    const chatProviders = providers.filter((p) => (p.kind ?? 'chat') !== 'embed');
    const def = chatProviders.find((p) => p.is_default) ?? chatProviders[0];
    if (!def?.id) {
      onOpenSettings();
      return;
    }
    newChat();
  };

  // 按工作目录分组会话。当前目录即便没有会话也单独成组，让用户看到新对话将归属何处。
  const groups = useMemo(() => {
    const map = new Map<string, Session[]>();
    for (const s of sessions) {
      const key = s.workdir || '';
      const arr = map.get(key);
      if (arr) arr.push(s);
      else map.set(key, [s]);
    }
    return map;
  }, [sessions]);

  const groupKeys = useMemo(() => {
    const ordered: string[] = [];
    if (workdir) ordered.push(workdir); // 当前目录优先（即使为空）
    [...groups.keys()]
      .filter((k) => k !== '' && k !== workdir)
      .sort()
      .forEach((k) => ordered.push(k));
    if (groups.has('')) ordered.push(''); // 未分组放最后
    return ordered;
  }, [groups, workdir]);

  return (
    <div className="flex h-full w-64 flex-col border-r border-sidebar-border bg-sidebar">
      {/* 品牌区 */}
      <div className="flex items-center gap-2.5 px-4 pb-3 pt-4">
        <div className="grid h-8 w-8 place-items-center rounded-lg bg-primary text-primary-foreground shadow-sm">
          <Icon name="sparkles" size={18} strokeWidth={2} />
        </div>
        <div className="leading-tight">
          <div className="text-[15px] font-semibold tracking-tight text-foreground">AgentForge</div>
          <div className="text-[11px] text-muted-foreground">AI 工作台</div>
        </div>
      </div>

      {/* 导航 */}
      <nav className="px-2">
        <NavItem
          icon="message-square"
          label="对话"
          active={activeView === 'chat'}
          onClick={() => onViewChange('chat')}
        />
        <NavItem
          icon="book-open"
          label="知识库"
          active={activeView === 'knowledge'}
          onClick={() => onViewChange('knowledge')}
        />
      </nav>

      {/* 新对话 */}
      <div className="px-3 pb-2 pt-3">
        <button
          className="btn-primary w-full"
          onClick={() => {
            onViewChange('chat');
            handleNewChat();
          }}
        >
          <Icon name="plus" size={16} strokeWidth={2.25} />
          新对话
        </button>
      </div>

      {/* 会话列表 */}
      <div className="flex-1 overflow-y-auto px-2 pb-2">
        {groupKeys.length === 0 ? (
          <div className="px-2 py-10 text-center text-xs text-muted-foreground">
            暂无会话
            <br />
            点击「新对话」开始
          </div>
        ) : (
          groupKeys.map((dir) => (
            <SessionGroup key={dir || '__ungrouped__'} dir={dir} sessions={groups.get(dir) ?? []} />
          ))
        )}
      </div>

      {/* 底部：设置 + 主题切换 */}
      <div className="flex items-center gap-1 border-t border-sidebar-border px-2 py-2">
        <button className="btn-ghost flex-1 justify-start gap-2" onClick={onOpenSettings}>
          <Icon name="settings" size={16} />
          设置
        </button>
        <button
          className="btn-ghost h-9 w-9 px-0"
          onClick={toggleTheme}
          title={themeResolved === 'dark' ? '切换到亮色' : '切换到暗色'}
          aria-label="切换主题"
        >
          <Icon name={themeResolved === 'dark' ? 'sun' : 'moon'} size={16} />
        </button>
      </div>
    </div>
  );
}

function NavItem({
  icon,
  label,
  active,
  onClick,
}: {
  icon: IconName;
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={
        'group relative flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-sm font-medium transition-colors ' +
        (active
          ? 'bg-sidebar-accent text-sidebar-accent-foreground'
          : 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground')
      }
    >
      {active && (
        <span className="absolute bottom-1.5 left-0 top-1.5 w-0.5 rounded-full bg-primary" />
      )}
      <Icon name={icon} size={17} />
      {label}
    </button>
  );
}

// 可折叠的目录分组：头部显示目录名（未选中目录则归入"未分组"），点击展开/合并。
// 悬停真实工作目录时，右侧出现"新建对话 / 删除目录"两个按钮。
function SessionGroup({ dir, sessions }: { dir: string; sessions: Session[] }) {
  const currentId = useSessionStore((s) => s.currentId);
  const newChat = useSessionStore((s) => s.newChat);
  const remove = useSessionStore((s) => s.remove);
  const workdir = useWorkDirStore((s) => s.workdir);
  const setWorkDir = useWorkDirStore((s) => s.setWorkDir);

  const [open, setOpen] = useState(true);
  const [confirming, setConfirming] = useState(false);
  const basename = dir.split(/[\\/]/).filter(Boolean).pop() || dir;

  // 切到该目录后再新建：新对话会归属此目录（后端按当前全局目录打戳），工具也在该目录执行。
  const onNew = async () => {
    await setWorkDir(dir);
    newChat();
  };

  // 删除整个目录分组：移除其下全部对话；若正是当前目录则清空，使空的当前分组也消失。
  const onDelete = async () => {
    for (const s of sessions) {
      await remove(s.id);
    }
    if (dir === workdir) {
      await setWorkDir('');
    }
    setConfirming(false);
  };

  const toggle = () => {
    if (!confirming) setOpen((o) => !o);
  };

  return (
    <div className="mb-0.5">
      <div
        className="group flex select-none items-center gap-1 rounded-md px-1.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-sidebar-accent/50"
        onClick={toggle}
        title={dir || '未分组'}
      >
        <Icon
          name="chevron-right"
          size={13}
          className={'shrink-0 transition-transform duration-150 ' + (open ? 'rotate-90' : '')}
        />
        <Icon
          name={dir ? 'folder' : 'folder-open'}
          size={14}
          className="shrink-0 text-muted-foreground/70"
        />
        <span className="flex-1 truncate">{dir ? basename : '未分组'}</span>
        {confirming ? (
          <span className="flex items-center gap-0.5">
            <span className="mr-0.5 text-[10px] text-destructive">删除全部?</span>
            <button
              className="rounded p-0.5 text-destructive hover:bg-destructive/10"
              title="确认删除"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Icon name="check" size={13} strokeWidth={2.5} />
            </button>
            <button
              className="rounded p-0.5 hover:bg-sidebar hover:text-foreground"
              title="取消"
              onClick={(e) => {
                e.stopPropagation();
                setConfirming(false);
              }}
            >
              <Icon name="x" size={13} strokeWidth={2.5} />
            </button>
          </span>
        ) : (
          <span className="flex items-center gap-0.5">
            {dir && (
              <span className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                <button
                  className="rounded p-0.5 text-muted-foreground hover:bg-sidebar hover:text-primary"
                  title="在此目录新建对话"
                  onClick={(e) => {
                    e.stopPropagation();
                    onNew();
                  }}
                >
                  <Icon name="plus" size={13} strokeWidth={2.25} />
                </button>
                <button
                  className="rounded p-0.5 text-muted-foreground hover:bg-sidebar hover:text-destructive"
                  title="删除该目录下全部对话"
                  onClick={(e) => {
                    e.stopPropagation();
                    setConfirming(true);
                  }}
                >
                  <Icon name="trash" size={13} />
                </button>
              </span>
            )}
            <span className="ml-0.5 text-[10px] tabular-nums text-muted-foreground/70">
              {sessions.length}
            </span>
          </span>
        )}
      </div>
      {open &&
        sessions.map((s) => <SessionRow key={s.id} session={s} active={s.id === currentId} />)}
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
      <div className="px-1.5 py-1">
        <input
          autoFocus
          className="w-full rounded-md border border-input bg-card px-2 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
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
        'group relative flex cursor-pointer items-center gap-2 rounded-md px-2.5 py-2 text-sm transition-colors ' +
        (active
          ? 'bg-sidebar-accent text-sidebar-accent-foreground'
          : 'text-sidebar-foreground/80 hover:bg-sidebar-accent/50')
      }
      onClick={() => select(session.id)}
    >
      {active && (
        <span className="absolute bottom-1.5 left-0 top-1.5 w-0.5 rounded-full bg-primary" />
      )}
      <span className={'flex-1 truncate ' + (active ? 'font-medium' : '')}>{session.title}</span>
      <button
        className="rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-sidebar hover:text-foreground group-hover:opacity-100"
        title="重命名"
        onClick={startEdit}
      >
        <Icon name="pencil" size={13} />
      </button>
      <button
        className="rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-sidebar hover:text-destructive group-hover:opacity-100"
        title="删除"
        onClick={(e) => {
          e.stopPropagation();
          remove(session.id);
        }}
      >
        <Icon name="x" size={14} strokeWidth={2} />
      </button>
    </div>
  );
}
