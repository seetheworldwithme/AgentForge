import { useTodoStore } from '../stores/todoStore';
import { Icon } from './Icon';

// TodoStrip：输入框正上方的紧凑待办条。
//
// 设计要点（遵循项目语义令牌 + Lucide 图标，零 emoji）：
// - 只展示未完成项（in_progress + pending），完成的即时移除——"完成一个去掉一个"；
// - 进行中排最前、loader 转圈（animate-spin）；待办用方形（square）；
// - 最多 4 项，超出显示 +N，避免输入框上方被长清单挤压；
// - 宽度与消息区一致（max-w-3xl 居中），视觉对齐。
export function TodoStrip() {
  const items = useTodoStore((s) => s.items);
  const inProg = items.filter((t) => t.status === 'in_progress');
  const pending = items.filter((t) => t.status === 'pending');
  const ordered = [...inProg, ...pending]; // 进行中优先
  if (ordered.length === 0) return null;

  const shown = ordered.slice(0, 4);
  const more = ordered.length - shown.length;

  return (
    <div className="mx-auto flex max-w-3xl flex-wrap items-center gap-1.5 px-4 pb-1 pt-1.5">
      {shown.map((t) => {
        const isProg = t.status === 'in_progress';
        return (
          <span
            key={t.id}
            className={
              'inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs transition-colors ' +
              (isProg
                ? 'border-primary/30 bg-primary/5 text-primary'
                : 'border-border bg-card text-muted-foreground')
            }
            title={t.subject}
          >
            <Icon
              name={isProg ? 'loader' : 'square'}
              size={12}
              className={isProg ? 'shrink-0 animate-spin' : 'shrink-0'}
            />
            <span className="max-w-[200px] truncate">{t.subject}</span>
          </span>
        );
      })}
      {more > 0 && <span className="px-1 text-xs text-muted-foreground">+{more}</span>}
    </div>
  );
}
