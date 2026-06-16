import { ProviderSettings } from './ProviderSettings';

export function SettingsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg p-6 w-[500px] max-h-[80vh] overflow-y-auto">
        <div className="flex justify-between mb-4">
          <h1 className="text-xl font-bold">Settings</h1>
          <button onClick={onClose}>×</button>
        </div>
        <ProviderSettings onSaved={onClose} />
      </div>
    </div>
  );
}
