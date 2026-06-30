import { useEffect, useState, type ReactNode } from 'react';
import { useConfigStore } from '../stores/configStore';
import { useConfirmModalStore } from '../stores/confirmModalStore';
import { api } from '../lib/api';
import { VENDORS, vendorLabel } from '../lib/vendors';
import { Icon } from './Icon';
import type { Provider } from '../types';

type Status =
  | { kind: 'idle' }
  | { kind: 'testing' }
  | { kind: 'error'; message: string }
  | { kind: 'success'; message: string };

const EMPTY = {
  name: '',
  base_url: 'https://api.openai.com/v1',
  api_key: '',
  chat_model: 'gpt-4o-mini',
  embed_model: 'text-embedding-3-small',
  is_default: true,
  vision: false,
  context_window: 200000,
};

type Form = typeof EMPTY;
type ModelCategory = 'chat' | 'embed' | 'rerank';

export function ProviderSettings() {
  const providers = useConfigStore((s) => s.providers);
  const loaded = useConfigStore((s) => s.loaded);
  const load = useConfigStore((s) => s.load);
  const create = useConfigStore((s) => s.create);
  const update = useConfigStore((s) => s.update);
  const remove = useConfigStore((s) => s.remove);
  const confirm = useConfirmModalStore((s) => s.confirm);

  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<Form>(EMPTY);
  const [status, setStatus] = useState<Status>({ kind: 'idle' });
  // 控制弹窗（添加/编辑）的显隐
  const [modalOpen, setModalOpen] = useState(false);
  // 厂商下拉
  const [vendorKey, setVendorKey] = useState<string>('openai');
  // 模型类别：chat / embed
  const [category, setCategory] = useState<ModelCategory>('chat');
  // API Key 输入框可见性：默认隐藏，点右侧眼睛切换显示。
  const [showApiKey, setShowApiKey] = useState(false);

  useEffect(() => {
    if (!loaded) load();
  }, [loaded, load]);

  const resetForm = () => {
    setEditingId(null);
    setForm(EMPTY);
    setStatus({ kind: 'idle' });
    setVendorKey('openai');
    setCategory('chat');
  };

  const openAdd = () => {
    resetForm();
    setModalOpen(true);
  };

  const closeModal = () => {
    setModalOpen(false);
    resetForm();
  };

  const startEdit = (p: Provider) => {
    setEditingId(p.id);
    setForm({
      name: p.name,
      base_url: p.base_url,
      api_key: p.api_key,
      chat_model: p.chat_model,
      embed_model: p.embed_model ?? '',
      is_default: p.is_default,
      vision: p.vision ?? false,
      // 后端存 token，表单用 k：加载时换算为 k（四舍五入，0 保持 0）。
      context_window: p.context_window ? Math.round(p.context_window / 1000) : 0,
    });
    setStatus({ kind: 'idle' });
    // 尝试匹配已有厂商
    const matched = VENDORS.find((v) => v.base_url === p.base_url);
    setVendorKey(matched ? matched.key : 'custom');
    setCategory(p.kind === 'embed' ? 'embed' : p.kind === 'rerank' ? 'rerank' : 'chat');
    setModalOpen(true);
  };

  const setField = <K extends keyof Form>(key: K, value: Form[K]) => {
    setForm((f) => ({ ...f, [key]: value }));
    setStatus({ kind: 'idle' });
  };

  // 选择厂商时自动填充
  const onSelectVendor = (key: string) => {
    setVendorKey(key);
    const v = VENDORS.find((x) => x.key === key);
    if (!v) return;
    setForm((f) => ({
      ...f,
      // 名称不再手填：选具体厂商时用厂商名；自定义时保留原值（save 时再用模型名兜底）。
      name: v.key === 'custom' ? f.name : v.label,
      base_url: v.base_url || f.base_url,
      chat_model: v.chat_model || f.chat_model,
      embed_model: v.embed_model || f.embed_model,
      context_window: v.context_window,
    }));
    setStatus({ kind: 'idle' });
  };

  const save = async () => {
    if (status.kind === 'testing') return;
    if ((category === 'chat' || category === 'rerank') && !form.chat_model.trim()) {
      setStatus({ kind: 'error', message: '请填写模型名称' });
      return;
    }
    if (category === 'embed' && !form.embed_model.trim()) {
      setStatus({ kind: 'error', message: '请填写模型名称' });
      return;
    }
    setStatus({ kind: 'testing' });
    // 保存前先测试连通性
    try {
      const res = await api.testProvider({
        base_url: form.base_url,
        api_key: form.api_key,
        chat_model: form.chat_model,
        embed_model: form.embed_model,
        kind: category,
      });
      if (!res.ok) {
        setStatus({ kind: 'error', message: res.error ?? '连通性测试失败' });
        return;
      }
    } catch (e) {
      setStatus({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      return;
    }
    try {
      // 持久化类别（chat/embed），供对话下拉框按类别过滤；两个 model 字段都保留。
      // 名称不再手填：用厂商名，自定义时回退模型名；上下文表单是 k，存储换算回 token。
      // 自定义厂商：名称固定为「自定义」（列表里也不再展示模型名）；其它厂商用厂商名。
      const isCustomVendor = vendorKey === 'custom';
      const payload = {
        ...form,
        kind: category,
        name: isCustomVendor ? '自定义' : (form.name || form.chat_model || form.embed_model || '未命名').trim(),
        context_window: form.context_window * 1000,
      };
      if (editingId) {
        await update(editingId, payload);
        setStatus({ kind: 'success', message: '测试通过，已更新' });
      } else {
        await create(payload);
        setStatus({ kind: 'success', message: '测试通过，已保存' });
      }
      // 保存成功后关闭弹窗
      closeModal();
    } catch (e) {
      setStatus({ kind: 'error', message: '保存失败：' + (e instanceof Error ? e.message : String(e)) });
    }
  };

  const onDelete = async (id: string) => {
    const ok = await confirm({
      title: '删除该模型配置？',
      message: '操作不可恢复。',
    });
    if (!ok) return;
    if (id === editingId) resetForm();
    await remove(id);
  };

  return (
    <div className="space-y-5">
      <div>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-base font-semibold text-foreground">已配置的模型</h2>
          <button className="btn-primary !py-1.5 text-xs" onClick={openAdd}>
            <Icon name="plus" size={15} strokeWidth={2.25} />
            添加
          </button>
        </div>
        {providers.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border bg-muted/30 px-4 py-10 text-center text-sm text-muted-foreground">
            还没有模型，点击右上角「添加」配置一个。
          </div>
        ) : (
          <div className="space-y-2">
            {providers.map((p) => (
              <div
                key={p.id}
                className="flex items-center gap-3 rounded-xl border border-border bg-card px-3 py-2.5 transition-colors hover:bg-muted/40"
              >
                <div className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
                  <Icon name={p.kind === 'embed' ? 'database' : 'bot'} size={18} />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium text-foreground">{vendorLabel(p.base_url)}</span>
                    <KindBadge kind={p.kind} />
                    {p.vision && (
                      <span
                        className="status-pill bg-primary/10 text-primary"
                        title="视觉模型，支持粘贴图片"
                      >
                        <Icon name="image" size={11} className="mr-0.5" />
                        视觉
                      </span>
                    )}
                    {p.is_default && (
                      <span className="status-pill bg-primary/10 text-primary">默认</span>
                    )}
                  </div>
                  <div className="truncate font-mono text-xs text-muted-foreground">
                    {p.chat_model} · {p.base_url}
                  </div>
                </div>
                <button
                  className="btn-outline gap-1.5 !px-2 !py-1.5 text-xs"
                  onClick={() => startEdit(p)}
                >
                  <Icon name="pencil" size={13} />
                  编辑
                </button>
                <button
                  className="btn-danger gap-1.5 !px-2 !py-1.5 text-xs"
                  onClick={() => onDelete(p.id)}
                >
                  <Icon name="trash" size={13} />
                  删除
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* 添加 / 编辑 弹窗 */}
      {modalOpen && (
        <div
          className="fixed inset-0 z-[60] flex animate-fade-in items-center justify-center bg-black/40 backdrop-blur-sm"
          onMouseDown={(e) => {
            // 记录鼠标按下时的目标：只有在遮罩本身按下、并在遮罩本身抬起时，
            // 才算"点击遮罩关闭"。在输入框内拖选文字超出弹窗边界导致的
            // mouseup 落到遮罩上，因为按下点不在遮罩，会被忽略，避免误关弹窗。
            if (e.target === e.currentTarget) e.currentTarget.dataset.down = '1';
            else delete e.currentTarget.dataset.down;
          }}
          onClick={(e) => {
            if (e.target === e.currentTarget && e.currentTarget.dataset.down === '1') {
              delete e.currentTarget.dataset.down;
              closeModal();
            }
          }}
        >
          <div
            className="flex w-[520px] max-w-[92vw] animate-scale-in flex-col overflow-hidden rounded-2xl border border-border bg-card shadow-lg"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b border-border px-5 py-3.5">
              <h3 className="font-semibold text-foreground">
                {editingId ? '编辑模型' : '添加模型'}
              </h3>
              <button
                className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                onClick={closeModal}
                aria-label="关闭"
              >
                <Icon name="x" size={18} />
              </button>
            </div>

            <div className="space-y-3 p-5">
              {/* 厂商下拉 */}
              <div>
                <label className="mb-1 block text-xs font-medium text-muted-foreground">
                  模型厂商
                </label>
                <select
                  className="field"
                  value={vendorKey}
                  onChange={(e) => onSelectVendor(e.target.value)}
                >
                  {VENDORS.map((v) => (
                    <option key={v.key} value={v.key}>
                      {v.label}
                    </option>
                  ))}
                </select>
              </div>

              {/* 模型类别：chat / embed */}
              <div>
                <label className="mb-1 block text-xs font-medium text-muted-foreground">
                  模型类别
                </label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    className={
                      'flex-1 rounded-md border py-1.5 text-sm transition-colors ' +
                      (category === 'chat'
                        ? 'border-primary/40 bg-primary/10 font-medium text-primary'
                        : 'border-border text-muted-foreground hover:bg-muted')
                    }
                    onClick={() => setCategory('chat')}
                  >
                    Chat（对话）
                  </button>
                  <button
                    type="button"
                    className={
                      'flex-1 rounded-md border py-1.5 text-sm transition-colors ' +
                      (category === 'embed'
                        ? 'border-primary/40 bg-primary/10 font-medium text-primary'
                        : 'border-border text-muted-foreground hover:bg-muted')
                    }
                    onClick={() => setCategory('embed')}
                  >
                    Embed（向量）
                  </button>
                  <button
                    type="button"
                    className={
                      'flex-1 rounded-md border py-1.5 text-sm transition-colors ' +
                      (category === 'rerank'
                        ? 'border-primary/40 bg-primary/10 font-medium text-primary'
                        : 'border-border text-muted-foreground hover:bg-muted')
                    }
                    onClick={() => setCategory('rerank')}
                  >
                    Rerank（重排）
                  </button>
                </div>
              </div>

              {/* 请求路径 / API Key / 模型名称 / 上下文长度：左侧中文标签 + 右侧输入，无占位提示 */}
              <div className="space-y-2.5">
                <FieldRow label="请求路径">
                  <input
                    className="field flex-1"
                    value={form.base_url}
                    onChange={(e) => setField('base_url', e.target.value)}
                  />
                </FieldRow>
                <FieldRow label="API Key">
                  <div className="relative flex-1">
                    <input
                      className="field w-full pr-9"
                      type={showApiKey ? 'text' : 'password'}
                      value={form.api_key}
                      onChange={(e) => setField('api_key', e.target.value)}
                    />
                    <button
                      type="button"
                      onClick={() => setShowApiKey((v) => !v)}
                      className="absolute right-2 top-1/2 grid -translate-y-1/2 place-items-center text-muted-foreground transition-colors hover:text-foreground"
                      aria-label={showApiKey ? '隐藏 API Key' : '显示 API Key'}
                      title={showApiKey ? '隐藏' : '显示'}
                    >
                      <Icon name={showApiKey ? 'eye-off' : 'eye'} size={15} />
                    </button>
                  </div>
                </FieldRow>
                <FieldRow label="模型名称">
                  <input
                    className="field flex-1"
                    value={category === 'embed' ? form.embed_model : form.chat_model}
                    onChange={(e) =>
                      setField(
                        category === 'embed' ? 'embed_model' : 'chat_model',
                        e.target.value,
                      )
                    }
                  />
                </FieldRow>
                {category === 'chat' && (
                  <FieldRow label="上下文长度">
                    <div className="flex flex-1 items-center gap-1.5">
                      <input
                        type="number"
                        min={0}
                        className="field flex-1"
                        value={form.context_window}
                        onChange={(e) =>
                          setField(
                            'context_window',
                            e.target.value === '' ? 0 : Number(e.target.value),
                          )
                        }
                      />
                      <span className="shrink-0 text-xs text-muted-foreground">k</span>
                    </div>
                  </FieldRow>
                )}
              </div>

              <label className="flex cursor-pointer items-center gap-2 text-sm text-foreground">
                <input
                  type="checkbox"
                  className="h-4 w-4 rounded border-input accent-primary"
                  checked={form.is_default}
                  onChange={(e) => setField('is_default', e.target.checked)}
                />
                设为默认
              </label>

              {category === 'chat' && (
                <label className="flex cursor-pointer items-center gap-2 text-sm text-foreground">
                  <input
                    type="checkbox"
                    className="h-4 w-4 rounded border-input accent-primary"
                    checked={form.vision}
                    onChange={(e) => setField('vision', e.target.checked)}
                  />
                  支持图片 / 视觉（允许在对话框粘贴图片）
                </label>
              )}

              {status.kind === 'error' && (
                <div className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/10 p-2.5 text-sm text-destructive">
                  <Icon name="alert-circle" size={15} className="mt-0.5 shrink-0" />
                  <span className="whitespace-pre-wrap break-all">{status.message}</span>
                </div>
              )}
              {status.kind === 'success' && (
                <div className="flex items-start gap-2 rounded-lg border border-success/30 bg-success/10 p-2.5 text-sm text-success">
                  <Icon name="check" size={15} className="mt-0.5 shrink-0" strokeWidth={2.5} />
                  <span className="whitespace-pre-wrap break-all">{status.message}</span>
                </div>
              )}
            </div>

            <div className="flex justify-end gap-2 border-t border-border px-5 py-3.5">
              <button className="btn-outline text-sm" onClick={closeModal}>
                取消
              </button>
              <button
                className="btn-primary text-sm"
                onClick={save}
                disabled={status.kind === 'testing'}
              >
                {status.kind === 'testing' ? (
                  <>
                    <Icon name="loader" size={15} className="animate-spin" />
                    测试中…
                  </>
                ) : editingId ? (
                  '测试并更新'
                ) : (
                  '测试并保存'
                )}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// provider 类别徽章：一眼区分对话 / 向量 / 重排模型，便于管理；无 kind 视为对话
function KindBadge({ kind }: { kind?: 'chat' | 'embed' | 'rerank' }) {
  if (kind === 'embed') {
    return <span className="status-pill bg-accent text-accent-foreground">向量</span>;
  }
  if (kind === 'rerank') {
    return <span className="status-pill bg-primary/15 text-primary">重排</span>;
  }
  return <span className="status-pill bg-muted text-muted-foreground">对话</span>;
}

// FieldRow：左侧固定宽度中文标签 + 右侧输入控件，用于模型表单字段排版。
function FieldRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center gap-3">
      <label className="w-24 shrink-0 text-xs font-medium text-muted-foreground">{label}</label>
      <div className="flex min-w-0 flex-1 items-center">{children}</div>
    </div>
  );
}
