package parser

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
)

type PDF struct{}

func (PDF) Supports(mime, fn string) bool {
	return mime == "application/pdf" || strings.HasSuffix(strings.ToLower(fn), ".pdf")
}

// Parse 提取 PDF 全文。ledongthuc/pdf 会按页解析内容流、用字体的
// ToUnicode(CMap) 解码文字，因此支持中文/CID 字体。对扫描型 PDF（纯图片、
// 无文字层）仍提不出文本——那种需要 OCR，不在此处理。
func (PDF) Parse(r io.Reader) (string, error) {
	// ledongthuc/pdf 需要 io.ReaderAt，所以先把整个流读进内存。
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	reader, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	textReader, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("extract pdf text: %w", err)
	}
	text, err := io.ReadAll(textReader)
	if err != nil {
		return "", fmt.Errorf("read pdf text: %w", err)
	}
	if strings.TrimSpace(string(text)) == "" {
		return "", fmt.Errorf("no extractable PDF text found (scanned PDFs need OCR)")
	}
	return string(text), nil
}
