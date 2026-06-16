import { useEffect, useState } from 'react';
import { Sidebar } from './components/Sidebar';
import { ChatView } from './components/ChatView';
import { SettingsModal } from './components/SettingsModal';
import { KBManager } from './components/KBManager';
import { ConfirmDialog } from './components/ConfirmDialog';
import { useConfigStore } from './stores/configStore';

export default function App() {
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [kbOpen, setKbOpen] = useState(false);
  const loadConfig = useConfigStore((s) => s.load);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  return (
    <div className="flex h-screen">
      <Sidebar onOpenSettings={() => setSettingsOpen(true)} onOpenKB={() => setKbOpen(true)} />
      <ChatView />
      <SettingsModal open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      <KBManager open={kbOpen} onClose={() => setKbOpen(false)} />
      <ConfirmDialog />
    </div>
  );
}
