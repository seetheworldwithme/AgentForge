import { useEffect, useState } from 'react';
import { useConfirmModalStore } from '../stores/confirmModalStore';
import { api } from '../lib/api';
import type { MCPServer } from '../types';
import { Icon } from './Icon';

type ViewMode = 'form' | 'json';
type Transport = 'stdio' | 'sse';
type Status = { kind: 'idle' } | { kind: 'success' | 'error'; message: string };

type Draft = Omit<MCPServer, 'args' | 'env' | 'headers' | 'transport'> & {
  transport: Transport;
  argsText: string;
  envText: string;
  headersText: string;
};

const TRANSPORT_LABEL: Record<Transport, string> = {
  stdio: 'stdio',
  sse: 'SSE/HTTP',
};

export function MCPSettings() {
  const [mode, setMode] = useState<ViewMode>('form');
  const [servers, setServers] = useState<Draft[]>([]);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [jsonText, setJsonText] = useState('');
  const [configPath, setConfigPath] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<Status>({ kind: 'idle' });
  const confirm = useConfirmModalStore((s) => s.confirm);

  const editing = servers.find((server) => server.id === editingId) ?? null;

  const load = async () => {
    setLoading(true);
    setStatus({ kind: 'idle' });
    try {
      const [items, config, path] = await Promise.all([
        api.listMCPServers(),
        api.getMCPConfig(),
        api.getMCPConfigPath(),
      ]);
      setServers(items.map(toDraft));
      setJsonText(JSON.stringify(config, null, 2));
      setConfigPath(path.path);
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const addServer = (transport: Transport) => {
    const draft: Draft = {
      id: `srv_${Date.now()}`,
      name: transport === 'stdio' ? '本地 MCP 服务' : '远程 MCP 服务',
      transport,
      command: '',
      argsText: '',
      envText: '',
      url: '',
      headersText: '',
      enabled: true,
    };
    setServers((items) => [...items, draft]);
    setEditingId(draft.id);
    setMode('form');
    setStatus({ kind: 'idle' });
  };

  const updateServer = <K extends keyof Draft>(id: string, key: K, value: Draft[K]) => {
    setServers((items) => items.map((item) => (item.id === id ? { ...item, [key]: value } : item)));
    setStatus({ kind: 'idle' });
  };

  const removeServer = async (id: string) => {
    const ok = await confirm({
      title: '删除该 MCP 服务？',
      message: '保存配置后生效，操作不可恢复。',
    });
    if (!ok) return;
    setServers((items) => items.filter((item) => item.id !== id));
    if (editingId === id) setEditingId(null);
    setStatus({ kind: 'idle' });
  };

  const save = async () => {
    setSaving(true);
    setStatus({ kind: 'idle' });
    try {
      if (mode === 'json') {
        const parsed = JSON.parse(jsonText);
        const saved = await api.saveMCPConfig(parsed);
        setJsonText(JSON.stringify(saved, null, 2));
        setServers((await api.listMCPServers()).map(toDraft));
      } else {
        const saved = await api.saveMCPServers(servers.map(fromDraft));
        setServers(saved.map(toDraft));
        setJsonText(JSON.stringify(await api.getMCPConfig(), null, 2));
      }
      setStatus({ kind: 'success', message: '已保存并验证可用' });
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    } finally {
      setSaving(false);
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
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-sm font-medium text-foreground">MCP 服务</div>
          <div className="mt-0.5 truncate font-mono text-[11px] text-muted-foreground">
            {configPath || '~/.agentforge/mcp.json'}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Segmented
            value={mode}
            options={[
              { value: 'form', label: '表单' },
              { value: 'json', label: 'JSON' },
            ]}
            onChange={(value) => setMode(value as ViewMode)}
          />
          <button className="btn-primary !px-2.5 !py-1.5 text-xs" onClick={save} disabled={saving}>
            {saving ? <Icon name="loader" size={13} className="animate-spin" /> : <Icon name="check" size={13} />}
            保存
          </button>
        </div>
      </div>

      {status.kind !== 'idle' && (
        <div
          className={
            'rounded-md border px-3 py-2 text-xs ' +
            (status.kind === 'error'
              ? 'border-destructive/30 bg-destructive/10 text-destructive'
              : 'border-success/30 bg-success/10 text-success')
          }
        >
          {status.message}
        </div>
      )}

      {mode === 'json' ? (
        <JSONEditor value={jsonText} onChange={setJsonText} />
      ) : (
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <button className="btn-outline !px-2.5 !py-1.5 text-xs" onClick={() => addServer('stdio')}>
              <Icon name="plus" size={13} />
              stdio
            </button>
            <button className="btn-outline !px-2.5 !py-1.5 text-xs" onClick={() => addServer('sse')}>
              <Icon name="plus" size={13} />
              SSE/HTTP
            </button>
          </div>

          {servers.length === 0 ? (
            <div className="rounded-xl border border-dashed border-border bg-muted/30 px-4 py-10 text-center text-sm text-muted-foreground">
              未配置 MCP 服务
            </div>
          ) : (
            <div className="space-y-2">
              {servers.map((server) => (
                <ServerRow
                  key={server.id}
                  server={server}
                  onEdit={() => setEditingId(server.id)}
                  onRemove={() => removeServer(server.id)}
                  onToggle={() => updateServer(server.id, 'enabled', !server.enabled)}
                />
              ))}
            </div>
          )}
        </div>
      )}

      {editing && (
        <EditDialog
          server={editing}
          onClose={() => setEditingId(null)}
          onUpdate={updateServer}
        />
      )}
    </div>
  );
}

function ServerRow({
  server,
  onEdit,
  onRemove,
  onToggle,
}: {
  server: Draft;
  onEdit: () => void;
  onRemove: () => void;
  onToggle: () => void;
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-border bg-card px-3 py-2.5">
      <div className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
        <Icon name={server.transport === 'stdio' ? 'terminal' : 'database'} size={18} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium text-foreground">{server.name || server.id}</span>
          <span className="status-pill bg-muted text-muted-foreground">{TRANSPORT_LABEL[server.transport]}</span>
          <span
            className={
              'status-pill ' +
              (server.enabled ? 'bg-success/10 text-success' : 'bg-muted text-muted-foreground')
            }
          >
            {server.enabled ? '启用' : '停用'}
          </span>
        </div>
        <div className="truncate font-mono text-[11px] text-muted-foreground">{server.id}</div>
      </div>
      <Switch enabled={server.enabled} onClick={onToggle} label={server.enabled ? '停用 MCP 服务' : '启用 MCP 服务'} />
      <button className="btn-outline !px-2 !py-1.5 text-xs" onClick={onEdit}>
        <Icon name="pencil" size={13} />
        编辑
      </button>
      <button className="btn-danger !px-2 !py-1.5 text-xs" onClick={onRemove} aria-label="删除 MCP 服务">
        <Icon name="trash" size={13} />
      </button>
    </div>
  );
}

function EditDialog({
  server,
  onClose,
  onUpdate,
}: {
  server: Draft;
  onClose: () => void;
  onUpdate: <K extends keyof Draft>(id: string, key: K, value: Draft[K]) => void;
}) {
  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 px-4" onClick={onClose}>
      <div
        className="max-h-[86vh] w-full max-w-xl overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-semibold text-foreground">编辑 MCP 服务</div>
            <div className="mt-0.5 text-xs text-muted-foreground">保存时会验证启用状态的 MCP 是否可用。</div>
          </div>
          <button className="btn-ghost !p-1.5" onClick={onClose} aria-label="关闭">
            <Icon name="x" size={16} />
          </button>
        </div>

        <div className="space-y-3">
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted-foreground">名称</span>
            <input
              className="field"
              value={server.name}
              onChange={(e) => onUpdate(server.id, 'name', e.target.value)}
              placeholder="MCP 服务名称"
            />
          </label>

          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted-foreground">类型</span>
            <Segmented
              value={server.transport}
              options={[
                { value: 'stdio', label: 'stdio' },
                { value: 'sse', label: 'SSE/HTTP' },
              ]}
              onChange={(value) => onUpdate(server.id, 'transport', value as Transport)}
            />
          </label>

          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted-foreground">ID</span>
            <input
              className="field font-mono"
              value={server.id}
              onChange={(e) => onUpdate(server.id, 'id', e.target.value)}
              placeholder="srv_filesystem"
            />
          </label>

          {server.transport === 'stdio' ? (
            <>
              <label className="block">
                <span className="mb-1 block text-xs font-medium text-muted-foreground">命令</span>
                <input
                  className="field font-mono"
                  value={server.command}
                  onChange={(e) => onUpdate(server.id, 'command', e.target.value)}
                  placeholder="npx"
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-xs font-medium text-muted-foreground">参数</span>
                <textarea
                  className="field min-h-20 resize-y font-mono"
                  value={server.argsText}
                  onChange={(e) => onUpdate(server.id, 'argsText', e.target.value)}
                  placeholder={'-y\n@modelcontextprotocol/server-filesystem\n/path/to/workdir'}
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-xs font-medium text-muted-foreground">环境变量</span>
                <textarea
                  className="field min-h-20 resize-y font-mono"
                  value={server.envText}
                  onChange={(e) => onUpdate(server.id, 'envText', e.target.value)}
                  placeholder="API_KEY=value"
                />
              </label>
            </>
          ) : (
            <>
              <label className="block">
                <span className="mb-1 block text-xs font-medium text-muted-foreground">URL</span>
                <input
                  className="field font-mono"
                  value={server.url}
                  onChange={(e) => onUpdate(server.id, 'url', e.target.value)}
                  placeholder="http://127.0.0.1:3000/mcp"
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-xs font-medium text-muted-foreground">请求头</span>
                <textarea
                  className="field min-h-20 resize-y font-mono"
                  value={server.headersText}
                  onChange={(e) => onUpdate(server.id, 'headersText', e.target.value)}
                  placeholder="Authorization=Bearer token"
                />
              </label>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function JSONEditor({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className="rounded-xl border border-border bg-card p-3">
      <label className="block">
        <span className="mb-1 block text-xs font-medium text-muted-foreground">mcp.json</span>
        <textarea
          className="field min-h-[360px] resize-y font-mono text-xs leading-5"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          spellCheck={false}
        />
      </label>
    </div>
  );
}

function Segmented({
  value,
  options,
  onChange,
}: {
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <div className="inline-flex rounded-md border border-border bg-muted/40 p-0.5">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className={
            'rounded px-2 py-1 text-xs transition-colors ' +
            (value === option.value
              ? 'bg-card text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground')
          }
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function Switch({ enabled, onClick, label }: { enabled: boolean; onClick: () => void; label: string }) {
  return (
    <button
      className={
        'relative h-6 w-11 shrink-0 rounded-full transition-colors ' +
        (enabled ? 'bg-primary' : 'bg-muted-foreground/30')
      }
      onClick={onClick}
      aria-label={label}
    >
      <span
        className={
          'absolute left-1 top-1 h-4 w-4 rounded-full bg-white shadow transition-transform ' +
          (enabled ? 'translate-x-5' : 'translate-x-0')
        }
      />
    </button>
  );
}

function toDraft(server: MCPServer): Draft {
  const transport = (server.transport === 'sse' ? 'sse' : 'stdio') as Transport;
  return {
    id: server.id ?? '',
    name: server.name ?? '',
    transport,
    command: server.command ?? '',
    enabled: server.enabled,
    url: server.url ?? '',
    argsText: (server.args ?? []).join('\n'),
    envText: stringifyMap(server.env ?? {}),
    headersText: stringifyMap(server.headers ?? {}),
  };
}

function fromDraft(draft: Draft): MCPServer {
  return {
    id: draft.id.trim(),
    name: draft.name.trim(),
    transport: draft.transport,
    command: draft.command.trim(),
    enabled: draft.enabled,
    url: draft.url.trim(),
    args: draft.argsText
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean),
    env: parseMap(draft.envText),
    headers: parseMap(draft.headersText),
  };
}

function stringifyMap(map: Record<string, string>): string {
  return Object.entries(map)
    .map(([key, value]) => `${key}=${value}`)
    .join('\n');
}

function parseMap(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const idx = trimmed.indexOf('=');
    if (idx <= 0) continue;
    out[trimmed.slice(0, idx)] = trimmed.slice(idx + 1);
  }
  return out;
}
