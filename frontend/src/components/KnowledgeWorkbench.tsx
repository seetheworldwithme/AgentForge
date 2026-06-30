import { useEffect, useMemo, useRef, useState } from 'react';
import { useKBStore } from '../stores/kbStore';
import { useConfigStore } from '../stores/configStore';
import { useConfirmModalStore } from '../stores/confirmModalStore';
import { vendorLabel } from '../lib/vendors';
import { Icon } from './Icon';
import type { Document, KnowledgeBase } from '../types';

const DEFAULT_CHUNK_SIZE = 500;
const DEFAULT_OVERLAP = 60;

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
  const confirm = useConfirmModalStore((s) => s.confirm);
  const retryDoc = useKBStore((s) => s.retryDoc);
  const pauseDoc = useKBStore((s) => s.pauseDoc);
  const resumeDoc = useKBStore((s) => s.resumeDoc);
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
  const [rerankProviderId, setRerankProviderId] = useState('');
  const [indexMode, setIndexMode] = useState<'chunk' | 'qa'>('chunk');
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
    setRerankProviderId(active.rerank_provider_id ?? '');
    setIndexMode((active.index_mode as 'chunk' | 'qa') ?? 'chunk');
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

  // 按类别拆分 provider：embed 下拉只列向量模型，chat/VL 下拉只列对话模型，rerank 下拉只列重排模型；无 kind 视为 chat
  const embedProviders = providers.filter((p) => p.kind === 'embed');
  const chatProviders = providers.filter((p) => (p.kind ?? 'chat') === 'chat');
  const rerankProviders = providers.filter((p) => p.kind === 'rerank');

  const dirty =
    !!active &&
    (name !== active.name ||
      description !== (active.description ?? '') ||
      providerId !== (active.embed_provider_id ?? '') ||
      chatProviderId !== (active.chat_provider_id ?? '') ||
      rerankProviderId !== (active.rerank_provider_id ?? '') ||
      indexMode !== (active.index_mode ?? 'chunk') ||
      chunkSize !== (active.chunk_size || DEFAULT_CHUNK_SIZE) ||
      overlap !== (active.chunk_overlap || DEFAULT_OVERLAP));

  const create = async () => {
    const firstProvider = embedProviders.find((p) => p.is_default) ?? embedProviders[0] ?? providers[0];
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
      rerank_provider_id: rerankProviderId,
      index_mode: indexMode,
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
    <div className="flex h-full flex-1 bg-background text-foreground">
      {/* 左：知识库列表 */}
      <aside className="flex w-72 shrink-0 flex-col border-r border-border bg-muted/30 p-4">
        <div className="mb-4 flex items-start justify-between">
          <div>
            <div className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground">
              RAG
            </div>
            <h1 className="text-lg font-semibold text-foreground">知识库</h1>
          </div>
          <button className="btn-primary !px-2.5 !py-1.5" onClick={create} title="新建知识库">
            <Icon name="plus" size={15} strokeWidth={2.25} />
            新建
          </button>
        </div>
        <div className="relative mb-3">
          <Icon
            name="search"
            size={14}
            className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground"
          />
          <input
            className="field pl-8"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="搜索知识库"
          />
        </div>
        <div className="-mr-2 flex-1 space-y-1.5 overflow-y-auto pr-1">
          {filtered.length === 0 ? (
            <div className="px-2 py-10 text-center text-xs text-muted-foreground">暂无知识库</div>
          ) : (
            filtered.map((kb) => (
              <button
                key={kb.id}
                className={
                  'w-full rounded-lg border px-3 py-2.5 text-left transition-colors ' +
                  (kb.id === activeId
                    ? 'border-primary/40 bg-card shadow-sm'
                    : 'border-transparent hover:border-border hover:bg-card')
                }
                onClick={() => setActiveId(kb.id)}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-sm font-medium text-foreground">{kb.name}</span>
                  <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-muted-foreground">
                    {kb.doc_count ?? 0} 文档
                  </span>
                </div>
                <div className="mt-0.5 truncate text-xs text-muted-foreground">
                  {kb.description || '无描述'}
                </div>
                <StatusSummary kb={kb} />
              </button>
            ))
          )}
        </div>
      </aside>

      {/* 中：文档列表 */}
      <main className="flex min-w-0 flex-1">
        <section className="flex min-w-0 flex-1 flex-col">
          {active ? (
            <>
              <header className="border-b border-border bg-background px-6 py-4">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0">
                    <h2 className="truncate text-xl font-semibold text-foreground">{active.name}</h2>
                    <p className="mt-0.5 text-sm text-muted-foreground">
                      {active.description || '无描述'}
                    </p>
                  </div>
                  <button
                    className="btn-danger"
                    onClick={async () => {
                      const ok = await confirm({
                        title: `删除知识库「${active.name}」？`,
                        message: '知识库及其全部文档将一并删除，操作不可恢复。',
                      });
                      if (ok) removeKB(active.id);
                    }}
                  >
                    <Icon name="trash" size={14} />
                    删除
                  </button>
                </div>
              </header>
              <div className="flex-1 overflow-y-auto p-6">
                {/* 拖拽上传区 */}
                <div
                  className={
                    'mb-5 flex min-h-32 cursor-pointer flex-col items-center justify-center rounded-xl border border-dashed p-6 text-center transition-colors ' +
                    (dragging
                      ? 'border-primary bg-primary/5'
                      : 'border-border bg-muted/30 hover:border-primary/40 hover:bg-muted/50')
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
                  <div
                    className={
                      'mb-2 grid h-10 w-10 place-items-center rounded-full transition-colors ' +
                      (dragging ? 'bg-primary/15 text-primary' : 'bg-muted text-muted-foreground')
                    }
                  >
                    <Icon name="upload" size={18} />
                  </div>
                  <div className="text-sm font-medium text-foreground">上传 txt / md / PDF</div>
                  <div className="mt-1 text-xs text-muted-foreground">拖拽文件到这里，或点击选择</div>
                  <input
                    ref={fileRef}
                    type="file"
                    multiple
                    accept=".txt,.md,.pdf,text/plain,text/markdown,application/pdf"
                    className="hidden"
                    onChange={(e) => e.target.files && uploadFiles(e.target.files)}
                  />
                </div>

                {/* 文档表 */}
                <div className="overflow-hidden rounded-xl border border-border bg-card">
                  <div className="grid grid-cols-[1fr_96px_72px_132px] border-b border-border bg-muted/40 px-3 py-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                    <span>文件</span>
                    <span>状态</span>
                    <span className="text-center">切片</span>
                    <span className="text-right">操作</span>
                  </div>
                  {docs.length === 0 ? (
                    <div className="px-3 py-10 text-center text-sm text-muted-foreground">
                      暂无文档
                    </div>
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
                        onPause={() => pauseDoc(active.id, doc.id)}
                        onResume={() => resumeDoc(active.id, doc.id)}
                        onDelete={async () => {
                          const ok = await confirm({
                            title: `删除文档「${doc.filename}」？`,
                            message: '文档及其切片将一并删除，操作不可恢复。',
                          });
                          if (ok) deleteDoc(active.id, doc.id);
                        }}
                      />
                    ))
                  )}
                </div>
              </div>
            </>
          ) : (
            <div className="flex flex-1 flex-col items-center justify-center px-6 text-center text-muted-foreground">
              <div className="mb-3 grid h-14 w-14 place-items-center rounded-2xl bg-muted text-muted-foreground">
                <Icon name="database" size={26} />
              </div>
              <div className="text-sm font-medium text-foreground">创建一个知识库</div>
              <p className="mt-1 max-w-xs text-xs">点击左上角「新建」开始管理你的文档。</p>
            </div>
          )}
        </section>

        {/* 右：配置 / 预览 / 召回 */}
        <aside className="w-[380px] shrink-0 overflow-y-auto border-l border-border bg-muted/30 p-5">
          {active && (
            <div className="space-y-6">
              <section>
                <div className="mb-3 flex items-center justify-between">
                  <h3 className="text-sm font-semibold text-foreground">配置</h3>
                  <button
                    className="btn-primary !px-3 !py-1.5 text-xs disabled:opacity-40"
                    disabled={!dirty || !name.trim()}
                    onClick={save}
                  >
                    保存
                  </button>
                </div>
                <div className="space-y-2.5">
                  <input className="field" value={name} onChange={(e) => setName(e.target.value)} />
                  <textarea
                    className="field min-h-20 resize-none"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="描述"
                  />
                  <label className="block">
                    <span className="mb-1 block text-xs font-medium text-muted-foreground">
                      Embedding 模型
                    </span>
                    <select
                      className="field"
                      value={providerId}
                      onChange={(e) => setProviderId(e.target.value)}
                    >
                      {embedProviders.length === 0 ? (
                        <option value="">暂无 Embed 模型，请先在设置添加</option>
                      ) : (
                        <option value="">未选择</option>
                      )}
                      {embedProviders.map((p) => (
                        <option key={p.id} value={p.id}>
                          {vendorLabel(p.base_url)} · {p.embed_model || '未配置'}
                        </option>
                      ))}
                    </select>
                    <span className="mt-1 block text-[11px] leading-4 text-muted-foreground">
                      用于文档切片的向量检索
                    </span>
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-xs font-medium text-muted-foreground">
                      Chat / VL 模型
                    </span>
                    <select
                      className="field"
                      value={chatProviderId}
                      onChange={(e) => setChatProviderId(e.target.value)}
                    >
                      <option value="">未选择（可选）</option>
                      {chatProviders.map((p) => (
                        <option key={p.id} value={p.id}>
                          {vendorLabel(p.base_url)} · {p.chat_model || '未配置'}
                        </option>
                      ))}
                    </select>
                    <span className="mt-1 block text-[11px] leading-4 text-muted-foreground">
                      用于图片描述等多模态任务
                    </span>
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-xs font-medium text-muted-foreground">
                      Rerank 模型
                    </span>
                    <select
                      className="field"
                      value={rerankProviderId}
                      onChange={(e) => setRerankProviderId(e.target.value)}
                    >
                      <option value="">未选择（可选）</option>
                      {rerankProviders.map((p) => (
                        <option key={p.id} value={p.id}>
                          {vendorLabel(p.base_url)} · {p.chat_model || '未配置'}
                        </option>
                      ))}
                    </select>
                    <span className="mt-1 block text-[11px] leading-4 text-muted-foreground">
                      可选：对召回结果重排序，未选则走纯 RRF 融合
                    </span>
                  </label>
                  <label className="block">
                    <span className="mb-1 block text-xs font-medium text-muted-foreground">
                      索引模式
                    </span>
                    <select
                      className="field"
                      value={indexMode}
                      onChange={(e) => setIndexMode(e.target.value as 'chunk' | 'qa')}
                    >
                      <option value="chunk">分块（父子分块，适合通用文档）</option>
                      <option value="qa">问答对（LLM 转问答，适合 FAQ；需 Chat 模型）</option>
                    </select>
                    <span className="mt-1 block text-[11px] leading-4 text-muted-foreground">
                      qa 模式入库较慢（每段调 LLM 生成问答）
                    </span>
                  </label>
                  <div className="grid grid-cols-2 gap-2.5">
                    <NumberField label="切片大小" value={chunkSize} onChange={setChunkSize} />
                    <NumberField label="重叠长度" value={overlap} onChange={setOverlap} />
                  </div>
                </div>
                {dirty && docs.length > 0 && (
                  <div className="mt-3 flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 p-3 text-sm text-foreground">
                    <Icon name="alert-circle" size={15} className="mt-0.5 shrink-0 text-warning" />
                    <div>
                      配置变化后需要重建索引
                      <button
                        className="ml-2 font-medium text-primary hover:underline"
                        onClick={retryAll}
                      >
                        重建全部
                      </button>
                    </div>
                  </div>
                )}
              </section>

              <section>
                <h3 className="mb-3 text-sm font-semibold text-foreground">切片预览</h3>
                <textarea
                  className="field min-h-24 resize-none"
                  value={previewText}
                  onChange={(e) => setPreviewText(e.target.value)}
                  placeholder="粘贴文本以预览切片效果"
                />
                <button className="btn-outline mt-2 !py-1.5 text-xs" onClick={runPreview}>
                  预览
                </button>
                <div className="mt-3 space-y-2">
                  {preview.slice(0, 5).map((chunk) => (
                    <ChunkBlock key={chunk.ordinal} ordinal={chunk.ordinal} text={chunk.text} />
                  ))}
                </div>
              </section>

              <section>
                <h3 className="mb-3 text-sm font-semibold text-foreground">召回测试</h3>
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
                  <button className="btn-primary shrink-0 !px-3 text-xs" onClick={runRetrieve}>
                    测试
                  </button>
                </div>
                <div className="mt-3 space-y-2.5">
                  {retrieveHits.map((hit) => (
                    <div key={hit.chunk_id} className="rounded-lg border border-border bg-card p-3">
                      <div className="mb-2 flex items-center justify-between gap-2 text-xs">
                        <span className="truncate font-mono text-muted-foreground">
                          {hit.filename} · #{hit.ordinal}
                        </span>
                        <span className="shrink-0 font-medium tabular-nums text-primary">
                          {(hit.similarity * 100).toFixed(1)}%
                        </span>
                      </div>
                      <p className="max-h-28 overflow-y-auto whitespace-pre-wrap text-xs leading-5 text-foreground/80">
                        {hit.text}
                      </p>
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

function StatusSummary({ kb }: { kb: KnowledgeBase }) {
  const ready = kb.ready_count ?? 0;
  const processing = kb.processing_count ?? 0;
  const failed = kb.failed_count ?? 0;
  const duplicate = kb.duplicate_count ?? 0;
  return (
    <div className="mt-2 flex flex-wrap gap-1">
      <span className="status-pill bg-success/10 text-success">{ready} ready</span>
      <span className="status-pill bg-muted text-muted-foreground">{processing} processing</span>
      {duplicate > 0 && <span className="status-pill bg-warning/10 text-warning">{duplicate} 重复</span>}
      {failed > 0 && <span className="status-pill bg-destructive/10 text-destructive">{failed} failed</span>}
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
  onPause,
  onResume,
  onDelete,
}: {
  doc: Document;
  kb: KnowledgeBase;
  expanded: boolean;
  chunks: { ordinal: number; text: string }[];
  onToggle: () => void;
  onRetry: () => void;
  onPause: () => void;
  onResume: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="border-t border-border first:border-t-0">
      <div className="grid grid-cols-[1fr_96px_72px_132px] items-center px-3 py-3 text-sm">
        <button className="flex min-w-0 items-center gap-2 text-left" onClick={onToggle}>
          <Icon name="file-text" size={16} className="shrink-0 text-muted-foreground" />
          <div className="min-w-0">
            <div className="truncate font-medium text-foreground">{doc.filename}</div>
            <div className="text-xs text-muted-foreground">{formatSize(doc.file_size)}</div>
            {doc.error && <div className="mt-0.5 truncate text-xs text-destructive">{doc.error}</div>}
          </div>
        </button>
        <span>
          <StatusBadge status={doc.status} done={doc.chunk_done} total={doc.chunk_total} />
        </span>
        <span className="text-center text-xs tabular-nums text-muted-foreground">{doc.chunk_count}</span>
        <span className="flex items-center justify-end gap-1 text-xs">
          <button
            className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            title="查看切片"
            onClick={onToggle}
          >
            <Icon name={expanded ? 'chevron-down' : 'chevron-right'} size={15} />
          </button>
          <button
            className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
            title="重试"
            onClick={onRetry}
          >
            <Icon name="refresh-cw" size={14} />
          </button>
          {doc.status === 'processing' && (
            <button
              className="rounded p-1 text-muted-foreground hover:bg-warning/10 hover:text-warning"
              title="暂停"
              onClick={onPause}
            >
              <Icon name="pause" size={14} />
            </button>
          )}
          {doc.status === 'paused' && (
            <button
              className="rounded p-1 text-muted-foreground hover:bg-success/10 hover:text-success"
              title="继续"
              onClick={onResume}
            >
              <Icon name="play" size={14} />
            </button>
          )}
          <button
            className="rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
            title="删除"
            onClick={onDelete}
          >
            <Icon name="trash" size={14} />
          </button>
        </span>
      </div>
      {expanded && (
        <div className="border-t border-border bg-muted/30 px-3 py-3">
          <div className="mb-2 text-xs text-muted-foreground">{kb.name}</div>
          <div className="space-y-2">
            {chunks.length === 0 ? (
              <div className="text-xs text-muted-foreground">暂无切片</div>
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

function StatusBadge({ status, done, total }: { status: string; done?: number; total?: number }) {
  // 入库中且已知总数：显示进度条（done/total + 百分比条），断点续传时从断点继续涨
  if (status === 'processing' && total && total > 0) {
    const pct = Math.round(((done ?? 0) / total) * 100);
    return (
      <div className="flex w-[88px] min-w-0 flex-col items-center gap-1">
        <span className="status-pill bg-primary/10 text-primary tabular-nums">{done ?? 0}/{total}</span>
        <div className="h-1 w-full overflow-hidden rounded-full bg-muted">
          <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
        </div>
      </div>
    );
  }
  // 入库中但 total=0：还在解析/图片描述阶段（切分前未设 total），给个提示而非裸 processing
  if (status === 'processing') {
    return <span className="status-pill bg-primary/10 text-primary">解析中…</span>;
  }
  if (status === 'paused') {
    return <span className="status-pill bg-warning/10 text-warning">已暂停</span>;
  }
  const cls =
    status === 'ready'
      ? 'bg-success/10 text-success'
      : status === 'failed'
        ? 'bg-destructive/10 text-destructive'
        : status === 'duplicate'
          ? 'bg-warning/10 text-warning'
          : 'bg-muted text-muted-foreground';
  return <span className={'status-pill ' + cls}>{status === 'duplicate' ? '重复' : status}</span>;
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
      <span className="mb-1 block text-xs text-muted-foreground">{label}</span>
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
    <div className="rounded-lg border border-border bg-background p-2.5">
      <div className="mb-1 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
        chunk {ordinal}
      </div>
      <div className="max-h-28 overflow-y-auto whitespace-pre-wrap break-all font-mono text-xs leading-5 text-foreground/80">
        {text}
      </div>
    </div>
  );
}

function formatSize(size: number) {
  if (!size) return '0 B';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}
