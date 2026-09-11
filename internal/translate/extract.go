package translate

// extract recognises which Coroot query a parsed SELECT is, returning the
// ResultShape the encoder must use. Recognition is structural (over the AST),
// not string-based. A statement that parses but matches no known shape is the
// upgrade tripwire — ErrUnknownQuery.
//
// Two shapes (log attribute-name / attribute-value lists) use CH arrayJoin over
// a map/array in the SELECT list; StarRocks expresses that as an UNNEST lateral
// join. Those are rewritten on the AST here before the generic builder runs.

func fromTableName(f fromItem) (string, bool) {
	switch v := f.(type) {
	case tableRef:
		return v.name, true
	case joinRef:
		return fromTableName(v.left)
	default:
		return "", false
	}
}

func colIdent(c selectCol) (string, bool) {
	id, ok := c.expr.(identExpr)
	if !ok {
		return "", false
	}
	return id.name, true
}

func colMember(c selectCol) (string, string, bool) {
	m, ok := c.expr.(memberExpr)
	if !ok {
		return "", "", false
	}
	return m.base, m.field, true
}

func colCall(c selectCol) (callExpr, bool) {
	fn, ok := c.expr.(callExpr)
	return fn, ok
}

// recognise returns the ResultShape for a parsed SELECT, applying any AST
// pre-transform the shape requires, or false if no shape matches.
func recognise(s *selectStmt) (ResultShape, bool) {
	table, _ := fromTableName(s.from)

	// GetServicesFromLogs / GetServicesFromTraces: SELECT DISTINCT ServiceName FROM <tbl>
	if s.distinct && len(s.cols) == 1 {
		if name, ok := colIdent(s.cols[0]); ok && name == "ServiceName" {
			switch {
			case hasPrefix(table, "otel_logs_service_name_severity_text"),
				hasPrefix(table, "otel_traces_service_name"):
				return shape1("ServiceName", "String"), true
			}
		}
		// GetProfileTypes: SELECT DISTINCT ServiceName, Type FROM profiling_profiles
	}
	if s.distinct && len(s.cols) == 2 && table == "profiling_profiles" {
		if a, ok := colIdent(s.cols[0]); ok && a == "ServiceName" {
			if b, ok := colIdent(s.cols[1]); ok && b == "Type" {
				return ResultShape{Columns: []Column{
					{Name: "ServiceName", CHType: "String"},
					{Name: "Type", CHType: "String"},
				}}, true
			}
		}
	}

	// log severity DISTINCT multiIf(...) FROM otel_logs
	if s.distinct && len(s.cols) == 1 && table == "otel_logs" {
		if c, ok := colCall(s.cols[0]); ok && c.fn == "multiIf" {
			return shape1("severity", "Int64"), true
		}
		// log attribute-value list: DISTINCT arrayJoin([LogAttributes[..], ResourceAttributes[..]])
		if c, ok := colCall(s.cols[0]); ok && c.fn == "arrayJoin" && len(c.args) == 1 {
			if arr, ok := c.args[0].(arrayExpr); ok {
				unnestLogAttrValues(s, arr)
				return shape1("v", "String"), true
			}
		}
	}

	// log attribute-name list: arrayJoin(arrayConcat(mapKeys(..), mapKeys(..))) AS k
	if !s.distinct && len(s.cols) == 1 && table == "otel_logs" {
		if c, ok := colCall(s.cols[0]); ok && c.fn == "arrayJoin" &&
			s.cols[0].alias == "k" && len(c.args) == 1 {
			if inner, ok := c.args[0].(callExpr); ok && inner.fn == "arrayConcat" {
				unnestLogAttrNames(s, c.args[0])
				return shape1("k", "String"), true
			}
		}
	}

	// GetLogsHistogram: multiIf(...), toStartOfInterval(...), count(1) FROM otel_logs GROUP BY 1,2
	if len(s.cols) == 3 && table == "otel_logs" && !s.distinct {
		if c0, ok := colCall(s.cols[0]); ok && c0.fn == "multiIf" {
			if c1, ok := colCall(s.cols[1]); ok && c1.fn == "toStartOfInterval" {
				return ResultShape{Columns: []Column{
					{Name: "severity", CHType: "Int64"},
					{Name: "ts", CHType: "DateTime"},
					{Name: "count", CHType: "UInt64"},
				}}, true
			}
		}
	}

	// GetLogs: ServiceName, Timestamp, multiIf(...), Body, TraceId, ResourceAttributes, LogAttributes
	if len(s.cols) == 7 && table == "otel_logs" {
		if a, ok := colIdent(s.cols[0]); ok && a == "ServiceName" {
			if _, ok := colCall(s.cols[2]); ok {
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
	if len(s.cols) == 2 && table == "otel_traces_trace_id_ts" {
		if c, ok := colCall(s.cols[0]); ok && c.fn == "min" {
			return ResultShape{Columns: []Column{
				{Name: "min", CHType: "DateTime"},
				{Name: "max", CHType: "DateTime"},
			}}, true
		}
	}

	// getTraces: count(1), groupArray(distinct TraceId) FROM (SELECT TraceId FROM otel_traces ...)
	if len(s.cols) == 2 {
		if c0, ok := colCall(s.cols[0]); ok && c0.fn == "count" {
			if c1, ok := colCall(s.cols[1]); ok && c1.fn == "groupArray" {
				return ResultShape{Columns: []Column{
					{Name: "count", CHType: "UInt64"},
					{Name: "traceIds", CHType: "Array(String)"},
				}}, true
			}
		}
	}

	// querySpans / getTraceSpans: 14-column span shape FROM otel_traces
	if len(s.cols) == 14 && table == "otel_traces" {
		if a, ok := colIdent(s.cols[0]); ok && a == "Timestamp" {
			if _, _, ok := colMember(s.cols[11]); ok {
				return spanShape(), true
			}
		}
	}

	// getSpansHistogram: toStartOfInterval(...), bucket, total, failed (MV or raw)
	if len(s.cols) == 4 {
		if c0, ok := colCall(s.cols[0]); ok && c0.fn == "toStartOfInterval" &&
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
	if len(s.cols) == 5 && (table == "otel_traces_histogram" || table == "otel_traces") {
		if a, ok := colIdent(s.cols[0]); ok && a == "ServiceName" {
			if b, ok := colIdent(s.cols[1]); ok && b == "SpanName" {
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
	if len(s.with) > 0 && s.with[0].name == "samples" {
		switch len(s.cols) {
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
//   SELECT arrayJoin(arrayConcat(mapKeys(A), mapKeys(B))) AS k FROM otel_logs ...
// into
//   SELECT k FROM otel_logs, unnest(<inner>) AS t(k) ...
// by moving the arrayConcat arg into an UNNEST lateral join column named k.
func unnestLogAttrNames(s *selectStmt, inner expr) {
	s.cols = []selectCol{{expr: identExpr{name: "k"}}}
	s.from = joinRef{
		left:  s.from,
		right: unnestRef{arg: inner, colAlias: "k"},
		comma: true,
	}
}

// unnestLogAttrValues rewrites
//   SELECT DISTINCT arrayJoin([A[x], B[y]]) FROM otel_logs ...
// into
//   SELECT DISTINCT k FROM otel_logs, unnest([A[x], B[y]]) AS t(k) ...
func unnestLogAttrValues(s *selectStmt, arr arrayExpr) {
	s.cols = []selectCol{{expr: identExpr{name: "k"}}}
	s.from = joinRef{
		left:  s.from,
		right: unnestRef{arg: arr, colAlias: "k"},
		comma: true,
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
