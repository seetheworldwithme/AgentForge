package parser

import (
	"io"
	"regexp"
	"strconv"
)

// Parser 把文档解析成文本（+ 提取的图片），供切片+向量化。图片在文本中的
// 位置用占位符 ImagePlaceholder(i) 标记，对应 Images[i]；由 ingestor 调 VLM
// 把占位符替换成图片描述（见 ReplacePlaceholders）。
type Parser interface {
	Supports(mimeType, filename string) bool
	Parse(r io.Reader) (ParseResult, error)
}

type ParseResult struct {
	Text   string
	Images []ParsedImage
}

type ParsedImage struct {
	Data []byte
	MIME string // "image/png" 等
}

var placeholderRE = regexp.MustCompile("\x00IMG:(\\d+)\x00")

// ImagePlaceholder 返回第 i 张图片在文本中的占位符（NUL 包裹，不会与正文冲突）。
func ImagePlaceholder(i int) string {
	return "\x00IMG:" + strconv.Itoa(i) + "\x00"
}

// ReplacePlaceholders 遍历文本中的图片占位符，用 fn(index) 的返回值替换。
// ingestor 用它把占位符替换成 VLM 描述；fn 返回 "" 即清除（图片跳过）。
func ReplacePlaceholders(text string, fn func(idx int) string) string {
	return placeholderRE.ReplaceAllStringFunc(text, func(m string) string {
		i, _ := strconv.Atoi(m[len("\x00IMG:") : len(m)-len("\x00")])
		return fn(i)
	})
}
