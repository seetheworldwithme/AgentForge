package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"slices"
	"strings"
)

type DOCX struct{}

func (DOCX) Supports(mime, fn string) bool {
	return mime == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
		strings.HasSuffix(strings.ToLower(fn), ".docx")
}

// Parse 解析 docx：文字流 + 表格（转 Markdown）+ 图片（在原位插占位符，
// 字节随 ParseResult.Images 返回，由 ingestor 调 VLM 替换）。
func (DOCX) Parse(r io.Reader) (ParseResult, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return ParseResult{}, err
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return ParseResult{}, fmt.Errorf("open docx (zip): %w", err)
	}
	doc, err := readZipEntry(zr, "word/document.xml")
	if err != nil {
		return ParseResult{}, fmt.Errorf("read document.xml: %w", err)
	}
	relsData, _ := readZipEntry(zr, "word/_rels/document.xml.rels")
	rels := parseRels(relsData)
	media := map[string][]byte{}
	for _, target := range rels {
		if strings.HasPrefix(target, "media/") {
			if data, err := readZipEntry(zr, "word/"+target); err == nil {
				media[target] = data
			}
		}
	}
	text, images := parseDocxBody(doc, rels, media)
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return ParseResult{}, fmt.Errorf("no extractable docx text")
	}
	return ParseResult{Text: text, Images: images}, nil
}

func readZipEntry(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("entry not found: %s", name)
}

// parseRels 解析 document.xml.rels，返回 rId → target（如 "media/image1.png"）。
func parseRels(data []byte) map[string]string {
	rels := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		id, target := "", ""
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			}
		}
		if id != "" {
			rels[id] = target
		}
	}
	return rels
}

// parseDocxBody 按 XML 出现顺序遍历 document.xml，输出段落文字、表格
// （Markdown）和图片占位符。图片占位符位置 = 它在文档中的位置，保证顺序。
func parseDocxBody(documentXML []byte, rels map[string]string, media map[string][]byte) (string, []ParsedImage) {
	dec := xml.NewDecoder(bytes.NewReader(documentXML))
	var out strings.Builder
	var images []ParsedImage

	var stack []string // 当前 open 元素的 local name
	var para strings.Builder
	var inT bool
	var rows [][]string
	var row []string
	var cell strings.Builder

	hasAncestor := func(name string) bool { return slices.Contains(stack, name) }

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			local := t.Name.Local
			stack = append(stack, local)
			switch local {
			case "p":
				para.Reset()
			case "t":
				inT = true
			case "tbl":
				rows = nil
			case "tr":
				row = nil
			case "tc":
				cell.Reset()
			case "blip":
				// 图片：<a:blip r:embed="rIdX"> → rels → media 字节
				target := rels[attrVal(t.Attr, "embed")]
				if data, ok := media[target]; ok {
					idx := len(images)
					images = append(images, ParsedImage{Data: data, MIME: sniffImageMIME(data)})
					para.WriteString(ImagePlaceholder(idx))
				}
			case "tab":
				para.WriteString("\t")
			case "br":
				para.WriteString("\n")
			}
		case xml.EndElement:
			local := t.Name.Local
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			switch local {
			case "t":
				inT = false
			case "p":
				s := para.String()
				if hasAncestor("tc") {
					// 表格单元格内的段落，累加到 cell
					if cell.Len() > 0 {
						cell.WriteString(" ")
					}
					cell.WriteString(s)
				} else if strings.TrimSpace(s) != "" {
					out.WriteString(s)
					out.WriteString("\n")
				}
			case "tc":
				row = append(row, cell.String())
			case "tr":
				rows = append(rows, row)
			case "tbl":
				out.WriteString(renderMarkdownTable(rows))
			}
		case xml.CharData:
			if inT {
				para.Write(t)
			}
		}
	}
	return out.String(), images
}

func attrVal(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// sniffImageMIME 按文件头判断图片类型。
func sniffImageMIME(data []byte) string {
	switch {
	case len(data) >= 8 && bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case len(data) >= 3 && bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case len(data) >= 6 && (bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))):
		return "image/gif"
	}
	return "image/png" // 兜底
}
