package parser

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
)

var (
	nl3PlusRE = regexp.MustCompile(`\n{3,}`)   // 3+ 连续换行 → 空行
	wsRunRE   = regexp.MustCompile(`[^\S\n]+`) // 行内连续空白（不含换行）→ 单空格
)

type PDF struct{}

func (PDF) Supports(mime, fn string) bool {
	return mime == "application/pdf" || strings.HasSuffix(strings.ToLower(fn), ".pdf")
}

// Parse 提取 PDF 全文。ledongthuc/pdf 按页解析内容流、用字体的
// ToUnicode(CMap) 解码文字，支持中文/CID 字体。提取后做碎片归一化
// （PDF 常把每个字/词切成独立文本块）。扫描型 PDF（纯图片、无文字层）
// 提不出文本，需要 OCR，不在此处理。
func (PDF) Parse(r io.Reader) (ParseResult, error) {
	// ledongthuc/pdf 需要 io.ReaderAt，所以先把整个流读进内存。
	b, err := io.ReadAll(r)
	if err != nil {
		return ParseResult{}, err
	}

	// 文字：ledongthuc/pdf 逐页提取（中文/CID 可靠）。
	reader, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return ParseResult{}, fmt.Errorf("open pdf: %w", err)
	}
	numPages := reader.NumPage()
	pageTexts := make([]string, numPages)
	for i := 1; i <= numPages; i++ {
		p := reader.Page(i)
		if p.V.IsNull() {
			continue
		}
		if txt, err := p.GetPlainText(nil); err == nil {
			pageTexts[i-1] = normalizePDFText(txt)
		}
	}

	// 图片：pdfcpu 按页提取。失败不致命，退化为无图片（仅文字）。
	var images []ParsedImage
	imgPages, imgErr := pdfcpuapi.ExtractImagesRaw(bytes.NewReader(b), nil, nil)
	if imgErr != nil {
		log.Printf("pdf: extract images failed (continuing text-only): %v", imgErr)
	}

	// 组装：每页文字 + 该页图片占位符（按页对齐）。
	var out strings.Builder
	for pageIdx := 0; pageIdx < numPages; pageIdx++ {
		if pageIdx < len(pageTexts) {
			out.WriteString(pageTexts[pageIdx])
		}
		if imgErr == nil && pageIdx < len(imgPages) {
			for _, k := range sortedIntKeys(imgPages[pageIdx]) {
				img := imgPages[pageIdx][k]
				data, err := io.ReadAll(img)
				if err != nil || len(data) == 0 {
					continue
				}
				idx := len(images)
				images = append(images, ParsedImage{Data: data, MIME: imgFileTypeToMIME(img.FileType)})
				out.WriteString(ImagePlaceholder(idx))
			}
		}
		out.WriteString("\n")
	}

	if strings.TrimSpace(out.String()) == "" && len(images) == 0 {
		return ParseResult{}, fmt.Errorf("no extractable PDF text found (scanned PDFs need OCR)")
	}
	return ParseResult{Text: out.String(), Images: images}, nil
}

// normalizePDFText 把 PDF 按文本块提取的碎片合并成正常段落：
//   - 统一换行；行内连续空白折叠为单空格并 trim
//   - 段落内的单个换行视为碎片断行，按中英文规则拼接（含中文时不加空格，
//     纯英文/数字之间加空格）
//   - 仅保留空行作为段落分隔，多余空行压缩
func normalizePDFText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = nl3PlusRE.ReplaceAllString(s, "\n\n")

	paras := strings.Split(s, "\n\n")
	out := make([]string, 0, len(paras))
	for _, p := range paras {
		if p = joinFragmentLines(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

// joinFragmentLines 合并一段内被单换行拆开的碎片行。
func joinFragmentLines(p string) string {
	lines := strings.Split(p, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = wsRunRE.ReplaceAllString(ln, " ")
		ln = strings.TrimSpace(ln)
		if ln != "" {
			cleaned = append(cleaned, ln)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(cleaned[0])
	for i := 1; i < len(cleaned); i++ {
		// 决定两行碎片之间是否加空格：
		//   - 任一端是中文/全角 → 不加（中文紧凑，如 用server）
		//   - 两端都是拉丁字母 → 加（两个英文词，如 server address）
		//   - 其余（含数字/标点，如 V+10、202+4）→ 不加，视作同一记号被拆开
		pl, fl := lastRune(cleaned[i-1]), firstRune(cleaned[i])
		if !isCJK(pl) && !isCJK(fl) && isLatin(pl) && isLatin(fl) {
			b.WriteByte(' ')
		}
		b.WriteString(cleaned[i])
	}
	return b.String()
}

// isCJK 判断中文/全角字符（用于决定碎片拼接时是否加空格）。
func isCJK(r rune) bool {
	return (r >= 0x4e00 && r <= 0x9fff) || // CJK 统一表意
		(r >= 0x3400 && r <= 0x4dbf) || // CJK 扩展A
		(r >= 0x3000 && r <= 0x303f) || // CJK 标点
		(r >= 0xff00 && r <= 0xffef) // 全角形式
}

// isLatin 判断 ASCII 拉丁字母（a-z, A-Z）。
func isLatin(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func lastRune(s string) rune {
	if s == "" {
		return 0
	}
	r := []rune(s)
	return r[len(r)-1]
}

func firstRune(s string) rune {
	if s == "" {
		return 0
	}
	r := []rune(s)
	return r[0]
}

// sortedIntKeys 返回 map 的 int 键升序，保证图片按出现顺序输出。
func sortedIntKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// imgFileTypeToMIME 把 pdfcpu 的 FileType（"png"/"jpeg"...）转成 MIME。
func imgFileTypeToMIME(ft string) string {
	switch strings.ToLower(ft) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "bmp":
		return "image/bmp"
	case "tif", "tiff":
		return "image/tiff"
	}
	return "image/png" // 兜底
}
