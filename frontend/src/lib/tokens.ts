// 前端共享的 token 估算：原内联于 MessageBubble，提取为公共 lib 以便复用。
// 与后端 internal/agent/token.go 的 EstimateTokens 精确对齐：遍历码点，落入 CJK
// 区间（0x3000..0x9fff / 0xf900..0xfaff / 0xff00..0xffef）约计 1 token，其余约
// 4 个字符计 1 token。仅作预算/实时展示参考，不代表精确值。
export function estimateTokens(s: string): number {
  let cjk = 0;
  let other = 0;
  for (const ch of s) {
    const code = ch.codePointAt(0)!;
    if ((code >= 0x3000 && code <= 0x9fff) || (code >= 0xf900 && code <= 0xfaff) || (code >= 0xff00 && code <= 0xffef)) {
      cjk++;
    } else {
      other++;
    }
  }
  return cjk + other / 4;
}

// 估算多条消息累计 token：每条累加正文 + 各工具调用参数，与后端
// EstimateMessageTokens 对齐（含 tool_calls，避免前端低估）。
export function estimateMessagesTokens(msgs: { content?: string; tool_calls?: { args?: string }[] }[]): number {
  let n = 0;
  for (const m of msgs) {
    n += estimateTokens(m.content ?? '');
    if (m.tool_calls) {
      for (const tc of m.tool_calls) {
        n += estimateTokens(tc.args ?? '');
      }
    }
  }
  return n;
}
