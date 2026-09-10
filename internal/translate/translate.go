package translate

import (
	"errors"
	"fmt"
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
	KindUnknown     Kind = iota
	KindSystemProbe      // system.* / handshake probe -> internal/system
	KindDDL              // CREATE/ALTER/MV -> swallow or map
	KindInsert           // INSERT -> Stream Load
	KindSelect           // known Coroot SELECT -> Translate
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
	{
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT ServiceName, Timestamp, multiIf(SeverityNumber=0, 0, intDiv(SeverityNumber, 4)+1), Body, TraceId, ResourceAttributes, LogAttributes FROM otel_logs")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "ServiceName", CHType: "String"},
			{Name: "Timestamp", CHType: "DateTime64(9)"},
			{Name: "severity", CHType: "Int64"},
			{Name: "Body", CHType: "String"},
			{Name: "TraceId", CHType: "String"},
			{Name: "ResourceAttributes", CHType: "Map(String,String)"},
			{Name: "LogAttributes", CHType: "Map(String,String)"},
		}},
	},
	{
		// GetServicesFromTraces (traces.go Q1):
		//   SELECT DISTINCT ServiceName FROM otel_traces_service_name WHERE LastSeen >= @from
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT DISTINCT ServiceName FROM otel_traces_service_name")
		},
		shape: ResultShape{Columns: []Column{{Name: "ServiceName", CHType: "String"}}},
	},
	{
		// GetProfileTypes (profiles.go Q10):
		//   SELECT DISTINCT ServiceName, Type FROM profiling_profiles WHERE LastSeen >= @from
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT DISTINCT ServiceName, Type FROM profiling_profiles")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "ServiceName", CHType: "String"},
			{Name: "Type", CHType: "String"},
		}},
	},
	{
		// trace-id ts window (traces.go Q2):
		//   SELECT min(Start), max(End)+1 FROM otel_traces_trace_id_ts WHERE TraceId ...
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT min(Start), max(End)+1 FROM otel_traces_trace_id_ts")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "min", CHType: "DateTime"},
			{Name: "max", CHType: "DateTime"},
		}},
	},
	{
		// getTraces (traces.go Q9):
		//   SELECT count(1), groupArray(distinct TraceId) FROM (SELECT TraceId FROM otel_traces WHERE ...)
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT count(1), groupArray(distinct TraceId) FROM (SELECT TraceId FROM otel_traces")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "count", CHType: "UInt64"},
			{Name: "traceIds", CHType: "Array(String)"},
		}},
	},
	{
		// querySpans / getTraceSpans (traces.go Q7, Q8): identical 14-col shape.
		// Events.* are CH Nested parallel-array columns; SR schema exposes them as
		// Array columns (mapping owned out-of-band, SQL passes through textually).
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT Timestamp, TraceId, SpanId, ParentSpanId, SpanName, ServiceName, Duration, StatusCode, StatusMessage, ResourceAttributes, SpanAttributes, Events.Timestamp, Events.Name, Events.Attributes FROM otel_traces")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "Timestamp", CHType: "DateTime64(9)"},
			{Name: "TraceId", CHType: "String"},
			{Name: "SpanId", CHType: "String"},
			{Name: "ParentSpanId", CHType: "String"},
			{Name: "SpanName", CHType: "String"},
			{Name: "ServiceName", CHType: "String"},
			{Name: "Duration", CHType: "Int64"},
			{Name: "StatusCode", CHType: "String"},
			{Name: "StatusMessage", CHType: "String"},
			{Name: "ResourceAttributes", CHType: "Map(String,String)"},
			{Name: "SpanAttributes", CHType: "Map(String,String)"},
			{Name: "Events.Timestamp", CHType: "Array(DateTime64(9))"},
			{Name: "Events.Name", CHType: "Array(String)"},
			{Name: "Events.Attributes", CHType: "Array(Map(String,String))"},
		}},
	},
	{
		// getSpansHistogram (traces.go Q3 MV branch / Q4 raw branch): same shape.
		//   MV:  SELECT toStartOfInterval(..), Bucket, sum(Total), sum(Failed) FROM otel_traces_histogram ...
		//   raw: SELECT toStartOfInterval(..), roundDown(Duration/1000000, [..]), count(1), countIf(..) FROM otel_traces ...
		match: func(n string) bool {
			return (strings.HasPrefix(n, "SELECT toStartOfInterval(Timestamp,") &&
				strings.Contains(n, ", Bucket, sum(Total), sum(Failed) FROM otel_traces_histogram")) ||
				(strings.HasPrefix(n, "SELECT toStartOfInterval(Timestamp,") &&
					strings.Contains(n, "roundDown(Duration/1000000,") && strings.Contains(n, "FROM otel_traces"))
		},
		shape: ResultShape{Columns: []Column{
			{Name: "ts", CHType: "DateTime"},
			{Name: "bucket", CHType: "Float64"},
			{Name: "total", CHType: "UInt64"},
			{Name: "failed", CHType: "UInt64"},
		}},
	},
	{
		// getTraceSpanStats (traces.go Q5 MV branch / Q6 raw branch): same shape.
		//   MV:  SELECT ServiceName, SpanName, Bucket, sum(Total), sum(Failed) FROM otel_traces_histogram ...
		//   raw: SELECT ServiceName, SpanName, roundDown(Duration/1000000, [..]), count(1), countIf(..) FROM otel_traces ...
		match: func(n string) bool {
			return strings.HasPrefix(n, "SELECT ServiceName, SpanName, Bucket, sum(Total), sum(Failed) FROM otel_traces_histogram") ||
				(strings.HasPrefix(n, "SELECT ServiceName, SpanName, roundDown(Duration/1000000,") &&
					strings.Contains(n, "FROM otel_traces"))
		},
		shape: ResultShape{Columns: []Column{
			{Name: "ServiceName", CHType: "String"},
			{Name: "SpanName", CHType: "String"},
			{Name: "bucket", CHType: "Float64"},
			{Name: "total", CHType: "UInt64"},
			{Name: "failed", CHType: "UInt64"},
		}},
	},
	{
		// getProfile (profiles.go Q11, qProfile):
		//   WITH samples AS (...), stacks AS (...) SELECT value, stack FROM stacks JOIN samples USING(hash)
		match: func(n string) bool {
			return strings.HasPrefix(n, "WITH samples AS (") &&
				strings.HasSuffix(n, "SELECT value, stack FROM stacks JOIN samples USING(hash)")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "value", CHType: "Int64"},
			{Name: "stack", CHType: "Array(String)"},
		}},
	},
	{
		// getProfile avg (profiles.go Q12, qProfileAvg):
		//   ... SELECT toInt64(value/profiles), stack FROM stacks JOIN samples USING(hash), profiles
		match: func(n string) bool {
			return strings.HasPrefix(n, "WITH samples AS (") &&
				strings.HasSuffix(n, "SELECT toInt64(value/profiles), stack FROM stacks JOIN samples USING(hash), profiles")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "value", CHType: "Int64"},
			{Name: "stack", CHType: "Array(String)"},
		}},
	},
	{
		// getDiffProfile (profiles.go Q13, qProfileDiff):
		//   ... SELECT base, comp, stack FROM stacks JOIN samples USING(hash)
		match: func(n string) bool {
			return strings.HasPrefix(n, "WITH samples AS (") &&
				strings.HasSuffix(n, "SELECT base, comp, stack FROM stacks JOIN samples USING(hash)")
		},
		shape: ResultShape{Columns: []Column{
			{Name: "base", CHType: "Int64"},
			{Name: "comp", CHType: "Int64"},
			{Name: "stack", CHType: "Array(String)"},
		}},
	},
}

// CH → StarRocks construct rewrites (see docs/DESIGN.md §4). Applied in order to
// a matched query body. Values are already bound, so these are textual.
var (
	// toStartOfInterval(<ts>, INTERVAL n second) -> from_unixtime(floor(unix_timestamp(<ts>)/n)*n)
	toStartOfIntervalRe = regexp.MustCompile(`toStartOfInterval\(\s*([^,]+?)\s*,\s*INTERVAL\s+(\d+)\s+second\s*\)`)
	// GLOBAL IN -> IN
	globalInRe     = regexp.MustCompile(`\bGLOBAL\s+IN\b`)
	// StarRocks requires every derived table to have an alias. Coroot's log
	// window query wraps a subquery as `min(Timestamp) FROM (SELECT ... )` with
	// no alias; give it one.
	minDerivedRe = regexp.MustCompile(`(min\(Timestamp\)\s+FROM\s+\(SELECT\b[\s\S]*?LIMIT\s+\d+)\)`)
	// getTraces (traces.go) wraps `array_agg(distinct TraceId) FROM (SELECT
	// TraceId FROM ... ORDER BY Timestamp DESC LIMIT n)` with no alias (after
	// groupArray has been rewritten to array_agg).
	traceIdsDerivedRe = regexp.MustCompile(`(array_agg\(distinct TraceId\)\s+FROM\s+\(SELECT\s+TraceId\s+FROM\b[\s\S]*?LIMIT\s+\d+)\)`)
	toDateTime64Re = regexp.MustCompile(`toDateTime64\(\s*'(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})(?:\.(\d+))?'\s*,\s*\d+\s*\)`)
)

func toDateTime64ToLiteral(m []string) string {
	frac := m[2]
	if len(frac) > 6 {
		frac = frac[:6]
	}
	if frac == "" {
		return "'" + m[1] + "'"
	}
	return "'" + m[1] + "." + frac + "'"
}

func rewriteConstructs(sql string) string {
	sql = toDateTime64Re.ReplaceAllStringFunc(sql, func(x string) string {
		return toDateTime64ToLiteral(toDateTime64Re.FindStringSubmatch(x))
	})
	sql = toStartOfIntervalRe.ReplaceAllString(sql, "from_unixtime(floor(unix_timestamp($1)/$2)*$2)")
	sql = rewriteCall(sql, "multiIf", multiIfToCase)
	sql = rewriteCall(sql, "intDiv", intDivToFloor)
	sql = rewriteCall(sql, "countIf", countIfToCount)
	sql = rewriteCall(sql, "groupArray", groupArrayToArrayAgg)
	sql = rewriteCall(sql, "any", anyToAnyValue)
	sql = rewriteCall(sql, "has", hasToArrayContains)
	sql = rewriteCall(sql, "empty", emptyToArrayLength)
	sql = rewriteCall(sql, "toInt64", toInt64ToCast)
	sql = rewriteCall(sql, "roundDown", roundDownToCase)
	sql = globalInRe.ReplaceAllString(sql, "IN")
	sql = minDerivedRe.ReplaceAllString(sql, "$1) t")
	sql = traceIdsDerivedRe.ReplaceAllString(sql, "$1) t")
	return sql
}

// roundDownToCase turns roundDown(x, [b0,b1,...,bn]) into a CASE ladder that
// snaps x down to the largest boundary <= x — StarRocks has no roundDown.
// The bucket array is a client-bound literal ([...]) by the time we see it, so
// its boundaries are known at translate time (see traces.go HistogramBuckets).
// Emitted highest-first so the first matching WHEN wins; below b0 yields b0.
func roundDownToCase(args []string) (string, bool) {
	if len(args) != 2 {
		return "", false
	}
	expr := args[0]
	bounds := parseArrayLiteral(args[1])
	if len(bounds) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("CASE")
	for i := len(bounds) - 1; i >= 1; i-- {
		fmt.Fprintf(&b, " WHEN (%s) >= %s THEN %s", expr, bounds[i], bounds[i])
	}
	fmt.Fprintf(&b, " ELSE %s END", bounds[0])
	return b.String(), true
}

// parseArrayLiteral splits a `[a, b, c]` literal into its top-level elements.
// Returns nil if s is not bracket-wrapped.
func parseArrayLiteral(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil
	}
	return splitArgs(inner)
}

// toInt64(x) -> cast(x as bigint)
func toInt64ToCast(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	return fmt.Sprintf("cast(%s as bigint)", args[0]), true
}

// countIf(cond) -> count(if(cond,1,null))
func countIfToCount(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	return fmt.Sprintf("count(if(%s,1,null))", args[0]), true
}

// groupArray(distinct x) -> array_agg(distinct x); groupArray(x) -> array_agg(x)
func groupArrayToArrayAgg(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	return fmt.Sprintf("array_agg(%s)", args[0]), true
}

// any(x) -> any_value(x)
func anyToAnyValue(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	return fmt.Sprintf("any_value(%s)", args[0]), true
}

// has(arr, x) -> array_contains(arr, x)
func hasToArrayContains(args []string) (string, bool) {
	if len(args) != 2 {
		return "", false
	}
	return fmt.Sprintf("array_contains(%s, %s)", args[0], args[1]), true
}

// empty(arr) -> array_length(arr) = 0
func emptyToArrayLength(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	return fmt.Sprintf("array_length(%s) = 0", args[0]), true
}

// rewriteCall replaces every fn(...) call in sql (paren-aware, possibly nested)
// by feeding its comma-split, top-level arguments to conv. If conv returns ok
// false the call is left untouched for the tripwire to reject downstream.
func rewriteCall(sql, fn string, conv func(args []string) (string, bool)) string {
	kw := fn + "("
	for from := 0; ; {
		i := strings.Index(sql[from:], kw)
		if i < 0 {
			return sql
		}
		i += from
		open := i + len(kw) - 1
		end := matchParen(sql, open)
		if end < 0 {
			return sql // unbalanced; leave for the tripwire
		}
		repl, ok := conv(splitArgs(sql[open+1 : end]))
		if !ok {
			from = open + 1 // skip past this call, keep scanning
			continue
		}
		sql = sql[:i] + repl + sql[end+1:]
	}
}

// multiIfToCase turns c1,v1,c2,v2,...,else into a CASE expression.
func multiIfToCase(args []string) (string, bool) {
	if len(args) < 3 || len(args)%2 == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("CASE")
	i := 0
	for ; i+1 < len(args); i += 2 {
		fmt.Fprintf(&b, " WHEN %s THEN %s", args[i], args[i+1])
	}
	fmt.Fprintf(&b, " ELSE %s END", args[i])
	return b.String(), true
}

// intDivToFloor turns a, b into floor((a)/(b)).
func intDivToFloor(args []string) (string, bool) {
	if len(args) != 2 {
		return "", false
	}
	return fmt.Sprintf("floor((%s)/(%s))", args[0], args[1]), true
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

// splitArgs splits a call's argument list on top-level commas (ignoring commas
// nested in parentheses or brackets) and trims whitespace from each argument.
// Bracket-awareness keeps array literals like [0, 5, 10] as one argument.
func splitArgs(s string) []string {
	var args []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(args, strings.TrimSpace(s[start:]))
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
