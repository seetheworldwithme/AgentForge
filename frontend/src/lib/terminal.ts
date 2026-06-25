// 内置终端的 WebSocket + xterm.js 封装。每个终端 = 一个 Terminal 实例 + 一个 WS 连接，
// 后端对应一个 PTY shell。实例由 terminalStore 持有，组件负责 open 到 DOM 并接线。
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { getCorePort } from './port';
import { useThemeStore } from '../stores/themeStore';

export interface TerminalTab {
  id: string;
  title: string;
  term: Terminal;
  fit: FitAddon;
  ws: WebSocket;
  attached: boolean; // 是否已 open 到 DOM（防重入）
  seq?: number; // 创建序号，用于编号（终端 1/2/3…），基于已有 tabs 递增
}

// 终端 WebSocket 地址：生产（Wails）直连 127.0.0.1:port；dev 走 vite 同源代理（proxy 已开 ws:true）。
export async function terminalWsUrl(): Promise<string> {
  const port = await getCorePort();
  if (!port) return `ws://${location.host}/api/terminal/ws`;
  return `ws://127.0.0.1:${port}/api/terminal/ws`;
}

// xterm 主题跟随项目亮/暗：与设计系统 card/foreground 色调对齐。
function xtermTheme() {
  return useThemeStore.getState().resolved === 'dark'
    ? { background: '#0b0f1a', foreground: '#e6e9f0', cursor: '#e6e9f0' }
    : { background: '#ffffff', foreground: '#1f2937', cursor: '#1f2937' };
}

// 创建一个终端实例（Terminal + FitAddon + WebSocket），ws 就绪后返回。此时尚未 open 到 DOM。
export async function createTerminalTab(id: string, title: string): Promise<TerminalTab> {
  const term = new Terminal({
    cursorBlink: true,
    fontFamily: 'Menlo, Monaco, Consolas, "Courier New", monospace',
    fontSize: 13,
    scrollback: 5000,
    theme: xtermTheme(),
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  const ws = new WebSocket(await terminalWsUrl());
  await new Promise<void>((resolve, reject) => {
    ws.onopen = () => resolve();
    ws.onerror = () => reject(new Error('终端连接失败'));
  });
  return { id, title, term, fit, ws, attached: false };
}

// 把终端挂到容器 DOM：open + 首帧 fit + 接线输入/输出/resize。
// 返回 cleanup：仅摘除监听与 ResizeObserver，不 dispose term（销毁由 disposeTab 负责，避免重复 dispose）。
export function attachToDom(tab: TerminalTab, container: HTMLElement): () => void {
  tab.term.open(container);
  tab.attached = true;

  // 首次 fit 必须在 open 后下一帧（xterm 需容器已测量尺寸），并发初始 resize。
  requestAnimationFrame(() => {
    safeFit(tab);
    sendResize(tab);
  });

  // 输入：xterm → WS
  tab.term.onData((d) => {
    if (tab.ws.readyState === WebSocket.OPEN) {
      tab.ws.send(JSON.stringify({ type: 'input', data: d }));
    }
  });

  // 输出：WS → xterm
  const onMessage = (e: MessageEvent) => {
    try {
      const msg = JSON.parse(typeof e.data === 'string' ? e.data : '');
      if (msg.type === 'output') tab.term.write(msg.data);
    } catch {
      /* 忽略非 JSON 帧 */
    }
  };
  tab.ws.addEventListener('message', onMessage);

  // 尺寸变化：debounce fit + 发 resize
  let roTimer: number | undefined;
  const ro = new ResizeObserver(() => {
    window.clearTimeout(roTimer);
    roTimer = window.setTimeout(() => {
      safeFit(tab);
      sendResize(tab);
    }, 100);
  });
  ro.observe(container);

  return () => {
    tab.ws.removeEventListener('message', onMessage);
    window.clearTimeout(roTimer);
    ro.disconnect();
  };
}

export function safeFit(tab: TerminalTab) {
  try {
    tab.fit.fit();
  } catch {
    /* 容器无尺寸时 fit 抛错，忽略 */
  }
}

// 把当前 fit 出的尺寸发给后端，让 PTY 跟随。
export function sendResize(tab: TerminalTab) {
  if (tab.ws.readyState !== WebSocket.OPEN) return;
  const dims = tab.fit.proposeDimensions();
  if (dims && dims.cols > 0 && dims.rows > 0) {
    tab.ws.send(JSON.stringify({ type: 'resize', cols: dims.cols, rows: dims.rows }));
  }
}

export function disposeTab(tab: TerminalTab) {
  try {
    tab.ws.close();
  } catch {
    /* 忽略 */
  }
  try {
    tab.term.dispose();
  } catch {
    /* 忽略 */
  }
}
