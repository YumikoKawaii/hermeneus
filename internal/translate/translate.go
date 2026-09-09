package translate

import (
	"errors"
	"regexp"
	"strings"
)

// ErrUnknownQuery is returned when a SELECT does not match any registered Coroot
// query. The server turns this into a ClickHouse exception AND logs the raw SQL —
// the upgrade tripwire (see docs/DESIGN.md §3).
var ErrUnknownQuery = errors.New("hermeneus: unrecognised query, no translation registered")

// Kind classifies an incoming statement so the server can route it.
type Kind int

const (
	KindUnknown Kind = iota
	KindSystemProbe // system.* / handshake probe -> internal/system
	KindDDL         // CREATE/ALTER/MV -> swallow or map
	KindInsert      // INSERT -> Stream Load
	KindSelect      // known Coroot SELECT -> Translate
)

// ResultShape declares the exact column order + ClickHouse types Coroot's
// Result.Auto() expects back, so the server can encode the block correctly.
type ResultShape struct {
	Columns []Column
}

type Column struct {
	Name   string
	CHType string // e.g. "String", "UInt64", "DateTime64(9)", "Array(String)", "Map(String,String)"
}

// Translated is the output of the rewriter: StarRocks SQL plus the result shape
// to encode the response with.
type Translated struct {
	SQL   string
	Shape ResultShape
}

// Classify inspects a raw statement body and decides its Kind.
// Coroot's clickhouse-go binds @named args client-side, so bodies arrive as
// concrete SQL. This is a cheap prefix/keyword pass, no full parse.
func Classify(sql string) Kind {
	s := strings.ToUpper(strings.TrimSpace(sql))
	switch {
	case strings.HasPrefix(s, "INSERT"):
		return KindInsert
	case strings.HasPrefix(s, "CREATE"), strings.HasPrefix(s, "ALTER"),
		strings.HasPrefix(s, "DROP"), strings.HasPrefix(s, "RENAME"),
		strings.HasPrefix(s, "TRUNCATE"), strings.HasPrefix(s, "OPTIMIZE"),
		strings.HasPrefix(s, "SET "), strings.HasPrefix(s, "USE "):
		return KindDDL
	case strings.Contains(s, "SYSTEM."), strings.Contains(s, "CURRENTDATABASE()"):
		return KindSystemProbe
	case strings.HasPrefix(s, "SELECT"), strings.HasPrefix(s, "WITH"):
		return KindSelect
	default:
		return KindUnknown
	}
}

// registered pairs a matcher for a known Coroot SELECT with its result shape.
// Coroot's clickhouse-go binds @named args client-side, so we match and rewrite
// concrete SQL (values already substituted). Matching is done on a normalised
// form (whitespace-collapsed) so binding differences don't defeat the match.
type registered struct {
	match func(norm string) bool
	shape ResultShape
}

var wsRe = regexp.MustCompile(`\s+`)

func normalise(sql string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(sql, " "))
}

var registry = []registered{
	{
		// GetServicesFromLogs:
		//   SELECT DISTINCT ServiceName FROM otel_logs_service_name_severity_text WHERE LastSeen >= ...
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT DISTINCT ServiceName FROM otel_logs_service_name_severity_text") ||
				strings.HasPrefix(n, "SELECT DISTINCT ServiceName FROM otel_logs_service_name_severity_text_distributed")
		},
		shape: ResultShape{Columns: []Column{{Name: "ServiceName", CHType: "String"}}},
	},
	{
		// GetLogsHistogram:
		//   SELECT multiIf(SeverityNumber=0, 0, intDiv(SeverityNumber, 4)+1),
		//          toStartOfInterval(Timestamp, INTERVAL n second), count(1)
		//   FROM otel_logs WHERE ... GROUP BY 1, 2
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT multiIf(SeverityNumber=0, 0, intDiv(SeverityNumber, 4)+1), toStartOfInterval(Timestamp,") &&
				strings.Contains(n, "count(1)") && strings.Contains(n, "FROM otel_logs")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "severity", CHType: "Int64"},
			{Name: "ts", CHType: "DateTime"},
			{Name: "count", CHType: "UInt64"},
		}},
	},
}

// CH → StarRocks construct rewrites (see docs/DESIGN.md §4). Applied in order to
// a matched query body. Values are already bound, so these are textual.
var (
	// toStartOfInterval(<ts>, INTERVAL n second) -> from_unixtime(floor(unix_timestamp(<ts>)/n)*n)
	toStartOfIntervalRe = regexp.MustCompile(`toStartOfInterval\(\s*([^,]+?)\s*,\s*INTERVAL\s+(\d+)\s+second\s*\)`)
	// GLOBAL IN -> IN
	globalInRe = regexp.MustCompile(`\bGLOBAL\s+IN\b`)
)

func rewriteConstructs(sql string) string {
	sql = toStartOfIntervalRe.ReplaceAllString(sql, "from_unixtime(floor(unix_timestamp($1)/$2)*$2)")
	sql = rewriteMultiIfAll(sql)
	sql = rewriteIntDivAll(sql)
	sql = globalInRe.ReplaceAllString(sql, "IN")
	return sql
}

// rewriteIntDivAll replaces intDiv(a, b) with floor((a)/(b)); paren-aware.
func rewriteIntDivAll(sql string) string {
	const kw = "intDiv("
	for {
		i := strings.Index(sql, kw)
		if i < 0 {
			return sql
		}
		open := i + len(kw) - 1
		close := matchParen(sql, open)
		if close < 0 {
			return sql
		}
		parts := splitTopLevel(sql[open+1 : close])
		if len(parts) != 2 {
			return sql // unexpected; leave for the tripwire
		}
		repl := "floor((" + strings.TrimSpace(parts[0]) + ")/(" + strings.TrimSpace(parts[1]) + "))"
		sql = sql[:i] + repl + sql[close+1:]
	}
}

// rewriteMultiIfAll replaces every multiIf(...) call (paren-aware, may nest)
// with a CASE expression.
func rewriteMultiIfAll(sql string) string {
	const kw = "multiIf("
	for {
		i := strings.Index(sql, kw)
		if i < 0 {
			return sql
		}
		open := i + len(kw) - 1 // index of '('
		close := matchParen(sql, open)
		if close < 0 {
			return sql // unbalanced; leave for the tripwire
		}
		inner := sql[open+1 : close]
		sql = sql[:i] + rewriteMultiIf(inner) + sql[close+1:]
	}
}

// matchParen returns the index of the ')' matching the '(' at open, or -1.
func matchParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// rewriteMultiIf turns c1,v1,c2,v2,...,else into a CASE expression.
func rewriteMultiIf(inner string) string {
	parts := splitTopLevel(inner)
	if len(parts) < 3 || len(parts)%2 == 0 {
		return "multiIf(" + inner + ")" // not the shape we handle; leave for the tripwire
	}
	var b strings.Builder
	b.WriteString("CASE")
	i := 0
	for ; i+1 < len(parts); i += 2 {
		b.WriteString(" WHEN ")
		b.WriteString(strings.TrimSpace(parts[i]))
		b.WriteString(" THEN ")
		b.WriteString(strings.TrimSpace(parts[i+1]))
	}
	b.WriteString(" ELSE ")
	b.WriteString(strings.TrimSpace(parts[i]))
	b.WriteString(" END")
	return b.String()
}

// splitTopLevel splits on commas not nested inside parentheses.
func splitTopLevel(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// Translate rewrites a known Coroot ClickHouse SELECT into StarRocks SQL.
// Returns ErrUnknownQuery if the statement matches no registered pattern —
// the upgrade tripwire (docs/DESIGN.md §3).
func Translate(sql string) (Translated, error) {
	n := normalise(sql)
	for _, r := range registry {
		if r.match(n) {
			return Translated{SQL: rewriteConstructs(n), Shape: r.shape}, nil
		}
	}
	return Translated{}, ErrUnknownQuery
}
