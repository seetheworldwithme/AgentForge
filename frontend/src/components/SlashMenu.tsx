// 斜杠菜单：输入 `/` 触发，按「计划模式 → Skills」分组展示。
// 支持模糊过滤与键盘导航（↑↓ 移动、Enter 切换、Esc 关闭）。勾选状态由父组件
// （ChatInput）持有并下传，本组件仅负责展示与触发 toggle，不持久化任何状态。
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { Icon, type IconName } from './Icon';
import type { Skill } from '../types';

// 父组件通过 ref 转发 textarea 的导航键到菜单。
export type SlashMenuHandle = { handleKey: (key: string) => void };

interface Props {
  query: string; // `/` 之后的过滤词
  planMode: boolean;
  skillIDs: string[];
  skills: Skill[] | null; // 由父组件加载并下发，null 表示尚未加载
  onTogglePlan: () => void;
  onToggleSkill: (id: string) => void;
  onClose: () => void;
}

export const SlashMenu = forwardRef<SlashMenuHandle, Props>(function SlashMenu(
  { query, planMode, skillIDs, skills, onTogglePlan, onToggleSkill, onClose },
  ref,
) {
  const [highlight, setHighlight] = useState(0);

  const q = query.trim().toLowerCase();
  const visibleSkills = useMemo(
    () =>
      // 斜杠菜单只展示「当前工作目录」的 skills，不含全局——全局 skills 由设置页统一
      // 启用后注入，菜单勾选用于临时限定本次使用的本地 skill。
      (skills ?? []).filter((s) => s.source !== 'global' && (!q || s.name.toLowerCase().includes(q))),
    [skills, q],
  );

  // 候选项顺序固定：plan(1) + skills。索引即高亮下标。
  const skillBase = 1;
  const total = skillBase + visibleSkills.length;

  // 过滤词变化时重置高亮到计划行，避免越界停留在已消失的项上。
  useEffect(() => {
    setHighlight(0);
  }, [query]);

  const triggerAt = (idx: number) => {
    if (idx === 0) onTogglePlan();
    else onToggleSkill(visibleSkills[idx - skillBase].id);
  };

  // 暴露给父组件：textarea 的导航键转发到这里。依赖变化时重建，避免闭包持有旧值。
  useImperativeHandle(
    ref,
    () => ({
      handleKey: (key: string) => {
        if (key === 'Escape') {
          onClose();
          return;
        }
        if (total === 0) return;
        if (key === 'ArrowDown') setHighlight((h) => (h + 1) % total);
        else if (key === 'ArrowUp') setHighlight((h) => (h - 1 + total) % total);
        else if (key === 'Enter') triggerAt(Math.min(highlight, total - 1));
      },
    }),
    [total, highlight, visibleSkills, onClose, onTogglePlan, onToggleSkill],
  );

  const loading = skills === null;

  return (
    <div className="absolute bottom-full left-0 right-0 z-50 mb-2 max-h-80 overflow-auto rounded-xl border border-border bg-card shadow-lg">
      {/* 计划模式（始终置顶，不参与过滤） */}
      <SectionTitle icon="file-text">计划模式</SectionTitle>
      <Row
        active={highlight === 0}
        selected={planMode}
        onClick={onTogglePlan}
        onHover={() => setHighlight(0)}
        icon="file-text"
        label="计划模式"
        desc="只读调研后产出结构化计划"
      />

      {/* Skills */}
      <SectionTitle icon="sparkles">Skills{skills && `（${visibleSkills.length}/${skills.length}）`}</SectionTitle>
      {visibleSkills.map((s, i) => (
        <Row
          key={s.id}
          active={highlight === skillBase + i}
          selected={skillIDs.includes(s.id)}
          onClick={() => onToggleSkill(s.id)}
          onHover={() => setHighlight(skillBase + i)}
          icon="sparkles"
          label={s.name}
          desc={s.description}
        />
      ))}

      {loading && (
        <div className="px-3 py-3 text-center text-xs text-muted-foreground">加载中…</div>
      )}
      {!loading && visibleSkills.length === 0 && (
        <div className="px-3 py-3 text-center text-xs text-muted-foreground">没有匹配的 Skill</div>
      )}
    </div>
  );
});

// 分组标题：图标 + 文字，小号大写。
function SectionTitle({ icon, children }: { icon: IconName; children: ReactNode }) {
  return (
    <div className="flex items-center gap-1.5 px-3 pb-1 pt-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
      <Icon name={icon} size={11} />
      {children}
    </div>
  );
}

// 单行候选项：高亮/选中态、点击切换、鼠标悬停同步高亮。
function Row({
  active,
  selected,
  onClick,
  onHover,
  icon,
  label,
  desc,
}: {
  active: boolean;
  selected: boolean;
  onClick: () => void;
  onHover: () => void;
  icon: IconName;
  label: string;
  desc?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      onMouseEnter={onHover}
      className={
        'flex w-full items-center gap-2 px-3 py-2 text-left transition-colors ' +
        (active ? 'bg-accent text-accent-foreground' : 'text-foreground hover:bg-accent/60')
      }
    >
      <Icon name={icon} size={14} className="shrink-0 text-muted-foreground" />
      <span className="flex-1 truncate text-sm">{label}</span>
      {desc && <span className="max-w-[45%] shrink truncate text-xs text-muted-foreground">{desc}</span>}
      {selected && <Icon name="check" size={14} className="shrink-0 text-primary" strokeWidth={2.5} />}
    </button>
  );
}
