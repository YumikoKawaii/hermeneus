package chserver

import (
	"time"

	"github.com/ClickHouse/ch-go/proto"
)

type colFactory func() proto.Column

func str() proto.Column   { return new(proto.ColStr) }
func lcStr() proto.Column { return new(proto.ColStr).LowCardinality() }
func dt64() proto.Column  { return new(proto.ColDateTime64).WithPrecision(proto.PrecisionNano) }
func lcMap() proto.Column {
	return proto.NewMap[string, string](new(proto.ColStr).LowCardinality(), new(proto.ColStr))
}
func strArr() proto.Column   { return new(proto.ColStr).Array() }
func lcStrArr() proto.Column { return new(proto.ColStr).LowCardinality().Array() }
func dt64Arr() proto.Column {
	return proto.NewArray[time.Time](new(proto.ColDateTime64).WithPrecision(proto.PrecisionNano))
}
func lcMapArr() proto.Column {
	return proto.NewArray[map[string]string](proto.NewMap[string, string](new(proto.ColStr).LowCardinality(), new(proto.ColStr)))
}
func i32() proto.Column { return new(proto.ColInt32) }
func i64() proto.Column { return new(proto.ColInt64) }
func u32() proto.Column { return new(proto.ColUInt32) }
func u64() proto.Column { return new(proto.ColUInt64) }

var insertSchemas = map[string]map[string]colFactory{
	"otel_logs": {
		"Timestamp":          dt64,
		"TraceId":            str,
		"SpanId":             str,
		"TraceFlags":         u32,
		"SeverityText":       lcStr,
		"SeverityNumber":     i32,
		"ServiceName":        lcStr,
		"ResourceAttributes": lcMap,
		"LogAttributes":      lcMap,
		"Body":               str,
	},
	"otel_traces": {
		"Timestamp":          dt64,
		"TraceId":            str,
		"SpanId":             str,
		"ParentSpanId":       str,
		"TraceState":         str,
		"SpanName":           lcStr,
		"SpanKind":           lcStr,
		"ServiceName":        lcStr,
		"ResourceAttributes": lcMap,
		"SpanAttributes":     lcMap,
		"Duration":           i64,
		"StatusCode":         lcStr,
		"StatusMessage":      str,
		"Events.Timestamp":   dt64Arr,
		"Events.Name":        lcStrArr,
		"Events.Attributes":  lcMapArr,
		"Links.TraceId":      strArr,
		"Links.SpanId":       strArr,
		"Links.TraceState":   strArr,
		"Links.Attributes":   lcMapArr,
	},
	"profiling_stacks": {
		"ServiceName": lcStr,
		"Hash":        u64,
		"LastSeen":    dt64,
		"Stack":       strArr,
	},
	"profiling_samples": {
		"ServiceName": lcStr,
		"Type":        lcStr,
		"Start":       dt64,
		"End":         dt64,
		"Labels":      lcMap,
		"StackHash":   u64,
		"Value":       i64,
	},
}
