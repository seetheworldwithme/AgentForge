package rag

import (
	"strings"
	"unicode/utf8"
)

// plainSeparators 是普通文本递归切分的分隔符优先级（从粗到细）。splitPlain 按
// 此顺序尝试：先用粗的（段落）切，切不动或某段仍超 size 再用更细的（句末标点），
// 最后用 rune 滑窗兜底。英文句末标点带尾随空格，避免切断小数/缩写（如 3.14、e.g.）。
var plainSeparators = []string{
	"\n\n", "\n",
	"。", "！", "？", "；",
	". ", "! ", "? ", "; ",
	"，", ", ",
}

// Chunk 把 text 切成最多 size 个 rune 的片段，相邻片段在语义边界处重叠约 overlap
// 个 rune。采用分层递归切分：代码块（```/~~~ 围栏）整体保护 → 段落/换行 → 句末
// 标点 → 逗号 → rune 滑窗兜底，尽量在语义边界断开，且对 UTF-8（中文）rune 安全，
// 不会切出半截汉字。
//
// size/overlap 按 rune 计数（ASCII 下 = 字节数，向后兼容）。
func Chunk(text string, size, overlap int) []string {
	if size <= 0 {
		size = 800
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) <= size {
		return []string{text}
	}
	// 1. 切成 ≤ size 的叶子（代码块保护 + 普通文本递归切分）
	leaves := splitLeaves(runes, size)
	// 2. 贪心聚合并加 overlap
	return mergeSplits(leaves, size, overlap)
}

// chunkBlock 是 splitLeaves 的中间产物：一段文本 + 是否受保护（代码块）。
type chunkBlock struct {
	text      string
	protected bool // 代码块：不在内部按标点切
}

// splitLeaves 把文本切成不超过 size 个 rune 的叶子片段。代码块（```/~~~ 围栏）
// 作为受保护整体（超 size 再按行切），其余用 plainSeparators 递归切分。
func splitLeaves(runes []rune, size int) []string {
	var leaves []string
	for _, b := range splitCodeBlocks(string(runes)) {
		br := []rune(b.text)
		if len(br) <= size {
			leaves = append(leaves, b.text)
			continue
		}
		if b.protected {
			leaves = append(leaves, splitCodeByLine(b.text, size)...)
		} else {
			leaves = append(leaves, splitPlain(br, size)...)
		}
	}
	return leaves
}

// splitCodeBlocks 把文本按行扫描切成块：``` 或 ~~~ 围栏内的代码块标记为 protected，
// 其余为普通文本块。未闭合的围栏（文档残缺）整体按代码块处理。
func splitCodeBlocks(text string) []chunkBlock {
	lines := strings.Split(text, "\n")
	var blocks []chunkBlock
	var cur []string
	inCode := false
	flush := func(protected bool) {
		if len(cur) == 0 {
			return
		}
		blocks = append(blocks, chunkBlock{text: strings.Join(cur, "\n"), protected: protected})
		cur = nil
	}
	for _, line := range lines {
		fence := isFence(line)
		if fence && !inCode {
			flush(false) // 落盘之前的普通文本
			cur = append(cur, line)
			inCode = true
			continue
		}
		if fence && inCode {
			cur = append(cur, line)
			flush(true) // 落盘整个代码块
			inCode = false
			continue
		}
		cur = append(cur, line)
	}
	flush(inCode) // 收尾：未闭合围栏按代码块处理
	return blocks
}

// isFence 判断一行是否是代码块围栏（``` 或 ~~~ 开头，允许缩进和语言标记）。
func isFence(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// splitCodeByLine 把超长代码块按行贪心切成 ≤ size 的片段，每段重新包裹开/闭围栏，
// 保证切分后每段仍是合法的代码块。
func splitCodeByLine(text string, size int) []string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return []string{text}
	}
	openFence := lines[0]
	rest := lines[1:]
	closeFence := "```"
	if len(rest) > 0 && isFence(rest[len(rest)-1]) {
		closeFence = strings.TrimSpace(rest[len(rest)-1])
		rest = rest[:len(rest)-1]
	}
	var out []string
	var cur strings.Builder
	cur.WriteString(openFence + "\n")
	curLen := utf8.RuneCountInString(openFence) + 1
	for _, line := range rest {
		n := utf8.RuneCountInString(line) + 1 // +1 for \n
		if curLen+n > size && cur.Len() > len(openFence)+1 {
			cur.WriteString(closeFence + "\n")
			out = append(out, cur.String())
			cur.Reset()
			cur.WriteString(openFence + "\n")
			curLen = utf8.RuneCountInString(openFence) + 1
		}
		cur.WriteString(line + "\n")
		curLen += n
	}
	cur.WriteString(closeFence + "\n")
	out = append(out, cur.String())
	return out
}

// splitPlain 递归按 plainSeparators 切分；切不动或单段仍超 size 时用 rune 滑窗兜底。
// 返回的叶子均 ≤ size 个 rune。
func splitPlain(runes []rune, size int) []string {
	if len(runes) <= size {
		return []string{string(runes)}
	}
	for _, sep := range plainSeparators {
		parts := splitOn(runes, sep)
		if len(parts) <= 1 {
			continue // 该分隔符没切动，试更细的
		}
		var out []string
		for _, p := range parts {
			if len(p) <= size {
				out = append(out, string(p))
			} else {
				out = append(out, splitPlain(p, size)...) // 仍超 size，递归用更细分隔符
			}
		}
		return out
	}
	return slideRunes(runes, size) // 所有分隔符都切不动（如一长串无标点），滑窗兜底
}

// splitOn 按 sep（可能是多 rune 字符串）切分 runes，sep 附在前段末尾（句末标点
// 属于该句）。返回非空片段。
func splitOn(runes []rune, sep string) [][]rune {
	sepRunes := []rune(sep)
	if len(sepRunes) == 0 || len(sepRunes) > len(runes) {
		return [][]rune{runes}
	}
	var parts [][]rune
	start := 0
	for i := 0; i+len(sepRunes) <= len(runes); i++ {
		eq := true
		for j := range sepRunes {
			if runes[i+j] != sepRunes[j] {
				eq = false
				break
			}
		}
		if eq {
			parts = append(parts, runes[start:i+len(sepRunes)])
			start = i + len(sepRunes)
			i += len(sepRunes) - 1
		}
	}
	if start < len(runes) {
		parts = append(parts, runes[start:])
	}
	var out [][]rune
	for _, p := range parts {
		if len(p) > 0 {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return [][]rune{runes}
	}
	return out
}

// slideRunes 按 size 滑窗切（步长 = size，无重叠——重叠由 mergeSplits 负责），
// rune 安全的兜底切分。
func slideRunes(runes []rune, size int) []string {
	var out []string
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return out
}

// mergeSplits 把叶子贪心聚合成 ≤ size 的 chunk，并在叶子边界处实现 overlap：
// 当加入一片会超 size 时落盘当前 chunk，并从上一 chunk 末尾保留累计 ≤ overlap 的
// 叶子作为新 chunk 的开头，使相邻 chunk 共享这部分上下文。
func mergeSplits(splits []string, size, overlap int) []string {
	var docs []string
	var cur []string
	curLen := 0
	for _, s := range splits {
		n := utf8.RuneCountInString(s)
		if curLen+n > size && len(cur) > 0 {
			docs = append(docs, strings.Join(cur, ""))
			// 从头部移除叶子，直到剩余累计 ≤ overlap（为新 chunk 提供重叠上下文）
			for curLen > overlap || curLen+n > size {
				if len(cur) == 0 {
					break
				}
				curLen -= utf8.RuneCountInString(cur[0])
				cur = cur[1:]
			}
		}
		cur = append(cur, s)
		curLen += n
	}
	if len(cur) > 0 {
		docs = append(docs, strings.Join(cur, ""))
	}
	return docs
}
