import { useEffect, useMemo, useRef, useState } from 'react';
import { useKBStore } from '../stores/kbStore';
import { useConfigStore } from '../stores/configStore';
import type { Document, KnowledgeBase } from '../types';

const DEFAULT_CHUNK_SIZE = 800;
const DEFAULT_OVERLAP = 100;

export function KnowledgeWorkbench() {
  const kbs = useKBStore((s) => s.kbs);
  const docsByKb = useKBStore((s) => s.docsByKb);
  const chunksByDoc = useKBStore((s) => s.chunksByDoc);
  const retrieveHits = useKBStore((s) => s.retrieveHits);
  const loadKBs = useKBStore((s) => s.load);
  const createKB = useKBStore((s) => s.create);
  const updateKB = useKBStore((s) => s.update);
  const removeKB = useKBStore((s) => s.remove);
  const loadDocs = useKBStore((s) => s.loadDocs);
  const upload = useKBStore((s) => s.upload);
  const deleteDoc = useKBStore((s) => s.deleteDoc);
  const retryDoc = useKBStore((s) => s.retryDoc);
  const loadChunks = useKBStore((s) => s.loadChunks);
  const previewChunks = useKBStore((s) => s.previewChunks);
  const retrieve = useKBStore((s) => s.retrieve);

  const providers = useConfigStore((s) => s.providers);
  const providersLoaded = useConfigStore((s) => s.loaded);
  const loadProviders = useConfigStore((s) => s.load);

  const [activeId, setActiveId] = useState<string>('');
  const [query, setQuery] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [providerId, setProviderId] = useState('');
  const [chatProviderId, setChatProviderId] = useState('');
  const [chunkSize, setChunkSize] = useState(DEFAULT_CHUNK_SIZE);
  const [overlap, setOverlap] = useState(DEFAULT_OVERLAP);
  const [search, setSearch] = useState('');
  const [previewText, setPreviewText] = useState('');
  const [preview, setPreview] = useState<{ ordinal: number; text: string }[]>([]);
  const [expandedDoc, setExpandedDoc] = useState('');
  const [dragging, setDragging] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    loadKBs();
  }, [loadKBs]);

  useEffect(() => {
    if (!providersLoaded) loadProviders();
  }, [providersLoaded, loadProviders]);

  useEffect(() => {
    if (!activeId && kbs.length > 0) setActiveId(kbs[0].id);
  }, [activeId, kbs]);

  const active = kbs.find((k) => k.id === activeId);
  const docs = active ? docsByKb[active.id] ?? [] : [];

  useEffect(() => {
    if (active) loadDocs(active.id);
  }, [active?.id, loadDocs]);

  useEffect(() => {
    if (!active) return;
    setName(active.name);
    setDescription(active.description ?? '');
    setProviderId(active.embed_provider_id ?? '');
    setChatProviderId(active.chat_provider_id ?? '');
    setChunkSize(active.chunk_size || DEFAULT_CHUNK_SIZE);
    setOverlap(active.chunk_overlap || DEFAULT_OVERLAP);
  }, [active?.id]);

  const filtered = useMemo(() => {
    const term = search.trim().toLowerCase();
    if (!term) return kbs;
    return kbs.filter((k) =>
      [k.name, k.description].filter(Boolean).some((v) => v!.toLowerCase().includes(term)),
    );
  }, [kbs, search]);

  const dirty =
    !!active &&
    (name !== active.name ||
      description !== (active.description ?? '') ||
      providerId !== (active.embed_provider_id ?? '') ||
      chatProviderId !== (active.chat_provider_id ?? '') ||
      chunkSize !== (active.chunk_size || DEFAULT_CHUNK_SIZE) ||
      overlap !== (active.chunk_overlap || DEFAULT_OVERLAP));

  const create = async () => {
    const firstProvider = providers.find((p) => p.is_default) ?? providers[0];
    await createKB({
      name: '未命名知识库',
      description: '',
      embed_provider_id: firstProvider?.id ?? '',
      chunk_size: DEFAULT_CHUNK_SIZE,
      chunk_overlap: DEFAULT_OVERLAP,
    });
  };

  const save = async () => {
    if (!active || !name.trim()) return;
    await updateKB(active.id, {
      name: name.trim(),
      description,
      embed_provider_id: providerId,
      chat_provider_id: chatProviderId,
      chunk_size: chunkSize,
      chunk_overlap: overlap,
    });
  };

  const uploadFiles = async (files: FileList | File[]) => {
    if (!active) return;
    for (const file of Array.from(files)) {
      await upload(active.id, file);
    }
  };

  const runPreview = async () => {
    if (!active || !previewText.trim()) return;
    setPreview(await previewChunks(active.id, previewText, chunkSize, overlap));
  };

  const runRetrieve = async () => {
    if (!active || !query.trim()) return;
    await retrieve(active.id, query, 5);
  };

  const retryAll = async () => {
    if (!active) return;
    for (const doc of docs) {
      await retryDoc(active.id, doc.id);
    }
  };

  return (
    <div className="flex h-screen flex-1 bg-[#f7f8f5] text-gray-900">
      <aside className="w-72 border-r border-gray-200 bg-[#fbfbf8] p-4">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <div className="text-xs uppercase tracking-[0.18em] text-gray-400">RAG</div>
            <h1 className="text-xl font-semibold">知识库</h1>
          </div>
          <button className="rounded bg-gray-900 px-3 py-1.5 text-sm text-white" onClick={create}>
            新建
          </button>
        </div>
        <input
          className="mb-3 w-full rounded border border-gray-200 bg-white px-3 py-2 text-sm outline-none focus:border-gray-400"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索知识库"
        />
        <div className="space-y-2">
          {filtered.map((kb) => (
            <button
              key={kb.id}
              className={
                'w-full rounded border px-3 py-3 text-left transition ' +
                (kb.id === activeId
                  ? 'border-gray-900 bg-white shadow-sm'
                  : 'border-transparent hover:border-gray-200 hover:bg-white')
              }
              onClick={() => setActiveId(kb.id)}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="truncate text-sm font-medium">{kb.name}</span>
                <span className="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] text-gray-500">
                  {kb.doc_count ?? 0} 文档
                </span>
              </div>
              <div className="mt-1 truncate text-xs text-gray-500">
                {kb.description || '无描述'}
              </div>
              <StatusSummary docs={docsByKb[kb.id] ?? []} />
            </button>
          ))}
        </div>
      </aside>

      <main className="flex min-w-0 flex-1">
        <section className="flex min-w-0 flex-1 flex-col border-r border-gray-200">
          {active ? (
            <>
              <header className="border-b border-gray-200 bg-white px-5 py-4">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0">
                    <h2 className="truncate text-2xl font-semibold">{active.name}</h2>
                    <p className="mt-1 text-sm text-gray-500">{active.description || '无描述'}</p>
                  </div>
                  <button
                    className="rounded border border-red-200 px-3 py-1.5 text-sm text-red-600 hover:bg-red-50"
                    onClick={() => removeKB(active.id)}
                  >
                    删除
                  </button>
                </div>
              </header>
              <div className="flex-1 overflow-y-auto p-5">
                <div
                  className={
                    'mb-5 flex min-h-32 cursor-pointer flex-col items-center justify-center rounded border border-dashed p-6 text-center transition ' +
                    (dragging ? 'border-gray-900 bg-white' : 'border-gray-300 bg-[#fbfbf8]')
                  }
                  onClick={() => fileRef.current?.click()}
                  onDragOver={(e) => {
                    e.preventDefault();
                    setDragging(true);
                  }}
                  onDragLeave={() => setDragging(false)}
                  onDrop={(e) => {
                    e.preventDefault();
                    setDragging(false);
                    uploadFiles(e.dataTransfer.files);
                  }}
                >
                  <div className="text-sm font-medium">上传 txt / md / PDF</div>
                  <div className="mt-1 text-xs text-gray-500">拖拽文件到这里，或点击选择</div>
                  <input
                    ref={fileRef}
                    type="file"
                    multiple
                    accept=".txt,.md,.pdf,text/plain,text/markdown,application/pdf"
                    className="hidden"
                    onChange={(e) => e.target.files && uploadFiles(e.target.files)}
                  />
                </div>

                <div className="overflow-hidden rounded border border-gray-200 bg-white">
                  <div className="grid grid-cols-[1fr_90px_80px_120px] border-b border-gray-100 px-3 py-2 text-xs uppercase text-gray-400">
                    <span>文件</span>
                    <span>状态</span>
                    <span>切片</span>
                    <span className="text-right">操作</span>
                  </div>
                  {docs.length === 0 ? (
                    <div className="px-3 py-8 text-center text-sm text-gray-400">暂无文档</div>
                  ) : (
                    docs.map((doc) => (
                      <DocumentRow
                        key={doc.id}
                        doc={doc}
                        kb={active}
                        expanded={expandedDoc === doc.id}
                        chunks={chunksByDoc[doc.id] ?? []}
                        onToggle={async () => {
                          const next = expandedDoc === doc.id ? '' : doc.id;
                          setExpandedDoc(next);
                          if (next) await loadChunks(active.id, doc.id);
                        }}
                        onRetry={() => retryDoc(active.id, doc.id)}
                        onDelete={() => deleteDoc(active.id, doc.id)}
                      />
                    ))
                  )}
                </div>
              </div>
            </>
          ) : (
            <div className="flex flex-1 items-center justify-center text-gray-400">创建一个知识库</div>
          )}
        </section>

        <aside className="w-[380px] overflow-y-auto bg-white p-5">
          {active && (
            <div className="space-y-6">
              <section>
                <div className="mb-3 flex items-center justify-between">
                  <h3 className="font-semibold">配置</h3>
                  <button
                    className="rounded bg-gray-900 px-3 py-1.5 text-sm text-white disabled:opacity-40"
                    disabled={!dirty || !name.trim()}
                    onClick={save}
                  >
                    保存
                  </button>
                </div>
                <div className="space-y-3">
                  <input className="field" value={name} onChange={(e) => setName(e.target.value)} />
                  <textarea
                    className="field min-h-20 resize-none"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="描述"
                  />
                  <select className="field" value={providerId} onChange={(e) => setProviderId(e.target.value)}>
                    <option value="">未选择 Embedding 模型</option>
                    {providers.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name} · {p.embed_model || '未配置 embed model'}
                      </option>
                    ))}
                  </select>
                  <select className="field" value={chatProviderId} onChange={(e) => setChatProviderId(e.target.value)}>
                    <option value="">未选择 Chat/VL 模型（图片描述用，可选）</option>
                    {providers.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name} · {p.chat_model || '未配置 chat model'}
                      </option>
                    ))}
                  </select>
                  <div className="grid grid-cols-2 gap-3">
                    <NumberField label="切片大小" value={chunkSize} onChange={setChunkSize} />
                    <NumberField label="重叠长度" value={overlap} onChange={setOverlap} />
                  </div>
                </div>
                {dirty && docs.length > 0 && (
                  <div className="mt-3 rounded border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
                    配置变化后需要重建索引
                    <button className="ml-2 underline" onClick={retryAll}>
                      重建全部
                    </button>
                  </div>
                )}
              </section>

              <section>
                <h3 className="mb-3 font-semibold">切片预览</h3>
                <textarea
                  className="field min-h-24 resize-none"
                  value={previewText}
                  onChange={(e) => setPreviewText(e.target.value)}
                  placeholder="粘贴文本"
                />
                <button className="mt-2 rounded border px-3 py-1.5 text-sm" onClick={runPreview}>
                  预览
                </button>
                <div className="mt-3 space-y-2">
                  {preview.slice(0, 5).map((chunk) => (
                    <ChunkBlock key={chunk.ordinal} ordinal={chunk.ordinal} text={chunk.text} />
                  ))}
                </div>
              </section>

              <section>
                <h3 className="mb-3 font-semibold">召回测试</h3>
                <div className="flex gap-2">
                  <input
                    className="field"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') runRetrieve();
                    }}
                    placeholder="输入问题"
                  />
                  <button className="rounded bg-gray-900 px-3 text-sm text-white" onClick={runRetrieve}>
                    测试
                  </button>
                </div>
                <div className="mt-3 space-y-3">
                  {retrieveHits.map((hit) => (
                    <div key={hit.chunk_id} className="rounded border border-gray-200 p-3">
                      <div className="mb-2 flex items-center justify-between gap-2 text-xs text-gray-500">
                        <span className="truncate">{hit.filename} · #{hit.ordinal}</span>
                        <span>相似度 {(hit.similarity * 100).toFixed(1)}%</span>
                      </div>
                      <p className="max-h-28 overflow-y-auto whitespace-pre-wrap text-sm">{hit.text}</p>
                    </div>
                  ))}
                </div>
              </section>
            </div>
          )}
        </aside>
      </main>
    </div>
  );
}

function StatusSummary({ docs }: { docs: Document[] }) {
  const processing = docs.filter((d) => d.status === 'processing').length;
  const failed = docs.filter((d) => d.status === 'failed').length;
  const ready = docs.filter((d) => d.status === 'ready').length;
  return (
    <div className="mt-2 flex gap-1 text-[10px]">
      <span className="rounded bg-emerald-50 px-1.5 py-0.5 text-emerald-700">{ready} ready</span>
      <span className="rounded bg-gray-100 px-1.5 py-0.5 text-gray-600">{processing} processing</span>
      {failed > 0 && <span className="rounded bg-red-50 px-1.5 py-0.5 text-red-600">{failed} failed</span>}
    </div>
  );
}

function DocumentRow({
  doc,
  kb,
  expanded,
  chunks,
  onToggle,
  onRetry,
  onDelete,
}: {
  doc: Document;
  kb: KnowledgeBase;
  expanded: boolean;
  chunks: { ordinal: number; text: string }[];
  onToggle: () => void;
  onRetry: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="border-b border-gray-100 last:border-b-0">
      <div className="grid grid-cols-[1fr_90px_80px_120px] items-center px-3 py-3 text-sm">
        <button className="min-w-0 text-left" onClick={onToggle}>
          <div className="truncate font-medium">{doc.filename}</div>
          <div className="text-xs text-gray-400">{formatSize(doc.file_size)}</div>
          {doc.error && <div className="mt-1 truncate text-xs text-red-600">{doc.error}</div>}
        </button>
        <span className={'text-xs ' + statusClass(doc.status)}>{doc.status}</span>
        <span className="text-xs text-gray-500">{doc.chunk_count}</span>
        <span className="flex justify-end gap-2 text-xs">
          <button className="hover:underline" onClick={onToggle}>
            切片
          </button>
          <button className="hover:underline" onClick={onRetry}>
            重试
          </button>
          <button className="text-red-600 hover:underline" onClick={onDelete}>
            删除
          </button>
        </span>
      </div>
      {expanded && (
        <div className="bg-gray-50 px-3 py-3">
          <div className="mb-2 text-xs text-gray-400">{kb.name}</div>
          <div className="space-y-2">
            {chunks.length === 0 ? (
              <div className="text-xs text-gray-400">暂无切片</div>
            ) : (
              chunks.slice(0, 8).map((chunk) => (
                <ChunkBlock key={chunk.ordinal} ordinal={chunk.ordinal} text={chunk.text} />
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function NumberField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs text-gray-500">{label}</span>
      <input
        className="field"
        type="number"
        min={0}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
      />
    </label>
  );
}

function ChunkBlock({ ordinal, text }: { ordinal: number; text: string }) {
  return (
    <div className="rounded border border-gray-200 bg-white p-2">
      <div className="mb-1 text-[10px] uppercase text-gray-400">chunk {ordinal}</div>
      <div className="max-h-28 overflow-y-auto whitespace-pre-wrap text-xs leading-5 text-gray-700">{text}</div>
    </div>
  );
}

function formatSize(size: number) {
  if (!size) return '0 B';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function statusClass(status: string) {
  if (status === 'ready') return 'text-emerald-600';
  if (status === 'failed') return 'text-red-600';
  return 'text-gray-500';
}
