// @ 文件菜单:输入 `@` 触发,双列两级导航(类 macOS Finder 列视图,限定两级)。
// 左列=工作目录顶层,右列=左列高亮文件夹的直接子项。
// ↑↓ 移当前列 │ → 进右列 │ ← 退左列 │ Enter 选当前列高亮项 │ Esc 关闭。
// 选中状态由父组件(ChatInput)持有(attachments),本组件仅展示与触发 onSelect。
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { Icon } from './Icon';
import { api } from '../lib/api';
import type { TreeItem } from '../types';

// 父组件通过 ref 转发 textarea 的导航键到菜单(与 SlashMenu 同款)。
export type FileMenuHandle = { handleKey: (key: string) => void };

interface Props {
  query: string; // `@` 之后的过滤词
  attachments: string[]; // 已选路径(展示选中态)
  workDir: string;
  onSelect: (path: string, isDir: boolean) => void;
  onClose: () => void;
}

export const FileMenu = forwardRef<FileMenuHandle, Props>(function FileMenu(
  { query, attachments, workDir, onSelect, onClose },
  ref,
) {
  const [leftRaw, setLeftRaw] = useState<TreeItem[] | null>(null); // null=加载中
  const [rightRaw, setRightRaw] = useState<TreeItem[]>([]);
  const [leftIdx, setLeftIdx] = useState(0);
  const [rightIdx, setRightIdx] = useState(0);
  const [activeCol, setActiveCol] = useState<'left' | 'right'>('left');

  const q = query.trim().toLowerCase();

  // 加载左列(工作目录顶层)。workDir 变化时重新加载。
  useEffect(() => {
    if (!workDir) {
      setLeftRaw([]);
      return;
    }
    let alive = true;
    api.listTree()
      .then((r) => alive && setLeftRaw(r.items))
      .catch(() => alive && setLeftRaw([]));
    return () => {
      alive = false;
    };
  }, [workDir]);

  const filteredLeft = useMemo(
    () => (leftRaw ?? []).filter((it) => !q || it.name.toLowerCase().includes(q)),
    [leftRaw, q],
  );
  // 左列当前高亮项(夹紧防越界)。
  const currentLeft = filteredLeft[Math.min(leftIdx, Math.max(0, filteredLeft.length - 1))];

  // 加载右列:左列高亮是文件夹时拉取其子项,否则清空。
  useEffect(() => {
    if (!currentLeft?.is_dir) {
      setRightRaw([]);
      return;
    }
    let alive = true;
    api.listTree(currentLeft.path)
      .then((r) => alive && setRightRaw(r.items))
      .catch(() => alive && setRightRaw([]));
    return () => {
      alive = false;
    };
  }, [currentLeft?.path, currentLeft?.is_dir]);

  const filteredRight = useMemo(
    () => rightRaw.filter((it) => !q || it.name.toLowerCase().includes(q)),
    [rightRaw, q],
  );

  // 过滤词或左列高亮变化时重置右列高亮并回到左列,避免越界停留。
  useEffect(() => {
    setLeftIdx(0);
  }, [q, leftRaw]);
  useEffect(() => {
    setRightIdx(0);
    setActiveCol('left');
  }, [currentLeft?.path]);

  useImperativeHandle(
    ref,
    () => ({
      handleKey: (key: string) => {
        if (key === 'Escape') {
          onClose();
          return;
        }
        if (key === 'ArrowLeft') {
          setActiveCol('left');
          return;
        }
        if (key === 'ArrowRight') {
          if (activeCol === 'left' && currentLeft?.is_dir && filteredRight.length > 0) {
            setActiveCol('right');
          }
          return;
        }
        if (key === 'ArrowUp' || key === 'ArrowDown') {
          if (activeCol === 'left') {
            const n = filteredLeft.length;
            if (n === 0) return;
            setLeftIdx((i) => (key === 'ArrowDown' ? (i + 1) % n : (i - 1 + n) % n));
          } else {
            const n = filteredRight.length;
            if (n === 0) return;
            setRightIdx((i) => (key === 'ArrowDown' ? (i + 1) % n : (i - 1 + n) % n));
          }
          return;
        }
        if (key === 'Enter') {
          if (activeCol === 'left' && currentLeft) {
            onSelect(currentLeft.path, currentLeft.is_dir);
          } else if (activeCol === 'right') {
            const it = filteredRight[Math.min(rightIdx, Math.max(0, filteredRight.length - 1))];
            if (it) onSelect(it.path, it.is_dir);
          }
        }
      },
    }),
    [activeCol, currentLeft, filteredLeft, filteredRight, leftIdx, rightIdx, onClose, onSelect],
  );

  const loading = leftRaw === null;

  return (
    <div className="absolute bottom-full left-0 right-0 z-50 mb-2 flex max-h-80 overflow-hidden rounded-xl border border-border bg-card shadow-lg">
      {/* 左列:工作目录顶层 */}
      <div className="flex w-1/2 flex-col border-r border-border">
        <ColTitle>工作目录</ColTitle>
        <div className="flex-1 overflow-auto">
          {!workDir ? (
            <Empty>请先选择工作目录</Empty>
          ) : loading ? (
            <Empty>加载中…</Empty>
          ) : filteredLeft.length === 0 ? (
            <Empty>没有匹配项</Empty>
          ) : (
            filteredLeft.map((it, i) => (
              <Row
                key={it.path}
                active={activeCol === 'left' && i === leftIdx}
                selected={attachments.includes(it.path)}
                onHover={() => {
                  setLeftIdx(i);
                  setActiveCol('left');
                }}
                onClick={() => onSelect(it.path, it.is_dir)}
                item={it}
              />
            ))
          )}
        </div>
      </div>

      {/* 右列:左列高亮文件夹的直接子项 */}
      <div className="flex w-1/2 flex-col">
        <ColTitle>{currentLeft?.is_dir ? currentLeft.name + '/' : '—'}</ColTitle>
        <div className="flex-1 overflow-auto">
          {!currentLeft?.is_dir ? (
            <Empty>选中左侧文件夹查看内容</Empty>
          ) : filteredRight.length === 0 ? (
            <Empty>(空)</Empty>
          ) : (
            filteredRight.map((it, i) => (
              <Row
                key={it.path}
                active={activeCol === 'right' && i === rightIdx}
                selected={attachments.includes(it.path)}
                onHover={() => {
                  setRightIdx(i);
                  setActiveCol('right');
                }}
                onClick={() => onSelect(it.path, it.is_dir)}
                item={it}
              />
            ))
          )}
        </div>
      </div>
    </div>
  );
});

function ColTitle({ children }: { children: ReactNode }) {
  return (
    <div className="border-b border-border px-3 py-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
      {children}
    </div>
  );
}

function Empty({ children }: { children: ReactNode }) {
  return <div className="px-3 py-3 text-center text-xs text-muted-foreground">{children}</div>;
}

function Row({
  active,
  selected,
  onHover,
  onClick,
  item,
}: {
  active: boolean;
  selected: boolean;
  onHover: () => void;
  onClick: () => void;
  item: TreeItem;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      onMouseEnter={onHover}
      className={
        'flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors ' +
        (active ? 'bg-accent text-accent-foreground' : 'text-foreground hover:bg-accent/60')
      }
    >
      <Icon
        name={item.is_dir ? 'folder' : 'file-text'}
        size={14}
        className="shrink-0 text-muted-foreground"
      />
      <span className="flex-1 truncate text-sm">{item.name}</span>
      {/* 文件夹显示进入箭头;已选则用 check 覆盖 */}
      {selected ? (
        <Icon name="check" size={14} className="shrink-0 text-primary" strokeWidth={2.5} />
      ) : item.is_dir ? (
        <Icon name="chevron-right" size={12} className="shrink-0 text-muted-foreground" />
      ) : null}
    </button>
  );
}
