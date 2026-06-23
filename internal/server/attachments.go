package server

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 排除目录:目录树注入与菜单列目录时跳过这些常见噪音目录,
// 避免上下文膨胀与菜单混乱(参考此前「rag 导致上下文爆炸」的教训)。
var excludedDirNames = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	"target": true, "vendor": true, ".next": true, ".idea": true, ".vscode": true,
}

// 附件注入的硬上限,防止单文件或大目录撑爆上下文。
const (
	maxFileBytes    = 256 * 1024 // 单文件最多注入 256KB
	maxTreeEntries  = 2000       // 目录树最多列出 2000 个文件
	binaryProbeSize = 1024       // 检测二进制读取的前 N 字节
)

// treeItem 是列目录与注入共用的目录项结构。
type treeItem struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Path  string `json:"path"`
}

// resolveUnder 把相对路径解析到 workdir 之下,越界(如 ../ 逃逸)返回 ok=false。
// 防止前端传入的路径读取到工作目录之外的文件。
func resolveUnder(workdir, rel string) (string, bool) {
	root := filepath.Clean(workdir)
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	if abs == root {
		return abs, true
	}
	return abs, strings.HasPrefix(abs, root+string(os.PathSeparator))
}

// buildAttachments 把一组相对路径(workdir 下)解析为注入给模型的上下文文本。
// 文件读取内容(超上限截断),文件夹展开为目录树;越界/失败项标注后跳过。
// 返回空串表示无需注入(没有 workdir 或没有可用附件)。
func buildAttachments(workdir string, paths []string) string {
	if workdir == "" || len(paths) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<attachments>\n")
	for _, rel := range paths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		abs, ok := resolveUnder(workdir, rel)
		if !ok {
			fmt.Fprintf(&sb, "### %s\n（已跳过:路径越出工作目录）\n\n", rel)
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			fmt.Fprintf(&sb, "### %s\n（读取失败:%s）\n\n", rel, err.Error())
			continue
		}
		if info.IsDir() {
			writeDirTree(&sb, rel, abs)
		} else {
			writeFileSnippet(&sb, rel, abs)
		}
	}
	sb.WriteString("</attachments>\n\n")
	return sb.String()
}

// writeFileSnippet 读取单个文件并写入代码块,超 maxFileBytes 截断并标注,二进制则跳过内容。
func writeFileSnippet(sb *strings.Builder, rel, abs string) {
	data, err := os.ReadFile(abs)
	if err != nil {
		fmt.Fprintf(sb, "### 文件 %s\n（读取失败:%s）\n\n", rel, err.Error())
		return
	}
	if isBinary(data) {
		fmt.Fprintf(sb, "### 文件 %s\n（二进制文件,已跳过内容,%s）\n\n", rel, humanSize(len(data)))
		return
	}
	fmt.Fprintf(sb, "### 文件 %s\n```%s\n", rel, langForExt(filepath.Ext(rel)))
	if len(data) > maxFileBytes {
		sb.Write(data[:maxFileBytes])
		fmt.Fprintf(sb, "\n```\n（已截断:原 %s,保留前 %s）\n\n",
			humanSize(len(data)), humanSize(maxFileBytes))
		return
	}
	sb.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		sb.WriteByte('\n')
	}
	sb.WriteString("```\n\n")
}

// writeDirTree 遍历目录,列出所有文件的相对路径,排除噪音目录与二进制,超条数截断。
func writeDirTree(sb *strings.Builder, rel, abs string) {
	var files []string
	truncated := false
	_ = filepath.WalkDir(abs, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// 跳过 .git/node_modules 等噪音目录(不递归进入)
			if p != abs && excludedDirNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxTreeEntries {
			truncated = true
			return filepath.SkipDir
		}
		if isBinaryFile(p) {
			return nil
		}
		rp, err := filepath.Rel(abs, p)
		if err != nil {
			return nil
		}
		files = append(files, slashJoin(rel, filepath.ToSlash(rp)))
		return nil
	})
	fmt.Fprintf(sb, "### 目录 %s/（%d 个文件%s）\n", rel, len(files), truncSuffix(truncated))
	for _, f := range files {
		sb.WriteString(f)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
}

// isBinary 粗略判定:前若干字节含 NUL 视为二进制。
func isBinary(data []byte) bool {
	n := len(data)
	if n > binaryProbeSize {
		n = binaryProbeSize
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// isBinaryFile 打开文件读取前若干字节判定是否二进制;打不开视为二进制(跳过)。
func isBinaryFile(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return true
	}
	defer f.Close()
	buf := make([]byte, binaryProbeSize)
	n, _ := f.Read(buf)
	return bytes.IndexByte(buf[:n], 0) >= 0
}

// langForExt 按扩展名映射代码块语言标注;未知扩展名返回空串。
func langForExt(ext string) string {
	langs := map[string]string{
		".go": "go", ".js": "javascript", ".mjs": "javascript", ".cjs": "javascript",
		".ts": "typescript", ".tsx": "tsx", ".jsx": "jsx", ".vue": "vue",
		".py": "python", ".java": "java", ".kt": "kotlin", ".rs": "rust",
		".c": "c", ".h": "c", ".cpp": "cpp", ".hpp": "cpp", ".cc": "cpp",
		".cs": "csharp", ".rb": "ruby", ".php": "php", ".swift": "swift",
		".sh": "bash", ".bash": "bash", ".zsh": "bash", ".sql": "sql",
		".html": "html", ".css": "css", ".scss": "scss", ".less": "less",
		".json": "json", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml",
		".xml": "xml", ".md": "markdown", ".svelte": "svelte", ".lua": "lua",
		".proto": "proto", ".gradle": "groovy", ".dockerfile": "dockerfile",
	}
	return langs[strings.ToLower(ext)]
}

// slashJoin 用 '/' 拼接相对路径片段,与前端约定一致。
func slashJoin(rel, name string) string {
	if rel == "" {
		return name
	}
	return rel + "/" + name
}

func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func truncSuffix(truncated bool) string {
	if truncated {
		return ",已截断"
	}
	return ""
}
