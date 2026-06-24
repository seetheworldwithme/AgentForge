package memory

import (
	"sort"
	"strings"
)

func stringsTrimSuffix(s, suffix string) string { return strings.TrimSuffix(s, suffix) }

// isValidNameQuiet 不返回错误，仅布尔，用于扫描过滤。
func isValidNameQuiet(name string) bool { return ValidName(name) == nil }

// sortByMtimeDesc 原地按 UpdatedAt 倒序（最近更新在前）。
func sortByMtimeDesc(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool { return es[i].UpdatedAt.After(es[j].UpdatedAt) })
}
