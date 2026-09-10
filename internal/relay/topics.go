package relay

import (
	"encoding/json"
	"fmt"

	"github.com/yumikokawaii/hermeneus/internal/sink"
)

const (
	TableLogs             = "otel_logs"
	TableTraces           = "otel_traces"
	TableProfilingStacks  = "profiling_stacks"
	TableProfilingSamples = "profiling_samples"

	TopicLogs             = "otel.logs"
	TopicTraces           = "otel.traces"
	TopicProfilingStacks  = "profiling.stacks"
	TopicProfilingSamples = "profiling.samples"
)

var tableTopic = map[string]string{
	TableLogs:             TopicLogs,
	TableTraces:           TopicTraces,
	TableProfilingStacks:  TopicProfilingStacks,
	TableProfilingSamples: TopicProfilingSamples,
}

func TopicFor(prefix, table string) (string, bool) {
	t, ok := tableTopic[table]
	return prefix + t, ok
}

type LogMessage struct {
	Timestamp          string            `json:"Timestamp"`
	TraceId            string            `json:"TraceId"`
	SpanId             string            `json:"SpanId"`
	TraceFlags         uint32            `json:"TraceFlags"`
	SeverityText       string            `json:"SeverityText"`
	SeverityNumber     int32             `json:"SeverityNumber"`
	ServiceName        string            `json:"ServiceName"`
	ResourceAttributes map[string]string `json:"ResourceAttributes"`
	LogAttributes      map[string]string `json:"LogAttributes"`
	Body               string            `json:"Body"`
}

type TraceMessage struct {
	Timestamp          string              `json:"Timestamp"`
	TraceId            string              `json:"TraceId"`
	SpanId             string              `json:"SpanId"`
	ParentSpanId       string              `json:"ParentSpanId"`
	TraceState         string              `json:"TraceState"`
	SpanName           string              `json:"SpanName"`
	SpanKind           string              `json:"SpanKind"`
	ServiceName        string              `json:"ServiceName"`
	ResourceAttributes map[string]string   `json:"ResourceAttributes"`
	SpanAttributes     map[string]string   `json:"SpanAttributes"`
	Duration           int64               `json:"Duration"`
	StatusCode         string              `json:"StatusCode"`
	StatusMessage      string              `json:"StatusMessage"`
	EventsTimestamp    []string            `json:"Events.Timestamp"`
	EventsName         []string            `json:"Events.Name"`
	EventsAttributes   []map[string]string `json:"Events.Attributes"`
	LinksTraceId       []string            `json:"Links.TraceId"`
	LinksSpanId        []string            `json:"Links.SpanId"`
	LinksTraceState    []string            `json:"Links.TraceState"`
	LinksAttributes    []map[string]string `json:"Links.Attributes"`
	NetSockPeerAddr    string              `json:"NetSockPeerAddr"`
}

type ProfilingStackMessage struct {
	ServiceName string   `json:"ServiceName"`
	Hash        int64    `json:"Hash"`
	LastSeen    string   `json:"LastSeen"`
	Stack       []string `json:"Stack"`
}

type ProfilingSampleMessage struct {
	ServiceName string            `json:"ServiceName"`
	Type        string            `json:"Type"`
	Start       string            `json:"Start"`
	End         string            `json:"End"`
	Labels      map[string]string `json:"Labels"`
	StackHash   int64             `json:"StackHash"`
	Value       int64             `json:"Value"`
}

func newMessage(table string) (any, error) {
	switch table {
	case TableLogs:
		return new(LogMessage), nil
	case TableTraces:
		return new(TraceMessage), nil
	case TableProfilingStacks:
		return new(ProfilingStackMessage), nil
	case TableProfilingSamples:
		return new(ProfilingSampleMessage), nil
	}
	return nil, fmt.Errorf("relay: no message type for table %q", table)
}

func rowObject(b sink.Batch, i int) map[string]any {
	obj := make(map[string]any, len(b.Columns))
	for j, col := range b.Columns {
		obj[col] = b.Rows[i][j]
	}
	return obj
}

func EncodeRow(b sink.Batch, i int) ([]byte, error) {
	msg, err := newMessage(b.Table)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(rowObject(b, i))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, msg); err != nil {
		return nil, fmt.Errorf("relay: row %d does not fit %s shape: %w", i, b.Table, err)
	}
	return json.Marshal(msg)
}
