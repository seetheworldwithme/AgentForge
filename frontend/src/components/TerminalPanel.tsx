// 内置终端面板：顶部 tab 栏（切换/新建/关闭）+ 主体多容器 xterm 区。
// 每个 tab 一个绝对定位容器，active 显示、其余 hidden，切 tab 不丢历史也不断连接。
import '@xterm/xterm/css/xterm.css';
import { useEffect, useRef } from 'react';
import { useTerminalStore } from '../stores/terminalStore';
import { attachToDom, safeFit, sendResize, type TerminalTab } from '../lib/terminal';
import { Icon } from './Icon';

export function TerminalPanel({ height }: { height: number }) {
  const tabs = useTerminalStore((s) => s.tabs);
  const activeId = useTerminalStore((s) => s.activeId);
  const createTab = useTerminalStore((s) => s.createTab);
  const closeTab = useTerminalStore((s) => s.closeTab);
  const setActive = useTerminalStore((s) => s.setActive);

  return (
    <div className="flex shrink-0 flex-col border-t border-border bg-card" style={{ height }}>
      {/* tab 栏：左侧各终端 tab + 右侧「+」新建 */}
      <div className="flex h-9 shrink-0 items-center gap-1 overflow-x-auto border-b border-border bg-muted/30 px-1.5">
        {tabs.map((t) => (
          <div
            key={t.id}
            onClick={() => setActive(t.id)}
            className={
              'group flex shrink-0 cursor-pointer items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors ' +
              (t.id === activeId
                ? 'bg-card text-foreground shadow-sm'
                : 'text-muted-foreground hover:bg-accent hover:text-foreground')
            }
          >
            <Icon name="terminal" size={12} className="shrink-0" />
            <span className="max-w-[120px] truncate">{t.title}</span>
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                closeTab(t.id);
              }}
              className="ml-0.5 grid h-4 w-4 place-items-center rounded text-muted-foreground transition-colors hover:bg-accent-foreground/10 hover:text-foreground"
              aria-label="关闭终端"
            >
              <Icon name="x" size={11} strokeWidth={2.4} />
            </button>
          </div>
        ))}
        <button
          type="button"
          onClick={() => createTab()}
          title="新建终端"
          className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        >
          <Icon name="plus" size={14} />
        </button>
      </div>
      {/* 容器堆：每 tab 一个绝对定位容器，非 active 用 hidden 保留 DOM/历史 */}
      <div className="relative min-h-0 flex-1">
        {tabs.map((t) => (
          <TermContainer key={t.id} tab={t} active={t.id === activeId} />
        ))}
      </div>
    </div>
  );
}

// 单个终端容器：首次挂载 open+fit+接线；切为 active 时重新 fit（hidden→visible 尺寸恢复）。
function TermContainer({ tab, active }: { tab: TerminalTab; active: boolean }) {
  const ref = useRef<HTMLDivElement>(null);

  // 首次挂载：open + fit + 接线（tab.attached 防重入，仅执行一次）
  useEffect(() => {
    if (!ref.current || tab.attached) return;
    return attachToDom(tab, ref.current);
  }, [tab]);

  // 切换为 active 时：下一帧重新 fit 并发 resize（容器从 hidden 恢复尺寸）
  useEffect(() => {
    if (!active || !tab.attached) return;
    const id = requestAnimationFrame(() => {
      safeFit(tab);
      sendResize(tab);
    });
    return () => cancelAnimationFrame(id);
  }, [active, tab]);

  return (
    <div
      ref={ref}
      className={
        active ? 'absolute inset-0 overflow-hidden p-1' : 'absolute inset-0 hidden overflow-hidden p-1'
      }
    />
  );
}
