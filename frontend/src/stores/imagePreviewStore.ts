import { create } from 'zustand';

interface ImagePreviewState {
  images: string[]; // 当前预览的 dataURL 列表；空数组即「未打开」
  index: number; // 当前大图索引
  open: (images: string[], index?: number) => void;
  close: () => void;
  setIndex: (i: number) => void;
}

// 图片预览 Lightbox 的全局状态：任意缩略图点击时调用 open(images, index)，
// 根节点 App 挂载的单一 <ImageLightbox/> 订阅本 store 渲染。
// 复用项目既有「store + 根节点 Dialog」模式（见 confirmStore / askStore）。
export const useImagePreviewStore = create<ImagePreviewState>((set) => ({
  images: [],
  index: 0,
  // 写入 images 并夹取起始 index 到合法区间，与 setIndex 的防御一致。
  open: (images, index = 0) =>
    set({ images, index: images.length ? Math.max(0, Math.min(images.length - 1, index)) : 0 }),
  close: () => set({ images: [], index: 0 }),
  // 切换当前页并夹取到合法区间，调用方无需关心越界。
  setIndex: (i) =>
    set((st) => {
      if (st.images.length === 0) return {};
      return { index: Math.max(0, Math.min(st.images.length - 1, i)) };
    }),
}));
