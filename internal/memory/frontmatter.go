package memory

import (
	"strings"
)

// escapeScalar 把字符串安全地包进双引号（YAML 双引号标量），内部 \ 与 " 转义。
func escapeScalar(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func unescapeScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		r := strings.NewReplacer(`\"`, `"`, `\\`, `\`)
		return r.Replace(inner)
	}
	return s
}

// splitKV 把 "key: value" 拆为 key、value；无冒号或空行返回 ok=false。
func splitKV(line string) (key, val string, ok bool) {
	i := strings.Index(line, ":")
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// formatEntry 把 Entry 序列化为完整 .md 文件内容（frontmatter + 正文）。
// body 原样写入（不补/不删尾换行），确保与 parseEntry 精确 round-trip。
func formatEntry(e Entry) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("name: " + e.Name + "\n")
	sb.WriteString("description: " + escapeScalar(e.Description) + "\n")
	sb.WriteString("type: " + string(e.Type) + "\n")
	sb.WriteString("---\n\n")
	sb.WriteString(e.Body)
	return sb.String()
}

const fmOpen = "---\n"

// parseEntry 解析 .md 文件内容为 Entry。frontmatter 仅支持扁平 key: value；
// 无 frontmatter 时视为纯正文，Name 取参数传入。
func parseEntry(name, raw string) (Entry, error) {
	e := Entry{Name: name}
	body := raw
	if strings.HasPrefix(raw, fmOpen) {
		rest := raw[len(fmOpen):]
		end := strings.Index(rest, "\n---")
		if end >= 0 {
			fm := rest[:end]
			after := rest[end+len("\n---"):]
			// 去掉结束分隔符后的所有前导换行（formatEntry 写入的空行分隔）。
			body = strings.TrimLeft(after, "\n")
			for _, line := range strings.Split(fm, "\n") {
				k, v, ok := splitKV(line)
				if !ok {
					continue
				}
				switch k {
				case "name":
					e.Name = v
				case "description":
					e.Description = unescapeScalar(v)
				case "type":
					e.Type = Type(v)
				}
			}
		}
	}
	e.Body = body
	return e, nil
}
