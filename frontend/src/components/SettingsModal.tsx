import { useEffect, useState } from 'react';
import { ProviderSettings } from './ProviderSettings';
import { Icon, type IconName } from './Icon';
import { MCPSettings } from './MCPSettings';
import { SkillsSettings } from './SkillsSettings';
import { MemoryPanel } from './MemoryPanel';

type TabKey = 'model' | 'mcp' | 'skills' | 'memory';

const TABS: { key: TabKey; label: string; desc: string; icon: IconName }[] = [
  { key: 'model', label: '模型', desc: '配置大语言模型供应商', icon: 'settings' },
  { key: 'mcp', label: 'MCP', desc: 'Model Context Protocol 服务器', icon: 'terminal' },
  { key: 'skills', label: 'Skills', desc: '技能管理', icon: 'sparkles' },
  { key: 'memory', label: '记忆', desc: '跨会话的事实记忆', icon: 'brain' },
];

export function SettingsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [tab, setTab] = useState<TabKey>('model');

  // Escape 关闭
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  const active = TABS.find((t) => t.key === tab)!;

  return (
    <div
      className="fixed inset-0 z-50 flex animate-fade-in items-center justify-center bg-black/40 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="flex h-[560px] max-h-[88vh] w-[760px] max-w-[92vw] animate-scale-in overflow-hidden rounded-2xl border border-border bg-card shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 左侧子菜单 */}
        <div className="flex w-48 flex-col border-r border-border bg-muted/30">
          <div className="border-b border-border px-4 py-3.5 text-base font-semibold text-foreground">
            设置
          </div>
          <nav className="flex-1 gap-1 p-2">
            {TABS.map((t) => (
              <button
                key={t.key}
                className={
                  'relative flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-sm transition-colors ' +
                  (t.key === tab
                    ? 'bg-card font-medium text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-card/60 hover:text-foreground')
                }
                onClick={() => setTab(t.key)}
              >
                {t.key === tab && (
                  <span className="absolute bottom-1.5 left-0 top-1.5 w-0.5 rounded-full bg-primary" />
                )}
                <Icon name={t.icon} size={16} />
                {t.label}
              </button>
            ))}
          </nav>
        </div>

        {/* 右侧内容 */}
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex items-center justify-between border-b border-border px-5 py-3.5">
            <div>
              <h2 className="font-semibold text-foreground">{active.label}</h2>
              <p className="text-xs text-muted-foreground">{active.desc}</p>
            </div>
            <button
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              onClick={onClose}
              aria-label="关闭"
            >
              <Icon name="x" size={18} />
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-5">
            {tab === 'model' && <ProviderSettings />}
            {tab === 'mcp' && <MCPSettings />}
            {tab === 'skills' && <SkillsSettings />}
            {tab === 'memory' && <MemoryPanel />}
          </div>
        </div>
      </div>
    </div>
  );
}
