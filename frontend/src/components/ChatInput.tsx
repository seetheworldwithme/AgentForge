import { useEffect, useMemo, useRef, useState, type ClipboardEvent, type SelectHTMLAttributes } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { useConfigStore } from '../stores/configStore';
import { useWorkDirStore } from '../stores/workdirStore';
import { useKBStore } from '../stores/kbStore';
import { api } from '../lib/api';
import { estimateMessagesTokens, estimateTokens } from '../lib/tokens';
import { Icon, type IconName } from './Icon';
import { SlashMenu, type SlashMenuHandle } from './SlashMenu';
import { FileMenu, type FileMenuHandle } from './FileMenu';
import type { Skill } from '../types';

export function ChatInput({ sessionId }: { sessionId: string | null }) {
  const [text, setText] = useState('');
  const [useRag, setUseRag] = useState(false);
  const [kbId, setKbId] = useState('');
  const [limitOpen, setLimitOpen] = useState(false);
  const [toolLimit, setToolLimit] = useState(50);
  const [confirmMode, setConfirmMode] = useState<'manual' | 'auto'>('manual');
  // 手动压缩历史弹窗：open 控制显隐，compacting 标记请求中，compactError 展示失败原因。
  const [compactOpen, setCompactOpen] = useState(false);
  const [compacting, setCompacting] = useState(false);
  const [compactError, setCompactError] = useState('');

  // 斜杠菜单的临时勾选状态：仅本次会话生效，切换会话时重置（见下方 effect）。
  const [planMode, setPlanMode] = useState(false);
  const [skillIDs, setSkillIDs] = useState<string[]>([]);
  // @ 选中的文件/文件夹:{path,is_dir}。发送时只取 path 数组传给后端注入。
  const [attachments, setAttachments] = useState<{ path: string; is_dir: boolean }[]>([]);
  // 粘贴的图片 dataURL（仅当前模型支持视觉时允许）；切换到纯文本模型时自动清空。
  const [images, setImages] = useState<string[]>([]);
  // 非 vision 模型粘贴图片 / 超出张数时的内联提示（自动清除）。
  const [pasteHint, setPasteHint] = useState('');
  // skills 列表：用于菜单展示与 chip 名称查找；菜单打开前即加载。
  const [skills, setSkills] = useState<Skill[] | null>(null);
  const menuRef = useRef<SlashMenuHandle>(null);
  const fileMenuRef = useRef<FileMenuHandle>(null);
  const send = useSessionStore((s) => s.send);
  const stopStreaming = useSessionStore((s) => s.stopStreaming);
  const streaming = useSessionStore((s) => s.streaming);
  const sessions = useSessionStore((s) => s.sessions);
  const messages = useSessionStore((s) => s.messages);
  const select = useSessionStore((s) => s.select);

  const providers = useConfigStore((s) => s.providers);
  const loaded = useConfigStore((s) => s.loaded);
  const load = useConfigStore((s) => s.load);

  // 当前选中的模型；未手动选择时跟随 is_default（没有默认则取第一个）
  const [providerId, setProviderId] = useState<string>('');
  // 用户是否手动选过模型：选过则保留其选择，不再自动跟随默认变化
  const [userPicked, setUserPicked] = useState(false);
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
    const next = def ? def.id : chatProviders[0].id;
    setProviderId((cur) => {
      // 用户已手动选择且该模型仍存在 → 尊重选择；否则跟随默认（或回退首个）
      if (userPicked && cur && chatProviders.some((p) => p.id === cur)) return cur;
      return next;
    });
  }, [chatProviders, userPicked]);

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

  // 切换会话时清空临时勾选状态（实现「本次会话临时生效」语义）。
  useEffect(() => {
    setPlanMode(false);
    setSkillIDs([]);
    setAttachments([]);
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
      attachments: attachments.map((a) => a.path),
      images,
    });
    setText('');
    setImages([]);
  };

  // 斜杠菜单：仅当输入以 `/` 开头时打开，`/` 之后的内容作为过滤词。
  const slashOpen = text.startsWith('/');
  const slashQuery = text.slice(1);
  // @ 文件菜单:仅当输入以 `@` 开头时打开,`@` 之后的内容作为过滤词。
  const fileOpen = text.startsWith('@');
  const fileQuery = text.slice(1);

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
  // @ 选中文件/文件夹:加入附件并清空触发文本(`@xxx`),菜单随之关闭;可再次输入 `@` 多选。
  const selectFile = (path: string, is_dir: boolean) => {
    setAttachments((cur) => (cur.some((a) => a.path === path) ? cur : [...cur, { path, is_dir }]));
    setText('');
  };

  // chip 名称：优先用已加载列表里的 name，回退到 id 片段。
  const skillName = (id: string) => skills?.find((s) => s.id === id)?.name ?? id.split(':').pop() ?? id;

  const ragOn = !!kbId && useRag;

  // 当前选中的对话模型是否支持视觉（粘贴图片）。
  const currentProvider = chatProviders.find((p) => p.id === providerId);
  const visionEnabled = !!currentProvider?.vision;

  // 上下文使用率：估算当前会话消息累计 token，对照所选模型的 context_window。
  // 模型未配置 window（0/缺省）时回退到 200000；canCompact 达到 60% 才允许手动压缩。
  // Message.tool_calls 为 JSON 字符串，这里按字符串长度并入估算以贴近真实占用。
  const usageTokens = useMemo(() => {
    const base = estimateMessagesTokens(messages.map((m) => ({ content: m.content })));
    let extra = 0;
    for (const m of messages) {
      if (m.tool_calls) extra += estimateTokens(m.tool_calls);
    }
    return base + extra;
  }, [messages]);
  const win = currentProvider && currentProvider.context_window && currentProvider.context_window > 0
    ? currentProvider.context_window
    : 200000;
  const usagePct = Math.min(100, Math.round((usageTokens / win) * 100));
  const canCompact = usagePct >= 60;

  // 手动压缩：调用后端 /compact，成功后重新加载会话消息以呈现 summary 卡片。
  const onCompact = async () => {
    if (!sessionId) return;
    setCompacting(true);
    setCompactError('');
    try {
      await api.compact(sessionId);
      await select(sessionId);
      setCompactOpen(false);
    } catch (e: any) {
      setCompactError(e?.message ?? '压缩失败');
    } finally {
      setCompacting(false);
    }
  };

  // 切换到不支持图片的模型时，清空已粘贴的图片并提示。
  useEffect(() => {
    if (!visionEnabled && images.length > 0) {
      setImages([]);
      setPasteHint('已切换到纯文本模型，图片已移除');
      const t = window.setTimeout(() => setPasteHint(''), 3000);
      return () => window.clearTimeout(t);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visionEnabled]);

  // 粘贴图片：仅视觉模型允许；非视觉模型阻止并提示。图片经 canvas 压缩后入列（上限 4 张）。
  const onPaste = async (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    const imageItems = Array.from(items).filter(
      (it) => it.kind === 'file' && it.type.startsWith('image/'),
    );
    if (imageItems.length === 0) return; // 非图片粘贴，交由默认行为
    e.preventDefault();
    const flash = (msg: string) => {
      setPasteHint(msg);
      window.setTimeout(() => setPasteHint(''), 3000);
    };
    if (!visionEnabled) {
      flash(`当前模型「${currentProvider?.chat_model ?? ''}」不支持图片，无法粘贴`);
      return;
    }
    if (images.length >= 4) {
      flash('最多粘贴 4 张图片');
      return;
    }
    for (const it of imageItems) {
      const file = it.getAsFile();
      if (!file) continue;
      if (images.length >= 4) {
        flash('最多粘贴 4 张图片');
        break;
      }
      const url = await compressImage(file);
      setImages((cur) => (cur.length >= 4 ? cur : [...cur, url]));
    }
  };

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
            skills={skills}
            onTogglePlan={togglePlan}
            onToggleSkill={toggleSkill}
            onClose={() => setText('')}
          />
        )}
        {fileOpen && (
          <FileMenu
            ref={fileMenuRef}
            query={fileQuery}
            attachments={attachments.map((a) => a.path)}
            workDir={workDir}
            onSelect={selectFile}
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
            onChange={(e) => {
              setProviderId(e.target.value);
              setUserPicked(true);
            }}
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

        {/* 已勾选的能力：计划模式 / Skills / 附件，点击 × 移除 */}
        {(planMode || skillIDs.length > 0 || attachments.length > 0) && (
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
            {attachments.map((a) => (
              <Chip
                key={a.path}
                icon={a.is_dir ? 'folder' : 'file-text'}
                label={a.path}
                onRemove={() => setAttachments((c) => c.filter((x) => x.path !== a.path))}
              />
            ))}
          </div>
        )}

        {/* 粘贴的图片预览：缩略图 + 移除按钮 */}
        {images.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5 px-2.5 pt-2">
            {images.map((src, idx) => (
              <div key={idx} className="relative">
                <img
                  src={src}
                  alt={`图片 ${idx + 1}`}
                  loading="lazy"
                  className="h-14 w-14 rounded-lg border border-border object-cover"
                />
                <button
                  type="button"
                  onClick={() => setImages((cur) => cur.filter((_, i) => i !== idx))}
                  className="absolute -right-1.5 -top-1.5 grid h-5 w-5 place-items-center rounded-full border border-border bg-card text-muted-foreground shadow-sm transition-colors hover:bg-accent hover:text-foreground"
                  aria-label="移除图片"
                >
                  <Icon name="x" size={11} strokeWidth={2.4} />
                </button>
              </div>
            ))}
          </div>
        )}

        {pasteHint && (
          <div className="flex items-center gap-1.5 px-2.5 pt-2 text-xs text-amber-600 dark:text-amber-400">
            <Icon name="alert-circle" size={12} className="shrink-0" />
            {pasteHint}
          </div>
        )}

        {/* 输入行 */}
        <div className="flex items-end gap-2 px-2.5 pb-2.5 pt-1.5">
          <textarea
            className="max-h-40 min-h-[44px] flex-1 resize-none bg-transparent px-1.5 py-2 text-sm leading-6 text-foreground outline-none placeholder:text-muted-foreground"
            rows={2}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onPaste={onPaste}
            onKeyDown={(e) => {
              // IME（中文输入法）组合期间的按键（如选词回车）交由输入法处理：
              // 既不触发斜杠菜单选中，也不发送——否则打中文时按回车会在候选词上屏的
              // 同时被误判为发送，把没打完的文字发出去。
              if (e.nativeEvent.isComposing || e.keyCode === 229) return;
              // 菜单打开时拦截导航键，交给对应菜单处理（不触发发送）。
              if (slashOpen) {
                if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === 'Escape') {
                  e.preventDefault();
                  menuRef.current?.handleKey(e.key);
                  return;
                }
              }
              if (fileOpen) {
                if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === 'Escape' || e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
                  e.preventDefault();
                  fileMenuRef.current?.handleKey(e.key);
                  return;
                }
              }
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                if (!streaming) submit();
              }
            }}
            placeholder={
              chatProviders.length === 0 ? '请先在设置中配置对话模型…' : '/召唤技能，@锁定文件，输入消息，Enter 发送…'
            }
          />
          {sessionId && (
            <button
              type="button"
              className="grid h-9 w-9 shrink-0 place-items-center self-end rounded-xl text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
              onClick={() => setCompactOpen(true)}
              title={`上下文 ${usageTokens.toLocaleString()} / ${win.toLocaleString()} tokens (${usagePct}%)`}
              aria-label="上下文使用率"
            >
              <ContextRing pct={usagePct} />
            </button>
          )}
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
      <CompactDialog
        open={compactOpen}
        usageTokens={usageTokens}
        win={win}
        usagePct={usagePct}
        canCompact={canCompact}
        onCompact={onCompact}
        compacting={compacting}
        compactError={compactError}
        onClose={() => setCompactOpen(false)}
      />
    </div>
  );
}

// 上下文使用率小圆环：按 pct 绘制填充弧。
// 颜色分档：< 60 翠绿（宽裕）、60–85 琥珀（偏紧）、>= 85 红色（吃紧）。
function ContextRing({ pct }: { pct: number }) {
  const r = 11;
  const c = 2 * Math.PI * r;
  const offset = c * (1 - Math.min(100, Math.max(0, pct)) / 100);
  // 颜色分档：< 60 翠绿、60–85 琥珀、>= 85 红色。
  const color = pct < 60 ? '#10b981' : pct < 85 ? '#f59e0b' : '#ef4444';
  return (
    <svg width={28} height={28} viewBox="0 0 28 28" className="shrink-0">
      {/* 背景圆环 */}
      <circle cx={14} cy={14} r={r} fill="none" stroke="currentColor" strokeOpacity={0.18} strokeWidth={2.5} />
      {/* 填充弧：从顶部顺时针绘制，stroke-dashoffset 控制已填充比例 */}
      <circle
        cx={14}
        cy={14}
        r={r}
        fill="none"
        stroke={color}
        strokeWidth={2.5}
        strokeLinecap="round"
        strokeDasharray={c}
        strokeDashoffset={offset}
        transform="rotate(-90 14 14)"
      />
      {/* 中心百分比数字 */}
      <text
        x={14}
        y={14}
        textAnchor="middle"
        dominantBaseline="central"
        fill="currentColor"
        fontSize={9}
        fontWeight={600}
      >
        {pct}
      </text>
    </svg>
  );
}

// 手动压缩历史弹窗：展示上下文用量详情 + 进度条 + 压缩按钮。
// 使用率未达 60% 时禁用压缩并提示；请求中显示 loader。
function CompactDialog({
  open,
  usageTokens,
  win,
  usagePct,
  canCompact,
  onCompact,
  compacting,
  compactError,
  onClose,
}: {
  open: boolean;
  usageTokens: number;
  win: number;
  usagePct: number;
  canCompact: boolean;
  onCompact: () => void;
  compacting: boolean;
  compactError: string;
  onClose: () => void;
}) {
  if (!open) return null;
  // 颜色分档与 ContextRing 保持一致。
  const barColor = usagePct < 60 ? 'bg-emerald-500' : usagePct < 85 ? 'bg-amber-500' : 'bg-red-500';
  return (
    <div className="fixed inset-0 z-[100] flex animate-fade-in items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="w-[380px] max-w-[92vw] animate-scale-in rounded-2xl border border-border bg-card p-5 shadow-lg">
        {/* 用量详情 */}
        <div className="mb-4">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-sm font-medium text-foreground">上下文使用率</span>
            <span className="text-sm font-semibold text-muted-foreground">{usagePct}%</span>
          </div>
          {/* 进度条：颜色随档位变化 */}
          <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
            <div
              className={'h-full rounded-full transition-all ' + barColor}
              style={{ width: `${Math.min(100, usagePct)}%` }}
            />
          </div>
          <p className="mt-1.5 text-xs leading-5 text-muted-foreground">
            {usageTokens.toLocaleString()} / {win.toLocaleString()} tokens
          </p>
        </div>

        {/* 禁用提示：使用率不足 */}
        {!canCompact && (
          <p className="mb-3 flex items-center gap-1.5 text-xs text-muted-foreground">
            <Icon name="alert-circle" size={12} className="shrink-0" />
            使用率未达 60%，无需压缩
          </p>
        )}
        {/* 错误提示 */}
        {compactError && (
          <p className="mb-3 flex items-center gap-1.5 text-xs text-red-600 dark:text-red-400">
            <Icon name="alert-circle" size={12} className="shrink-0" />
            {compactError}
          </p>
        )}

        <div className="flex items-center justify-end gap-2">
          <button className="btn-outline gap-1.5 text-muted-foreground" onClick={onClose} disabled={compacting}>
            <Icon name="x" size={15} />
            取消
          </button>
          <button className="btn-primary gap-1.5" onClick={onCompact} disabled={!canCompact || compacting}>
            {compacting ? (
              <>
                <Icon name="loader" size={15} className="animate-spin" />
                压缩中…
              </>
            ) : (
              <>
                <Icon name="archive" size={15} />
                压缩历史
              </>
            )}
          </button>
        </div>
      </div>
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

// compressImage 用 canvas 把图片重绘到最长边 ≤ maxEdge、输出 dataURL，
// 控制单张体积避免 base64 撑爆请求体与数据库；任何环节失败都回退原始 dataURL。
async function compressImage(file: File, maxEdge = 1568, quality = 0.82): Promise<string> {
  const dataUrl = await new Promise<string>((resolve, reject) => {
    const fr = new FileReader();
    fr.onload = () => resolve(fr.result as string);
    fr.onerror = () => reject(fr.error);
    fr.readAsDataURL(file);
  });
  try {
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const i = new Image();
      i.onload = () => resolve(i);
      i.onerror = reject;
      i.src = dataUrl;
    });
    let { width, height } = img;
    if (width > maxEdge || height > maxEdge) {
      const scale = maxEdge / Math.max(width, height);
      width = Math.round(width * scale);
      height = Math.round(height * scale);
    }
    const canvas = document.createElement('canvas');
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext('2d');
    if (!ctx) return dataUrl;
    ctx.drawImage(img, 0, 0, width, height);
    // PNG（可能含透明度）保留 PNG，其余统一 JPEG 压缩。
    return file.type === 'image/png' ? canvas.toDataURL('image/png') : canvas.toDataURL('image/jpeg', quality);
  } catch {
    return dataUrl;
  }
}
