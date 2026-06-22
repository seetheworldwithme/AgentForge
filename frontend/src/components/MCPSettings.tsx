import { useEffect, useState } from 'react';
import { api } from '../lib/api';
import type { MCPServer } from '../types';
import { Icon } from './Icon';

type Draft = Omit<MCPServer, 'args' | 'env'> & {
  argsText: string;
  envText: string;
};

export function MCPSettings() {
  const [servers, setServers] = useState<Draft[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState('');

  const load = async () => {
    setLoading(true);
    setStatus('');
    try {
      const items = await api.listMCPServers();
      setServers(items.map(toDraft));
    } catch (e) {
      setStatus(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const addServer = () => {
    setServers((items) => [
      ...items,
      {
        id: `srv_${Date.now()}`,
        name: '新 MCP 服务',
        command: '',
        argsText: '',
        envText: '',
        enabled: true,
      },
    ]);
  };

  const updateServer = <K extends keyof Draft>(id: string, key: K, value: Draft[K]) => {
    setServers((items) => items.map((item) => (item.id === id ? { ...item, [key]: value } : item)));
    setStatus('');
  };

  const removeServer = (id: string) => {
    setServers((items) => items.filter((item) => item.id !== id));
    setStatus('');
  };

  const save = async () => {
    setSaving(true);
    setStatus('');
    try {
      const payload = servers.map(fromDraft);
      const saved = await api.saveMCPServers(payload);
      setServers(saved.map(toDraft));
      setStatus('已保存');
    } catch (e) {
      setStatus(e instanceof Error ? e.message : String(e));
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
      <div className="flex items-center justify-between">
        <div className="text-sm font-medium text-foreground">MCP 服务</div>
        <div className="flex items-center gap-2">
          <button className="btn-outline !px-2.5 !py-1.5 text-xs" onClick={addServer}>
            <Icon name="plus" size={13} />
            添加
          </button>
          <button className="btn-primary !px-2.5 !py-1.5 text-xs" onClick={save} disabled={saving}>
            {saving ? <Icon name="loader" size={13} className="animate-spin" /> : <Icon name="check" size={13} />}
            保存
          </button>
        </div>
      </div>

      {status && (
        <div className="rounded-md border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          {status}
        </div>
      )}

      {servers.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border bg-muted/30 px-4 py-10 text-center text-sm text-muted-foreground">
          未配置 MCP 服务
        </div>
      ) : (
        <div className="space-y-3">
          {servers.map((server) => (
            <div key={server.id} className="rounded-xl border border-border bg-card p-3">
              <div className="mb-3 flex items-center gap-2">
                <div className="grid h-8 w-8 place-items-center rounded-lg bg-primary/10 text-primary">
                  <Icon name="terminal" size={17} />
                </div>
                <input
                  className="field h-9 flex-1"
                  value={server.name}
                  onChange={(e) => updateServer(server.id, 'name', e.target.value)}
                  placeholder="名称"
                />
                <button
                  className={
                    'relative h-6 w-11 shrink-0 rounded-full transition-colors ' +
                    (server.enabled ? 'bg-primary' : 'bg-muted-foreground/30')
                  }
                  onClick={() => updateServer(server.id, 'enabled', !server.enabled)}
                  aria-label={server.enabled ? '停用 MCP 服务' : '启用 MCP 服务'}
                >
                  <span
                    className={
                      'absolute top-1 h-4 w-4 rounded-full bg-white shadow transition-transform ' +
                      (server.enabled ? 'translate-x-6' : 'translate-x-1')
                    }
                  />
                </button>
                <button
                  className="btn-danger !px-2 !py-1.5 text-xs"
                  onClick={() => removeServer(server.id)}
                  aria-label="删除 MCP 服务"
                >
                  <Icon name="trash" size={13} />
                </button>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-muted-foreground">命令</span>
                  <input
                    className="field font-mono"
                    value={server.command}
                    onChange={(e) => updateServer(server.id, 'command', e.target.value)}
                    placeholder="npx"
                  />
                </label>
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-muted-foreground">ID</span>
                  <input
                    className="field font-mono"
                    value={server.id}
                    onChange={(e) => updateServer(server.id, 'id', e.target.value)}
                    placeholder="srv_filesystem"
                  />
                </label>
              </div>

              <div className="mt-3 grid gap-3 sm:grid-cols-2">
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-muted-foreground">参数</span>
                  <textarea
                    className="field min-h-20 resize-y font-mono"
                    value={server.argsText}
                    onChange={(e) => updateServer(server.id, 'argsText', e.target.value)}
                    placeholder="-y&#10;@modelcontextprotocol/server-filesystem&#10;/path/to/workdir"
                  />
                </label>
                <label className="block">
                  <span className="mb-1 block text-xs font-medium text-muted-foreground">环境变量</span>
                  <textarea
                    className="field min-h-20 resize-y font-mono"
                    value={server.envText}
                    onChange={(e) => updateServer(server.id, 'envText', e.target.value)}
                    placeholder="API_KEY=value"
                  />
                </label>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function toDraft(server: MCPServer): Draft {
  return {
    id: server.id,
    name: server.name,
    command: server.command,
    enabled: server.enabled,
    argsText: (server.args ?? []).join('\n'),
    envText: Object.entries(server.env ?? {})
      .map(([key, value]) => `${key}=${value}`)
      .join('\n'),
  };
}

function fromDraft(draft: Draft): MCPServer {
  return {
    id: draft.id.trim(),
    name: draft.name.trim(),
    command: draft.command.trim(),
    enabled: draft.enabled,
    args: draft.argsText
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean),
    env: parseEnv(draft.envText),
  };
}

function parseEnv(text: string): Record<string, string> {
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
