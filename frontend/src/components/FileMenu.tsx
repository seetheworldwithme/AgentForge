// @ 文件菜单:输入 `@` 触发,多列无限级导航(类 macOS Finder 列视图)。
// 每列=上一列高亮文件夹的子项;列数随用户按 → 下钻动态增长。
// ↑↓ 移当前列 │ → 进下一列(任意层级) │ ← 退上一列 │ Enter 选当前列高亮项 │ Esc 关闭。
// 选中状态由父组件(ChatInput)持有(attachments),本组件仅展示与触发 onSelect。
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
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
  // columns[i] = 第 i 列的原始子项(列 0 = 工作目录顶层)。列数随下钻动态增长。
  const [columns, setColumns] = useState<TreeItem[][]>([]);
  // colIdx[i] = 第 i 列高亮 index(基于过滤后列表)。
  const [colIdx, setColIdx] = useState<number[]>([]);
  const [activeCol, setActiveCol] = useState(0);

  const q = query.trim().toLowerCase();
  // 每列按过滤词过滤后的列表;渲染与导航都基于此。
  const filtered = useMemo(
    () => columns.map((col) => col.filter((it) => !q || it.name.toLowerCase().includes(q))),
    [columns, q],
  );

  // 当前列表为空时 idxOf 返回 0,否则把高亮夹紧到合法区间,防越界。
  const idxOf = (i: number) => {
    const len = filtered[i]?.length ?? 0;
    return len === 0 ? 0 : Math.min(colIdx[i] ?? 0, len - 1);
  };
  const curItem = () => filtered[activeCol]?.[idxOf(activeCol)];

  // 加载第 col 列(前驱为 parentPath,空串=根),并截断其后所有列。
  // setColumns/setColIdx 均用函数式更新,避免并发加载互相覆盖。
  const loadColumn = (parentPath: string, col: number) => {
    api
      .listTree(parentPath)
      .then((r) => {
        setColumns((prev) => {
          const next = prev.slice(0, col);
          next[col] = r.items;
          return next;
        });
        setColIdx((prev) => {
          const next = prev.slice(0, col);
          next[col] = 0;
          return next;
        });
      })
      .catch(() => {});
  };

  // 列 0:工作目录变化时加载顶层。
  useEffect(() => {
    if (!workDir) {
      setColumns([]);
      setColIdx([]);
      return;
    }
    loadColumn('', 0);
    setActiveCol(0);
    // loadColumn 闭包稳定(只用 setState),无需纳入依赖。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workDir]);

  // 过滤词变化时重置每列高亮到首项,避免停留在已消失的项上。
  useEffect(() => {
    setColIdx((prev) => prev.map(() => 0));
  }, [q]);

  useImperativeHandle(
    ref,
    () => ({
      handleKey: (key: string) => {
        if (key === 'Escape') {
          onClose();
          return;
        }
        if (key === 'ArrowLeft') {
          if (activeCol > 0) setActiveCol(activeCol - 1);
          return;
        }
        if (key === 'ArrowRight') {
          const it = curItem();
          // 文件夹才可下钻:已展开过则切焦点,否则先加载再进入。
          if (it?.is_dir) {
            if (columns[activeCol + 1] === undefined) loadColumn(it.path, activeCol + 1);
            setActiveCol(activeCol + 1);
          }
          return;
        }
        if (key === 'ArrowUp' || key === 'ArrowDown') {
          const items = filtered[activeCol] ?? [];
          const n = items.length;
          if (n === 0) return;
          const cur = idxOf(activeCol);
          const ni = key === 'ArrowDown' ? (cur + 1) % n : (cur - 1 + n) % n;
          setColIdx((prev) => {
            const next = prev.slice(0, activeCol + 1);
            next[activeCol] = ni;
            return next;
          });
          // 已有下一列时,跟随新高亮项刷新(Finder 行为);高亮变文件则截断子列。
          if (columns.length > activeCol + 1) {
            const newItem = items[ni];
            if (newItem?.is_dir) loadColumn(newItem.path, activeCol + 1);
            else {
              setColumns((p) => p.slice(0, activeCol + 1));
              setColIdx((p) => p.slice(0, activeCol + 1));
            }
          }
          return;
        }
        if (key === 'Enter') {
          const it = curItem();
          if (it) onSelect(it.path, it.is_dir);
        }
      },
    }),
    [activeCol, columns, filtered, colIdx, onClose, onSelect],
  );

  const loading = columns.length === 0;

  return (
    <div className="absolute bottom-full left-0 right-0 z-50 mb-2 flex h-80 overflow-hidden rounded-xl border border-border bg-card shadow-lg">
      {!workDir ? (
        <div className="flex w-full items-center justify-center px-3 text-center text-xs text-muted-foreground">
          请先选择工作目录
        </div>
      ) : loading ? (
        <div className="flex w-full items-center justify-center px-3 text-center text-xs text-muted-foreground">
          加载中…
        </div>
      ) : (
        <div className="flex overflow-x-auto">
          {filtered.map((items, i) => {
            const parent = i === 0 ? null : filtered[i - 1]?.[idxOf(i - 1)] ?? null;
            return (
              <div
                key={i}
                className="flex h-full w-48 shrink-0 flex-col border-r border-border last:border-r-0"
              >
                <ColTitle>{parent ? parent.name + '/' : '工作目录'}</ColTitle>
                <div className="flex-1 overflow-y-auto">
                  {items.length === 0 ? (
                    <Empty>{i === 0 ? '没有匹配项' : '(空)'}</Empty>
                  ) : (
                    items.map((it, idx) => (
                      <Row
                        key={it.path}
                        active={i === activeCol && idx === idxOf(i)}
                        selected={attachments.includes(it.path)}
                        onHover={() => {
                          setColIdx((prev) => {
                            const next = prev.slice(0, i + 1);
                            next[i] = idx;
                            return next;
                          });
                          setActiveCol(i);
                        }}
                        onClick={() => onSelect(it.path, it.is_dir)}
                        item={it}
                      />
                    ))
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
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
  const ref = useRef<HTMLButtonElement>(null);
  // 高亮变化时把该项滚入可视区,解决「按 ↓ 到列表底部后无法继续往下看」的问题。
  useEffect(() => {
    if (active && ref.current) ref.current.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [active]);
  return (
    <button
      ref={ref}
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
