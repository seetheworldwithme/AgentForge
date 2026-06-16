import { useConfirmStore } from '../stores/confirmStore';

export function ConfirmDialog() {
  const pending = useConfirmStore((s) => s.pending);
  const respond = useConfirmStore((s) => s.respond);
  if (pending.length === 0) return null;
  const req = pending[0];
  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-[100]">
      <div className="bg-white rounded-lg p-6 w-[420px]">
        <h2 className="text-lg font-bold mb-2">Allow tool execution?</h2>
        <div className="bg-gray-100 rounded p-2 mb-4 font-mono text-sm">
          <div className="font-semibold">{req.tool}</div>
          <pre className="whitespace-pre-wrap">{JSON.stringify(req.input, null, 2)}</pre>
        </div>
        <div className="flex gap-2 justify-end">
          <button
            className="border rounded px-3 py-1 text-sm"
            onClick={() => respond(req.request_id, 'deny', 'never')}
          >
            Deny
          </button>
          <button
            className="border rounded px-3 py-1 text-sm"
            onClick={() => respond(req.request_id, 'allow', 'session')}
          >
            Allow (session)
          </button>
          <button
            className="bg-blue-600 text-white rounded px-3 py-1 text-sm"
            onClick={() => respond(req.request_id, 'allow', 'never')}
          >
            Allow once
          </button>
        </div>
      </div>
    </div>
  );
}
