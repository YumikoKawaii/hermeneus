package translate

import (
	"github.com/yumikokawaii/hermeneus/internal/reader"
	"github.com/yumikokawaii/hermeneus/internal/writer"
)

// extract recognises which Coroot query a parsed SELECT is, returning the
// ResultShape the encoder must use. Recognition is structural (over the AST),
// not string-based. A statement that parses but matches no known shape is the
// upgrade tripwire — ErrUnknownQuery.
//
// Two shapes (log attribute-name / attribute-value lists) use CH arrayJoin over
// a map/array in the SELECT list; StarRocks expresses that as an UNNEST lateral
// join. Those are rewritten on the AST here before the generic builder runs.

func fromTableName(f reader.FromItem) (string, bool) {
	switch v := f.(type) {
	case reader.TableRef:
		return v.Name, true
	case reader.JoinRef:
		return fromTableName(v.Left)
	default:
		return "", false
	}
}

func colIdent(c reader.SelectCol) (string, bool) {
	id, ok := c.Expr.(reader.IdentExpr)
	if !ok {
		return "", false
	}
	return id.Name, true
}

func colMember(c reader.SelectCol) (string, string, bool) {
	m, ok := c.Expr.(reader.MemberExpr)
	if !ok {
		return "", "", false
	}
	return m.Base, m.Field, true
}

func colCall(c reader.SelectCol) (reader.CallExpr, bool) {
	fn, ok := c.Expr.(reader.CallExpr)
	return fn, ok
}

// recognise returns the ResultShape for a parsed SELECT, applying any AST
// pre-transform the shape requires, or false if no shape matches.
func recognise(s *reader.SelectStmt) (ResultShape, bool) {
	table, _ := fromTableName(s.From)

	// GetServicesFromLogs / GetServicesFromTraces: SELECT DISTINCT ServiceName FROM <tbl>
	if s.Distinct && len(s.Cols) == 1 {
		if name, ok := colIdent(s.Cols[0]); ok && name == "ServiceName" {
			switch {
			case hasPrefix(table, "otel_logs_service_name_severity_text"),
				hasPrefix(table, "otel_traces_service_name"):
				return shape1("ServiceName", "String"), true
			}
		}
		// GetProfileTypes: SELECT DISTINCT ServiceName, Type FROM profiling_profiles
	}
	if s.Distinct && len(s.Cols) == 2 && table == "profiling_profiles" {
		if a, ok := colIdent(s.Cols[0]); ok && a == "ServiceName" {
			if b, ok := colIdent(s.Cols[1]); ok && b == "Type" {
				return ResultShape{Columns: []Column{
					{Name: "ServiceName", CHType: "String"},
					{Name: "Type", CHType: "String"},
				}}, true
			}
		}
	}

	// log severity DISTINCT multiIf(...) FROM otel_logs
	if s.Distinct && len(s.Cols) == 1 && table == "otel_logs" {
		if c, ok := colCall(s.Cols[0]); ok && c.Fn == "multiIf" {
			return shape1("severity", "Int64"), true
		}
		// log attribute-value list: DISTINCT arrayJoin([LogAttributes[..], ResourceAttributes[..]])
		if c, ok := colCall(s.Cols[0]); ok && c.Fn == "arrayJoin" && len(c.Args) == 1 {
			if arr, ok := c.Args[0].(reader.ArrayExpr); ok {
				unnestLogAttrValues(s, arr)
				return shape1("v", "String"), true
			}
		}
	}

	// log attribute-name list: arrayJoin(arrayConcat(mapKeys(..), mapKeys(..))) AS k
	if !s.Distinct && len(s.Cols) == 1 && table == "otel_logs" {
		if c, ok := colCall(s.Cols[0]); ok && c.Fn == "arrayJoin" &&
			s.Cols[0].Alias == "k" && len(c.Args) == 1 {
			if inner, ok := c.Args[0].(reader.CallExpr); ok && inner.Fn == "arrayConcat" {
				unnestLogAttrNames(s, c.Args[0])
				return shape1("k", "String"), true
			}
		}
	}

	// GetLogsHistogram: multiIf(...), toStartOfInterval(...), count(1) FROM otel_logs GROUP BY 1,2
	if len(s.Cols) == 3 && table == "otel_logs" && !s.Distinct {
		if c0, ok := colCall(s.Cols[0]); ok && c0.Fn == "multiIf" {
			if c1, ok := colCall(s.Cols[1]); ok && c1.Fn == "toStartOfInterval" {
				return ResultShape{Columns: []Column{
					{Name: "severity", CHType: "Int64"},
					{Name: "ts", CHType: "DateTime"},
					{Name: "count", CHType: "UInt64"},
				}}, true
			}
		}
	}

	// GetLogs: ServiceName, Timestamp, multiIf(...), Body, TraceId, ResourceAttributes, LogAttributes
	if len(s.Cols) == 7 && table == "otel_logs" {
		if a, ok := colIdent(s.Cols[0]); ok && a == "ServiceName" {
			if _, ok := colCall(s.Cols[2]); ok {
				return ResultShape{Columns: []Column{
					{Name: "ServiceName", CHType: "String"},
					{Name: "Timestamp", CHType: "DateTime64(9)"},
					{Name: "severity", CHType: "Int64"},
					{Name: "Body", CHType: "String"},
					{Name: "TraceId", CHType: "String"},
					{Name: "ResourceAttributes", CHType: "Map(String,String)"},
					{Name: "LogAttributes", CHType: "Map(String,String)"},
				}}, true
			}
		}
	}

	// trace-id ts window: min(Start), max(End)+1 FROM otel_traces_trace_id_ts
	if len(s.Cols) == 2 && table == "otel_traces_trace_id_ts" {
		if c, ok := colCall(s.Cols[0]); ok && c.Fn == "min" {
			return ResultShape{Columns: []Column{
				{Name: "min", CHType: "DateTime"},
				{Name: "max", CHType: "DateTime"},
			}}, true
		}
	}

	// getTraces: count(1), groupArray(distinct TraceId) FROM (SELECT TraceId FROM otel_traces ...)
	if len(s.Cols) == 2 {
		if c0, ok := colCall(s.Cols[0]); ok && c0.Fn == "count" {
			if c1, ok := colCall(s.Cols[1]); ok && c1.Fn == "groupArray" {
				return ResultShape{Columns: []Column{
					{Name: "count", CHType: "UInt64"},
					{Name: "traceIds", CHType: "Array(String)"},
				}}, true
			}
		}
	}

	// querySpans / getTraceSpans: 14-column span shape FROM otel_traces
	if len(s.Cols) == 14 && table == "otel_traces" {
		if a, ok := colIdent(s.Cols[0]); ok && a == "Timestamp" {
			if _, _, ok := colMember(s.Cols[11]); ok {
				return spanShape(), true
			}
		}
	}

	// getSpansHistogram: toStartOfInterval(...), bucket, total, failed (MV or raw)
	if len(s.Cols) == 4 {
		if c0, ok := colCall(s.Cols[0]); ok && c0.Fn == "toStartOfInterval" &&
			(table == "otel_traces_histogram" || table == "otel_traces") {
			return ResultShape{Columns: []Column{
				{Name: "ts", CHType: "DateTime"},
				{Name: "bucket", CHType: "Float64"},
				{Name: "total", CHType: "UInt64"},
				{Name: "failed", CHType: "UInt64"},
			}}, true
		}
	}

	// getTraceSpanStats: ServiceName, SpanName, bucket, total, failed (MV or raw)
	if len(s.Cols) == 5 && (table == "otel_traces_histogram" || table == "otel_traces") {
		if a, ok := colIdent(s.Cols[0]); ok && a == "ServiceName" {
			if b, ok := colIdent(s.Cols[1]); ok && b == "SpanName" {
				return ResultShape{Columns: []Column{
					{Name: "ServiceName", CHType: "String"},
					{Name: "SpanName", CHType: "String"},
					{Name: "bucket", CHType: "Float64"},
					{Name: "total", CHType: "UInt64"},
					{Name: "failed", CHType: "UInt64"},
				}}, true
			}
		}
	}

	// getProfile / avg / diff: WITH samples AS (...) ... JOIN samples USING(hash)
	if len(s.With) > 0 && s.With[0].Name == "samples" {
		switch len(s.Cols) {
		case 2:
			// value/stack (getProfile) or toInt64(value/profiles)/stack (avg)
			return ResultShape{Columns: []Column{
				{Name: "value", CHType: "Int64"},
				{Name: "stack", CHType: "Array(String)"},
			}}, true
		case 3:
			// base, comp, stack (diff)
			return ResultShape{Columns: []Column{
				{Name: "base", CHType: "Int64"},
				{Name: "comp", CHType: "Int64"},
				{Name: "stack", CHType: "Array(String)"},
			}}, true
		}
	}

	return ResultShape{}, false
}

func shape1(name, ch string) ResultShape {
	return ResultShape{Columns: []Column{{Name: name, CHType: ch}}}
}

func spanShape() ResultShape {
	return ResultShape{Columns: []Column{
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
	}}
}

// unnestLogAttrNames rewrites
//
//	SELECT arrayJoin(arrayConcat(mapKeys(A), mapKeys(B))) AS k FROM otel_logs ...
//
// into
//
//	SELECT k FROM otel_logs, unnest(<inner>) AS t(k) ...
//
// by moving the arrayConcat arg into an UNNEST lateral join column named k.
func unnestLogAttrNames(s *reader.SelectStmt, inner reader.Expr) {
	s.Cols = []reader.SelectCol{{Expr: reader.IdentExpr{Name: "k"}}}
	s.From = reader.JoinRef{
		Left:  s.From,
		Right: writer.UnnestRef{Arg: inner, ColAlias: "k"},
		Comma: true,
	}
}

// unnestLogAttrValues rewrites
//
//	SELECT DISTINCT arrayJoin([A[x], B[y]]) FROM otel_logs ...
//
// into
//
//	SELECT DISTINCT k FROM otel_logs, unnest([A[x], B[y]]) AS t(k) ...
func unnestLogAttrValues(s *reader.SelectStmt, arr reader.ArrayExpr) {
	s.Cols = []reader.SelectCol{{Expr: reader.IdentExpr{Name: "k"}}}
	s.From = reader.JoinRef{
		Left:  s.From,
		Right: writer.UnnestRef{Arg: arr, ColAlias: "k"},
		Comma: true,
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
