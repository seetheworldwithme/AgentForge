package agent

import "github.com/agent-rust/core/internal/llm"

// EstimateTokens 粗略估算字符串的 token 数，与前端
// frontend/src/components/MessageBubble.tsx 的 estimateTokens 精确对齐：
// 遍历 rune，落入 CJK 区间（0x3000..0x9fff / 0xf900..0xfaff / 0xff00..0xffef）
// 的码点约计 1 token，其余约 4 个字符计 1 token（整数除法）。空串返回 0。
func EstimateTokens(s string) int {
	cjk := 0
	other := 0
	for _, r := range s {
		switch {
		case r >= 0x3000 && r <= 0x9fff:
			cjk++
		case r >= 0xf900 && r <= 0xfaff:
			cjk++
		case r >= 0xff00 && r <= 0xffef:
			cjk++
		default:
			other++
		}
	}
	return cjk + other/4
}

// EstimateMessageTokens 估算单条消息占用的 token 数：正文 + 各工具调用参数 + 图片。
// 图片按 DataURL 字节长度保守量级估算（整除 750），仅用于预算决策，不代表精确值。
func EstimateMessageTokens(m llm.Message) int {
	n := EstimateTokens(m.Content)
	for i := range m.ToolCalls {
		n += EstimateTokens(m.ToolCalls[i].Args)
	}
	// 图片为保守量级估算：DataURL（base64 内联）字节累加后整除 750，
	// 仅参与预算决策，不追求精确。
	imgBytes := 0
	for i := range m.Images {
		imgBytes += len(m.Images[i].DataURL)
	}
	n += imgBytes / 750
	return n
}
