import { useState } from 'react';
import { ProviderSettings } from './ProviderSettings';

type TabKey = 'model' | 'mcp' | 'skills';

const TABS: { key: TabKey; label: string; desc: string }[] = [
  { key: 'model', label: '模型', desc: '配置大语言模型供应商' },
  { key: 'mcp', label: 'MCP', desc: 'Model Context Protocol 服务器' },
  { key: 'skills', label: 'Skills', desc: '技能管理' },
];

function Placeholder({ title, desc }: { title: string; desc: string }) {
  return (
    <div className="flex flex-col items-center justify-center h-full text-center text-gray-400 py-16">
      <div className="text-2xl mb-2">{title}</div>
      <p className="text-sm">{desc}</p>
      <p className="text-xs mt-1">（即将支持）</p>
    </div>
  );
}

export function SettingsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [tab, setTab] = useState<TabKey>('model');
  if (!open) return null;

  const active = TABS.find((t) => t.key === tab)!;

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg w-[760px] h-[560px] max-w-[92vw] max-h-[88vh] flex overflow-hidden">
        {/* 左侧子菜单 */}
        <div className="w-44 border-r bg-gray-50 flex flex-col">
          <div className="px-4 py-3 font-bold text-lg border-b">设置</div>
          <nav className="flex-1 py-2">
            {TABS.map((t) => (
              <button
                key={t.key}
                className={
                  'w-full text-left px-4 py-2.5 text-sm transition-colors ' +
                  (t.key === tab
                    ? 'bg-blue-100 text-blue-700 font-medium border-l-2 border-blue-600'
                    : 'hover:bg-gray-200 text-gray-700')
                }
                onClick={() => setTab(t.key)}
              >
                {t.label}
              </button>
            ))}
          </nav>
        </div>

        {/* 右侧内容 */}
        <div className="flex-1 flex flex-col min-w-0">
          <div className="flex items-center justify-between px-5 py-3 border-b">
            <div>
              <h2 className="font-semibold">{active.label}</h2>
              <p className="text-xs text-gray-500">{active.desc}</p>
            </div>
            <button
              className="text-gray-500 hover:text-gray-800 text-2xl leading-none"
              onClick={onClose}
              aria-label="关闭"
            >
              ×
            </button>
          </div>
          <div className="flex-1 overflow-y-auto p-5">
            {tab === 'model' && <ProviderSettings />}
            {tab === 'mcp' && <Placeholder title="MCP" desc="在此管理 MCP 服务器连接" />}
            {tab === 'skills' && <Placeholder title="Skills" desc="在此管理可用技能" />}
          </div>
        </div>
      </div>
    </div>
  );
}
