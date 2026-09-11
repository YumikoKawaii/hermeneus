package extractor

import (
	"regexp"
	"strings"
)

type Record struct {
	Columns []string
	Values  []Value
}

type Value struct {
	Kind ValueKind
	V    any
}

type ValueKind uint8

const (
	KindNull ValueKind = iota
	KindString
	KindInt
	KindUint
	KindFloat
	KindBool
	KindTime  // time.Time
	KindArray // []Value
	KindMap   // map[string]Value
)

var insertRe = regexp.MustCompile("(?is)^\\s*INSERT\\s+INTO\\s+[`\"]?([A-Za-z_][A-Za-z0-9_.]*)[`\"]?\\s*\\(([^)]*)\\)")

// ExtractValues ...
func ExtractValues(body string) (table string, records []Record, ok bool) {
	m := insertRe.FindStringSubmatch(body)
	if m == nil {
		return "", nil, false
	}
	table = m[1]
	if i := strings.LastIndex(table, "."); i >= 0 {
		table = table[i+1:]
	}
	var cols []string
	for _, c := range strings.Split(m[2], ",") {
		c = strings.Trim(strings.TrimSpace(c), "`\"")
		if c != "" {
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		return "", nil, false
	}
	return table, []Record{{Columns: cols}}, true
}
