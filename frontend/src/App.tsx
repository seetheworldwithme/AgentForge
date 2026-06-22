import { useEffect, useState } from 'react';
import { Sidebar } from './components/Sidebar';
import { ChatView } from './components/ChatView';
import { SettingsModal } from './components/SettingsModal';
import { KnowledgeWorkbench } from './components/KnowledgeWorkbench';
import { ConfirmDialog } from './components/ConfirmDialog';
import { useConfigStore } from './stores/configStore';
import { useThemeStore } from './stores/themeStore';

export default function App() {
  const [view, setView] = useState<'chat' | 'knowledge'>('chat');
  const [settingsOpen, setSettingsOpen] = useState(false);
  const loadConfig = useConfigStore((s) => s.load);
  const initTheme = useThemeStore((s) => s.init);

  useEffect(() => {
    initTheme();
    loadConfig();
  }, [initTheme, loadConfig]);

  return (
    <div className="flex h-screen overflow-hidden bg-background text-foreground">
      <Sidebar
        activeView={view}
        onViewChange={setView}
        onOpenSettings={() => setSettingsOpen(true)}
      />
      {view === 'chat' ? <ChatView /> : <KnowledgeWorkbench />}
      <SettingsModal open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      <ConfirmDialog />
    </div>
  );
}
