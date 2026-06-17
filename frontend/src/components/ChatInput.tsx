import { useEffect, useState } from 'react';
import { useSessionStore } from '../stores/sessionStore';
import { useConfigStore } from '../stores/configStore';
import { useWorkDirStore } from '../stores/workdirStore';
import { useKBStore } from '../stores/kbStore';

export function ChatInput({ sessionId }: { sessionId: string | null }) {
  const [text, setText] = useState('');
  const [useRag, setUseRag] = useState(false);
  const [kbId, setKbId] = useState('');
  const send = useSessionStore((s) => s.send);
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

  useEffect(() => {
    if (providers.length === 0) return;
    const def = providers.find((p) => p.is_default);
    setProviderId((cur) => cur || (def ? def.id : providers[0].id));
  }, [providers]);

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

  const selected = providers.find((p) => p.id === providerId);

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
    <div className="border-t p-3">
      <div className="mb-2 flex gap-4 text-sm items-center">
        <select
          className="max-w-[260px] rounded border px-2 py-1 text-xs"
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
        </select>
        <label className={'flex items-center gap-1 ' + (!kbId ? 'text-gray-400' : '')}>
          <input
            type="checkbox"
            checked={!!kbId && useRag}
            disabled={!kbId}
            onChange={(e) => setUseRag(e.target.checked)}
          />
          本条检索
        </label>
        {/* 选择工作目录 */}
        <button
          className="ml-auto text-xs border rounded px-2 py-1 hover:bg-gray-100 flex items-center gap-1 max-w-xs"
          onClick={pickDirectory}
          title="选择工作目录"
        >
          <span>📁</span>
          <span className="truncate">
            {workDir ? workDir : '选择工作目录'}
          </span>
        </button>
      </div>
      <div className="flex gap-2">
        <textarea
          className="flex-1 border rounded p-2 text-sm resize-none"
          rows={2}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          placeholder="Send a message…"
        />
        {/* 模型列表：展示 model name，选中后用该模型对话 */}
        <div className="flex flex-col gap-1 w-40">
          <select
            className="border rounded p-1.5 text-sm"
            value={providerId}
            onChange={(e) => setProviderId(e.target.value)}
            title="选择对话使用的模型"
          >
            {providers.length === 0 && <option value="">未配置模型</option>}
            {providers.map((p) => (
              <option key={p.id} value={p.id}>
                {p.chat_model}
              </option>
            ))}
          </select>
          {selected && (
            <span className="text-[10px] text-gray-400 truncate text-center">
              {selected.name}
            </span>
          )}
        </div>
        <button
          className="bg-blue-600 text-white px-4 rounded disabled:opacity-50"
          onClick={submit}
          disabled={!text.trim() || streaming || (!sessionId && !providerId)}
        >
          Send
        </button>
      </div>
    </div>
  );
}
