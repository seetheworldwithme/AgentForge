// 把当前会话消息导出为 markdown 文件。
// 颗粒度可控：勾选思考过程和工具调用时包含对应内容，否则只导出 user + assistant 正文。
import type { Message } from '../types';

// tool 消息中"调用内容"与"执行结果"的分隔符。
// 与 sessionStore.ts（流式拼接）、MessageBubble.tsx（渲染拆分）、
// internal/server/handler_session.go（历史重放拼接）保持一致——改动需同步四处。
const TOOL_RESULT_SEPARATOR = '\n─────────\n';

export interface ExportOptions {
  includeThinking: boolean;
  includeTools: boolean;
}

// 导出当前会话为 markdown 文本
export function exportMessagesToMarkdown(
  messages: Message[],
  sessionTitle: string | undefined,
  options: ExportOptions,
): string {
  const { includeThinking, includeTools } = options;
  const lines: string[] = [];
  const title = sessionTitle || '对话记录';
  const now = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  const dateStr = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}`;

  lines.push(`# ${title}`);
  lines.push('');
  lines.push(`> 导出时间：${dateStr}`);
  lines.push('');
  lines.push('---');
  lines.push('');

  for (const m of messages) {
    // 跳过 UI 元素：warning 气泡、summary 卡片、system 消息
    if (m.variant === 'warning') continue;
    if (m.role === 'summary') continue;
    if (m.role === 'system') continue;

    if (m.role === 'user') {
      lines.push(`## 用户`);
      lines.push('');
      lines.push(m.content || '');
      lines.push('');
      continue;
    }

    if (m.role === 'assistant') {
      const content = m.content.trim();
      if (!content) continue;
      lines.push(`## 助手`);
      lines.push('');
      // 思考过程（勾选时才包含）
      if (includeThinking && m.thinking && m.thinking.trim()) {
        lines.push('<details>');
        lines.push('<summary>思考过程</summary>');
        lines.push('');
        lines.push(m.thinking.trim());
        lines.push('');
        lines.push('</details>');
        lines.push('');
      }
      lines.push(content);
      lines.push('');
      continue;
    }

    if (m.role === 'tool') {
      // 工具调用（勾选时才包含）
      if (!includeTools) continue;
      const sepIdx = m.content.indexOf(TOOL_RESULT_SEPARATOR);
      const callPart = sepIdx >= 0 ? m.content.slice(0, sepIdx) : m.content;
      const resultPart = sepIdx >= 0 ? m.content.slice(sepIdx + TOOL_RESULT_SEPARATOR.length) : '';

      lines.push('<details>');
      lines.push(`<summary>工具调用：${callPart.replace(/\s+/g, ' ').trim().slice(0, 80)}</summary>`);
      lines.push('');
      lines.push('```');
      lines.push(callPart.trim());
      lines.push('');
      if (resultPart) {
        lines.push('--- 结果 ---');
        lines.push(resultPart.trim());
      }
      lines.push('```');
      lines.push('');
      lines.push('</details>');
      lines.push('');
    }
  }

  return lines.join('\n');
}

// 保存文件到用户选择的路径。
// 优先使用 Wails 原生保存对话框（生产构建可用）；回退到浏览器 Blob 下载（开发模式 / 浏览器环境）。
// 用户取消对话框视为正常（静默返回）；真实写入失败则向上抛出，由调用方提示用户。
export async function downloadTextFile(content: string, filename: string) {
  // Wails 生产构建：window.go.main.DialogBinder.SaveFile 可用
  const wailsSave = (window as any)?.go?.main?.DialogBinder?.SaveFile;
  if (typeof wailsSave === 'function') {
    try {
      await wailsSave(filename, content);
      return;
    } catch (e) {
      throw new Error('保存文件失败：' + (e instanceof Error ? e.message : String(e)));
    }
  }
  // 回退：浏览器 Blob 下载（开发模式 vite dev server 场景）
  const blob = new Blob([content], { type: 'text/markdown;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
