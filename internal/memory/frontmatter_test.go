package memory

import "testing"

func TestFormatAndParseRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   Entry
	}{
		{"simple", Entry{Name: "go-env", Description: "Go 环境坑", Type: TypeProject, Body: "正文内容"}},
		{"colon-in-desc", Entry{Name: "x", Description: "含: 冒号的描述", Type: TypeUser, Body: "b"}},
		{"quote-in-desc", Entry{Name: "y", Description: `带"引号"的`, Type: TypeFeedback, Body: "**Why:** 1\n**How to apply:** 2"}},
		{"multiline-body", Entry{Name: "z", Description: "多行", Type: TypeReference, Body: "第一行\n\n第二行\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := formatEntry(c.in)
			got, err := parseEntry(c.in.Name, raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got.Name != c.in.Name || got.Description != c.in.Description ||
				got.Type != c.in.Type || got.Body != c.in.Body {
				t.Errorf("round-trip mismatch\nwant=%+v\ngot =%+v\nraw=%q", c.in, got, raw)
			}
		})
	}
}

func TestParseSkipsFrontmatterWhenAbsent(t *testing.T) {
	got, err := parseEntry("plain", "只是一段正文\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Name != "plain" || got.Body != "只是一段正文\n" || got.Type != "" {
		t.Errorf("unexpected: %+v", got)
	}
}
