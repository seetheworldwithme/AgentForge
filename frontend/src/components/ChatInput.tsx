import { useEffect, useMemo, useRef, useState, type SelectHTMLAttributes } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { useConfigStore } from '../stores/configStore';
import { useWorkDirStore } from '../stores/workdirStore';
import { useKBStore } from '../stores/kbStore';
import { api } from '../lib/api';
import { Icon, type IconName } from './Icon';
import { SlashMenu, type SlashMenuHandle } from './SlashMenu';
import type { Skill, MCPServer } from '../types';

export function ChatInput({ sessionId }: { sessionId: string | null }) {
  const [text, setText] = useState('');
  const [useRag, setUseRag] = useState(false);
  const [kbId, setKbId] = useState('');
  const [limitOpen, setLimitOpen] = useState(false);
  const [toolLimit, setToolLimit] = useState(50);
  const [confirmMode, setConfirmMode] = useState<'manual' | 'auto'>('manual');

  // 斜杠菜单的临时勾选状态：仅本次会话生效，切换会话时重置（见下方 effect）。
  const [planMode, setPlanMode] = useState(false);
  const [skillIDs, setSkillIDs] = useState<string[]>([]);
  const [mcpIDs, setMcpIDs] = useState<string[]>([]);
  // skills/mcp 列表：用于菜单展示与 chip 名称查找；菜单打开前即加载。
  const [skills, setSkills] = useState<Skill[] | null>(null);
  const [mcps, setMcps] = useState<MCPServer[] | null>(null);
  const menuRef = useRef<SlashMenuHandle>(null);
  const send = useSessionStore((s) => s.send);
  const stopStreaming = useSessionStore((s) => s.stopStreaming);
  const streaming = useSessionStore((s) => s.streaming);
  const sessions = useSessionStore((s) => s.sessions);

  const providers = useConfigStore((s) => s.providers);
  const loaded = useConfigStore((s) => s.loaded);
  const load = useConfigStore((s) => s.load);

  // 当前选中的模型；默认取 is_default 或第一个
  const [providerId, setProviderId] = useState<string>('');
  // 工作目录（共享状态，侧边栏分组也依赖它）
  const workDir = useWorkDirStore((s) => s.workdir);
  const wdLoaded = useWorkDirStore((s) => s.loaded);
  const wdLoad = useWorkDirStore((s) => s.load);
  const setWorkDir = useWorkDirStore((s) => s.setWorkDir);
  const kbs = useKBStore((s) => s.kbs);
  const loadKBs = useKBStore((s) => s.load);

  useEffect(() => {
    if (!loaded) load();
  }, [loaded, load]);

  // 对话下拉框只展示 chat 类模型（排除 embed 向量模型）；老数据无 kind 视为 chat
  const chatProviders = useMemo(
    () => providers.filter((p) => (p.kind ?? 'chat') !== 'embed'),
    [providers],
  );

  useEffect(() => {
    if (chatProviders.length === 0) return;
    const def = chatProviders.find((p) => p.is_default);
    setProviderId((cur) => cur || (def ? def.id : chatProviders[0].id));
  }, [chatProviders]);

  // 初始化读取当前工作目录
  useEffect(() => {
    if (!wdLoaded) wdLoad();
  }, [wdLoaded, wdLoad]);

  useEffect(() => {
    loadKBs();
  }, [loadKBs]);

  // skills 随当前工作目录变化重新加载——工作目录决定 workspace 来源的 skills。
  // 后端 WorkDir 启动时为空，首次加载通常只拿到 global；用户切换工作目录后必须
  // 重新拉取，否则斜杠菜单会一直停留在初始的 global 列表（过滤后显示 0/N）。
  useEffect(() => {
    let alive = true;
    api.listSkills().then((s) => alive && setSkills(s)).catch(() => alive && setSkills([]));
    return () => {
      alive = false;
    };
  }, [workDir]);

  // mcp 列表与工作目录无关（配置在全局 ~/.agent/mcp.json），挂载时加载一次即可。
  useEffect(() => {
    let alive = true;
    api.listMCPServers().then((m) => alive && setMcps(m)).catch(() => alive && setMcps([]));
    return () => {
      alive = false;
    };
  }, []);

  // 切换会话时清空临时勾选状态（实现「本次会话临时生效」语义）。
  useEffect(() => {
    setPlanMode(false);
    setSkillIDs([]);
    setMcpIDs([]);
  }, [sessionId]);

  // 读取工具调用上限与确认规则配置（齿轮按钮使用）
  useEffect(() => {
    api.getToolLimit().then((r) => setToolLimit(r.limit)).catch(() => {});
    api.getConfirmMode().then((r) => setConfirmMode(r.mode === 'auto' ? 'auto' : 'manual')).catch(() => {});
  }, []);

  const saveConfig = async (n: number, mode: 'manual' | 'auto') => {
    try {
      await Promise.all([api.setToolLimit(n), api.setConfirmMode(mode)]);
      setToolLimit(n);
      setConfirmMode(mode);
    } catch {
      /* 保存失败，忽略 */
    }
  };

  useEffect(() => {
    const session = sessions.find((s) => s.id === sessionId);
    setKbId(session?.kb_id ?? '');
    setUseRag(!!session?.kb_id);
  }, [sessionId, sessions]);

  const submit = () => {
    if (streaming) {
      stopStreaming();
      return;
    }
    if (!text.trim() || streaming) return;
    if (!sessionId && !providerId) return;
    send(text, {
      tools_enabled: true,
      use_rag: !!kbId && useRag,
      provider_id: providerId || undefined,
      kb_id: kbId,
      plan_mode: planMode,
      skill_ids: skillIDs,
      mcp_server_ids: mcpIDs,
    });
    setText('');
  };

  // 斜杠菜单：仅当输入以 `/` 开头时打开，`/` 之后的内容作为过滤词。
  const slashOpen = text.startsWith('/');
  const slashQuery = text.slice(1);

  // 勾选回调：切换对应状态并清空触发文本（`/xxx`），菜单随之关闭；
  // 用户可再次输入 `/` 继续多选。
  const togglePlan = () => {
    setPlanMode((v) => !v);
    setText('');
  };
  const toggleSkill = (id: string) => {
    setSkillIDs((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));
    setText('');
  };
  const toggleMCP = (id: string) => {
    setMcpIDs((cur) => (cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]));
    setText('');
  };

  // chip 名称：优先用已加载列表里的 name，回退到 id 片段。
  const skillName = (id: string) => skills?.find((s) => s.id === id)?.name ?? id.split(':').pop() ?? id;
  const mcpName = (id: string) => mcps?.find((m) => m.id === id)?.name ?? id;

  const ragOn = !!kbId && useRag;

  // 打开目录选择对话框
  const pickDirectory = async () => {
    // Wails 生产模式：调用原生目录选择对话框
    const w = window as any;
    if (w.go?.main?.DialogBinder?.OpenDirectory) {
      try {
        const dir = await w.go.main.DialogBinder.OpenDirectory();
        if (dir) {
          await setWorkDir(dir);
        }
      } catch {
        /* 用户取消或出错，忽略 */
      }
      return;
    }
    // 开发模式（浏览器）：回退到手动输入
    const dir = window.prompt('请输入工作目录的绝对路径', workDir);
    if (dir && dir.trim()) {
      try {
        await setWorkDir(dir.trim());
      } catch {
        /* 保存失败，忽略 */
      }
    }
  };

  return (
    <div className="px-4 pb-4 pt-2">
      <div className="relative rounded-2xl border border-border bg-card shadow-md transition-colors focus-within:border-primary/50">
        {slashOpen && (
          <SlashMenu
            ref={menuRef}
            query={slashQuery}
            planMode={planMode}
            skillIDs={skillIDs}
            mcpIDs={mcpIDs}
            skills={skills}
            mcps={mcps}
            onTogglePlan={togglePlan}
            onToggleSkill={toggleSkill}
            onToggleMCP={toggleMCP}
            onClose={() => setText('')}
          />
        )}
        {/* 工具栏：知识库 / 检索 / 模型 / 工作目录 */}
        <div className="flex flex-wrap items-center gap-1.5 px-2.5 pt-2.5">
          <IconSelect
            icon="database"
            value={kbId}
            onChange={(e) => {
              setKbId(e.target.value);
              setUseRag(!!e.target.value);
            }}
            title="选择本会话使用的知识库"
          >
            <option value="">不使用知识库</option>
            {kbs.map((kb) => (
              <option key={kb.id} value={kb.id}>
                {kb.name}
              </option>
            ))}
          </IconSelect>

          <button
            type="button"
            disabled={!kbId}
            onClick={() => setUseRag(!useRag)}
            className={
              'inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-40 ' +
              (ragOn
                ? 'border-primary/30 bg-primary/10 text-primary'
                : 'border-transparent bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground')
            }
            title="本条消息检索知识库"
          >
            <Icon name="search" size={12} />
            本条检索
          </button>

          <IconSelect
            icon="settings"
            value={providerId}
            onChange={(e) => setProviderId(e.target.value)}
            title="选择对话使用的模型"
          >
            {chatProviders.length === 0 && <option value="">未配置模型</option>}
            {chatProviders.map((p) => (
              <option key={p.id} value={p.id}>
                {p.chat_model}
              </option>
            ))}
          </IconSelect>

          {/* 右侧：工具上限配置 + 工作目录 */}
          <div className="ml-auto flex items-center gap-1.5">
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-md border border-transparent bg-muted px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
              onClick={() => setLimitOpen(true)}
              title="工具配置"
            >
              <Icon name="settings" size={13} className="shrink-0" />
              <span>配置</span>
            </button>
            <button
              type="button"
              className="inline-flex max-w-[200px] items-center gap-1.5 rounded-md border border-transparent bg-muted px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
              onClick={pickDirectory}
              title={workDir || '选择工作目录'}
            >
              <Icon name="folder" size={13} className="shrink-0 text-primary" />
              <span className="truncate">{workDir ? workDir.split(/[\\/]/).pop() : '工作目录'}</span>
            </button>
          </div>
        </div>

        {/* 已勾选的能力：计划模式 / Skills / MCP，点击 × 移除 */}
        {(planMode || skillIDs.length > 0 || mcpIDs.length > 0) && (
          <div className="flex flex-wrap items-center gap-1.5 px-2.5 pt-2">
            {planMode && <Chip icon="file-text" label="计划模式" onRemove={() => setPlanMode(false)} />}
            {skillIDs.map((id) => (
              <Chip
                key={id}
                icon="sparkles"
                label={skillName(id)}
                onRemove={() => setSkillIDs((c) => c.filter((x) => x !== id))}
              />
            ))}
            {mcpIDs.map((id) => (
              <Chip
                key={id}
                icon="wrench"
                label={mcpName(id)}
                onRemove={() => setMcpIDs((c) => c.filter((x) => x !== id))}
              />
            ))}
          </div>
        )}

        {/* 输入行 */}
        <div className="flex items-end gap-2 px-2.5 pb-2.5 pt-1.5">
          <textarea
            className="max-h-40 min-h-[44px] flex-1 resize-none bg-transparent px-1.5 py-2 text-sm leading-6 text-foreground outline-none placeholder:text-muted-foreground"
            rows={2}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              // 菜单打开时拦截导航键，交给 SlashMenu 处理（不触发发送）。
              if (slashOpen) {
                if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === 'Escape') {
                  e.preventDefault();
                  menuRef.current?.handleKey(e.key);
                  return;
                }
              }
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                if (!streaming) submit();
              }
            }}
            placeholder={
              chatProviders.length === 0 ? '请先在设置中配置对话模型…' : '输入消息，Enter 发送…'
            }
          />
          <button
            className={
              'grid h-9 w-9 shrink-0 place-items-center self-end rounded-xl text-primary-foreground shadow-sm transition-all active:scale-95 disabled:bg-muted disabled:text-muted-foreground disabled:shadow-none ' +
              (streaming ? 'bg-primary/90 hover:bg-primary' : 'bg-primary hover:bg-primary/90')
            }
            onClick={submit}
            disabled={!streaming && (!text.trim() || (!sessionId && !providerId))}
            aria-label={streaming ? '停止回答' : '发送'}
            title={streaming ? '停止回答' : '发送'}
          >
            {streaming ? (
              <Icon name="square" size={16} strokeWidth={2.4} className="animate-pulse" />
            ) : (
              <Icon name="arrow-up" size={18} strokeWidth={2.25} />
            )}
          </button>
        </div>
      </div>
      <div className="mt-2 px-2 text-[11px] text-muted-foreground">
        Enter 发送 · Shift+Enter 换行
      </div>
      <ConfigDialog
        open={limitOpen}
        limit={toolLimit}
        mode={confirmMode}
        onClose={() => setLimitOpen(false)}
        onSave={saveConfig}
      />
    </div>
  );
}

// 工具配置弹窗：齿轮按钮触发。包含「工具调用上限」与「规则（手动/自动）」两项。
function ConfigDialog({
  open,
  limit,
  mode,
  onClose,
  onSave,
}: {
  open: boolean;
  limit: number;
  mode: 'manual' | 'auto';
  onClose: () => void;
  onSave: (limit: number, mode: 'manual' | 'auto') => void | Promise<void>;
}) {
  const [val, setVal] = useState(String(limit));
  const [m, setM] = useState<'manual' | 'auto'>(mode);
  useEffect(() => {
    if (open) {
      setVal(String(limit));
      setM(mode);
    }
  }, [open, limit, mode]);

  if (!open) return null;

  const save = () => {
    const n = parseInt(val, 10);
    if (!Number.isNaN(n) && n >= 0) {
      onSave(n, m);
      onClose();
    }
  };

  const seg = (active: boolean) =>
    'flex-1 rounded-md border px-3 py-1.5 text-xs transition-colors ' +
    (active
      ? 'border-primary/40 bg-primary/10 text-primary'
      : 'border-border bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground');

  return (
    <div className="fixed inset-0 z-[100] flex animate-fade-in items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="w-[380px] max-w-[92vw] animate-scale-in rounded-2xl border border-border bg-card p-5 shadow-lg">
        {/* 工具调用上限 */}
        <div className="mb-4">
          <div className="mb-1.5 text-sm font-medium text-foreground">工具调用上限</div>
          <div className="flex items-center gap-2">
            <input
              type="number"
              min={0}
              value={val}
              onChange={(e) => setVal(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') save();
              }}
              className="w-28 rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus-visible:border-primary"
            />
            <span className="text-xs text-muted-foreground">次（0 = 不限制）</span>
          </div>
        </div>

        {/* 规则：手动 / 自动 */}
        <div className="mb-5">
          <div className="mb-1.5 text-sm font-medium text-foreground">规则</div>
          <div className="flex gap-2">
            <button type="button" className={seg(m === 'manual')} onClick={() => setM('manual')}>
              手动
            </button>
            <button type="button" className={seg(m === 'auto')} onClick={() => setM('auto')}>
              自动
            </button>
          </div>
          <p className="mt-1.5 text-xs leading-5 text-muted-foreground">
            {m === 'manual' ? '调用工具或命令前弹窗确认。' : '直接执行工具或命令，不再询问用户。'}
          </p>
        </div>

        <div className="flex items-center justify-end gap-2">
          <button className="btn-outline gap-1.5 text-muted-foreground" onClick={onClose}>
            <Icon name="x" size={15} />
            取消
          </button>
          <button className="btn-primary gap-1.5" onClick={save}>
            <Icon name="check" size={15} strokeWidth={2.5} />
            保存
          </button>
        </div>
      </div>
    </div>
  );
}

// 带前置图标的紧凑下拉选择器（原生 select，appearance-none 自定义样式）
function IconSelect({
  icon,
  className,
  children,
  ...props
}: { icon: IconName; className?: string } & SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className={'relative ' + (className ?? '')}>
      <Icon
        name={icon}
        size={13}
        className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground"
      />
      <select
        {...props}
        className="max-w-[200px] cursor-pointer appearance-none truncate rounded-md border border-transparent bg-muted py-1 pl-6 pr-6 text-xs text-foreground outline-none transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:border-primary focus-visible:bg-background"
      >
        {children}
      </select>
      <Icon
        name="chevron-down"
        size={12}
        className="pointer-events-none absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground"
      />
    </div>
  );
}

// 已勾选能力的标签：图标 + 名称 + 移除按钮。统一使用 Icon，禁用 emoji。
function Chip({ icon, label, onRemove }: { icon: IconName; label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-primary/30 bg-primary/10 px-2 py-1 text-xs text-primary">
      <Icon name={icon} size={12} className="shrink-0" />
      <span className="max-w-[160px] truncate">{label}</span>
      <button
        type="button"
        onClick={onRemove}
        className="shrink-0 rounded-sm p-0.5 hover:bg-primary/20"
        aria-label="移除"
      >
        <Icon name="x" size={12} />
      </button>
    </span>
  );
}
