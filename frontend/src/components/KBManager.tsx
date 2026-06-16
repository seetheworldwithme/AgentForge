import { useEffect, useRef, useState } from 'react';
import { useKBStore } from '../stores/kbStore';

export function KBManager({ open, onClose }: { open: boolean; onClose: () => void }) {
  const kbs = useKBStore((s) => s.kbs);
  const docsByKb = useKBStore((s) => s.docsByKb);
  const load = useKBStore((s) => s.load);
  const create = useKBStore((s) => s.create);
  const remove = useKBStore((s) => s.remove);
  const loadDocs = useKBStore((s) => s.loadDocs);
  const upload = useKBStore((s) => s.upload);
  const [name, setName] = useState('');
  const fileRef = useRef<HTMLInputElement>(null);
  const [activeKb, setActiveKb] = useState<string | null>(null);

  useEffect(() => {
    if (open) load();
  }, [open, load]);
  useEffect(() => {
    if (activeKb) loadDocs(activeKb);
  }, [activeKb, loadDocs]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
      <div className="bg-white rounded-lg p-6 w-[600px] max-h-[80vh] overflow-y-auto">
        <div className="flex justify-between mb-4">
          <h1 className="text-xl font-bold">Knowledge Bases</h1>
          <button onClick={onClose}>×</button>
        </div>
        <div className="flex gap-2 mb-4">
          <input
            className="flex-1 border rounded p-1 text-sm"
            placeholder="New KB name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <button
            className="bg-blue-600 text-white rounded px-3 text-sm"
            onClick={async () => {
              if (name) {
                await create({ name });
                setName('');
              }
            }}
          >
            Create
          </button>
        </div>
        <div className="space-y-2">
          {kbs.map((kb) => (
            <div key={kb.id} className="border rounded p-2">
              <div className="flex items-center">
                <span
                  className="flex-1 font-medium cursor-pointer"
                  onClick={() => setActiveKb(activeKb === kb.id ? null : kb.id)}
                >
                  {kb.name}
                </span>
                <button className="text-red-500 text-sm" onClick={() => remove(kb.id)}>
                  Delete
                </button>
              </div>
              {activeKb === kb.id && (
                <div className="mt-2 ml-4 space-y-1">
                  <input ref={fileRef} type="file" className="text-xs" />
                  <button
                    className="ml-2 text-xs bg-gray-200 rounded px-2 py-1"
                    onClick={() =>
                      fileRef.current?.files?.[0] && upload(kb.id, fileRef.current.files[0])
                    }
                  >
                    Upload
                  </button>
                  {(docsByKb[kb.id] ?? []).map((d) => (
                    <div key={d.ID} className="text-xs flex justify-between">
                      <span>{d.Filename}</span>
                      <span
                        className={
                          d.Status === 'ready'
                            ? 'text-green-600'
                            : d.Status === 'failed'
                              ? 'text-red-600'
                              : 'text-gray-500'
                        }
                      >
                        {d.Status}
                        {d.ChunkCount ? ` (${d.ChunkCount})` : ''}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
