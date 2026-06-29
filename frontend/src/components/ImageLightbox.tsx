import { useEffect, useRef } from 'react';
import { useImagePreviewStore } from '../stores/imagePreviewStore';
import { Icon } from './Icon';

// 全局图片预览 Lightbox：订阅 imagePreviewStore，images 非空时全屏展示。
// Esc 关闭、←/→ 翻页、点遮罩（起止都在遮罩本身）关闭、点图片/按钮不关闭；
// 支持下载与多图翻页。在 App 根节点挂载一份即可，所有缩略图通过 store.open 触发。
export function ImageLightbox() {
  const images = useImagePreviewStore((s) => s.images);
  const index = useImagePreviewStore((s) => s.index);
  const close = useImagePreviewStore((s) => s.close);
  const setIndex = useImagePreviewStore((s) => s.setIndex);
  const isOpen = images.length > 0;
  const multi = images.length > 1;

  // 打开时锁背景滚动，关闭恢复，避免滚轮穿透到下层消息流。
  useEffect(() => {
    if (!isOpen) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = prev;
    };
  }, [isOpen]);

  // 键盘：Esc 关闭、←/→ 翻页。依赖仅 isOpen，监听只绑一次，用 getState 取最新 index，
  // 避免每次翻页重绑监听（与 confirmStore 的 get 风格一致）。
  useEffect(() => {
    if (!isOpen) return;
    const onKey = (e: KeyboardEvent) => {
      const st = useImagePreviewStore.getState();
      if (e.key === 'Escape') st.close();
      else if (e.key === 'ArrowLeft') st.setIndex(st.index - 1);
      else if (e.key === 'ArrowRight') st.setIndex(st.index + 1);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isOpen]);

  // 点击遮罩关闭的判定：仅当 mousedown 与 click 落点都是遮罩本身时才关闭，
  // 规避「从大图拖到遮罩松手」「点到工具栏/翻页按钮」等误关闭。
  const downTarget = useRef<EventTarget | null>(null);

  if (!isOpen) return null;

  const src = images[index];
  const ext = dataUrlExt(src);

  return (
    <div
      className="fixed inset-0 z-[100] flex animate-fade-in items-center justify-center bg-black/90 p-4"
      onMouseDown={(e) => {
        downTarget.current = e.target;
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget && downTarget.current === e.currentTarget) close();
      }}
    >
      {/* 顶部工具栏：左下载、右关闭（点击不关闭遮罩，由根容器的 target 判定兜底） */}
      <div className="absolute inset-x-0 top-0 flex items-center justify-between p-3">
        <button
          type="button"
          onClick={() => downloadImage(src, `image-${index + 1}.${ext}`)}
          className="grid h-9 w-9 place-items-center rounded-lg bg-white/10 text-white/90 transition-colors hover:bg-white/20 hover:text-white"
          title="下载"
          aria-label="下载"
        >
          <Icon name="download" size={18} />
        </button>
        <button
          type="button"
          onClick={close}
          className="grid h-9 w-9 place-items-center rounded-lg bg-white/10 text-white/90 transition-colors hover:bg-white/20 hover:text-white"
          title="关闭 (Esc)"
          aria-label="关闭 (Esc)"
        >
          <Icon name="x" size={20} />
        </button>
      </div>

      {/* 左翻页（仅多图；首张禁用） */}
      {multi && (
        <button
          type="button"
          onClick={() => setIndex(index - 1)}
          disabled={index === 0}
          className="absolute left-3 grid h-11 w-11 place-items-center rounded-full bg-white/10 text-white/90 transition-colors hover:bg-white/20 hover:text-white disabled:cursor-not-allowed disabled:opacity-30"
          aria-label="上一张"
          title="上一张 (←)"
        >
          <Icon name="chevron-right" size={22} className="rotate-180" />
        </button>
      )}

      {/* 大图 */}
      <img
        src={src}
        alt={`图片 ${index + 1}`}
        className="max-h-[88vh] max-w-[90vw] animate-scale-in rounded-lg object-contain shadow-2xl"
      />

      {/* 右翻页（仅多图；末张禁用） */}
      {multi && (
        <button
          type="button"
          onClick={() => setIndex(index + 1)}
          disabled={index === images.length - 1}
          className="absolute right-3 grid h-11 w-11 place-items-center rounded-full bg-white/10 text-white/90 transition-colors hover:bg-white/20 hover:text-white disabled:cursor-not-allowed disabled:opacity-30"
          aria-label="下一张"
          title="下一张 (→)"
        >
          <Icon name="chevron-right" size={22} />
        </button>
      )}

      {/* 底部页码（仅多图） */}
      {multi && (
        <div className="pointer-events-none absolute inset-x-0 bottom-0 flex justify-center p-3">
          <span className="rounded-full bg-black/50 px-3 py-1 text-xs font-medium text-white/80 tabular-nums">
            {index + 1} / {images.length}
          </span>
        </div>
      )}
    </div>
  );
}

// 从 dataURL 下载图片：优先转 Blob + createObjectURL（各 webview 行为一致，Wails 下更可靠），
// 失败回退直接 dataURL 下载。
function downloadImage(dataUrl: string, filename: string) {
  const fallback = () => {
    const a = document.createElement('a');
    a.href = dataUrl;
    a.download = filename;
    a.click();
  };
  fetch(dataUrl)
    .then((r) => r.blob())
    .then((blob) => {
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
    })
    .catch(fallback);
}

// 从 dataURL 前缀提取文件后缀，用于下载文件名；非 dataURL 或未知类型回退 png。
function dataUrlExt(src: string): string {
  const m = src.match(/^data:image\/([a-z+]+);/i);
  if (!m) return 'png';
  const t = m[1].toLowerCase();
  if (t === 'jpeg' || t === 'jpg') return 'jpg';
  if (t === 'png') return 'png';
  if (t === 'webp') return 'webp';
  if (t === 'gif') return 'gif';
  return 'png';
}
