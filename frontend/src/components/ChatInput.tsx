import { useEffect, useMemo, useState, type SelectHTMLAttributes } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { useConfigStore } from '../stores/configStore';
import { useWorkDirStore } from '../stores/workdirStore';
import { useKBStore } from '../stores/kbStore';
import { Icon, type IconName } from './Icon';

export function ChatInput({ sessionId }: { sessionId: string | null }) {
  const [text, setText] = useState('');
  const [useRag, setUseRag] = useState(false);
  const [kbId, setKbId] = useState('');
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
    });
    setText('');
  };

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
      <div className="rounded-2xl border border-border bg-card shadow-md transition-colors focus-within:border-primary/50">
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

          {/* 选择工作目录 */}
          <button
            type="button"
            className="ml-auto inline-flex max-w-[220px] items-center gap-1.5 rounded-md border border-transparent bg-muted px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
            onClick={pickDirectory}
            title={workDir || '选择工作目录'}
          >
            <Icon name="folder" size={13} className="shrink-0 text-primary" />
            <span className="truncate">{workDir ? workDir.split(/[\\/]/).pop() : '工作目录'}</span>
          </button>
        </div>

        {/* 输入行 */}
        <div className="flex items-end gap-2 px-2.5 pb-2.5 pt-1.5">
          <textarea
            className="max-h-40 min-h-[44px] flex-1 resize-none bg-transparent px-1.5 py-2 text-sm leading-6 text-foreground outline-none placeholder:text-muted-foreground"
            rows={2}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
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
