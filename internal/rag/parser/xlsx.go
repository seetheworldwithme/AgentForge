package parser

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

type XLSX struct{}

func (XLSX) Supports(mime, fn string) bool {
	return mime == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
		strings.HasSuffix(strings.ToLower(fn), ".xlsx")
}

// Parse 把每个 sheet 的表格转成 Markdown 表格；多 sheet 用 "# 工作表名" 分隔。
func (XLSX) Parse(r io.Reader) (ParseResult, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return ParseResult{}, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		return ParseResult{}, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	var sb strings.Builder
	for i, sheet := range sheets {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return ParseResult{}, fmt.Errorf("read sheet %s: %w", sheet, err)
		}
		if len(rows) == 0 {
			continue
		}
		if i > 0 {
			sb.WriteString("\n\n")
		}
		if len(sheets) > 1 {
			sb.WriteString("# " + sheet + "\n\n")
		}
		sb.WriteString(renderMarkdownTable(rows))
	}
	text := sb.String()
	if strings.TrimSpace(text) == "" {
		return ParseResult{}, fmt.Errorf("no rows found in xlsx")
	}
	return ParseResult{Text: text}, nil
}

// renderMarkdownTable 把行数据渲染成 Markdown 表格（首行作表头），
// xlsx 和 docx 共用。
func renderMarkdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	ncol := 0
	for _, r := range rows {
		if len(r) > ncol {
			ncol = len(r)
		}
	}
	padRow := func(r []string) []string {
		out := make([]string, ncol)
		for i := range out {
			if i < len(r) {
				out[i] = sanitizeCell(r[i])
			}
		}
		return out
	}
	var sb strings.Builder
	writeLine := func(r []string) {
		sb.WriteString("| ")
		sb.WriteString(strings.Join(r, " | "))
		sb.WriteString(" |\n")
	}
	writeLine(padRow(rows[0]))
	sep := make([]string, ncol)
	for i := range sep {
		sep[i] = "---"
	}
	writeLine(sep)
	for _, r := range rows[1:] {
		writeLine(padRow(r))
	}
	return sb.String()
}

// sanitizeCell 清理单元格内容，避免破坏 Markdown 表格结构。
func sanitizeCell(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}
