package translate

import (
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		sql  string
		want Kind
	}{
		{"INSERT INTO otel_logs VALUES", KindInsert},
		{"CREATE TABLE x (a Int)", KindDDL},
		{"  alter table x add column", KindDDL},
		{"SELECT * FROM system.tables", KindSystemProbe},
		{"SELECT currentDatabase()", KindSystemProbe},
		{"SELECT DISTINCT ServiceName FROM otel_logs", KindSelect},
		{"WITH x AS (SELECT 1) SELECT * FROM x", KindSelect},
		{"EXPLAIN SELECT 1", KindUnknown},
	}
	for _, tt := range tests {
		if got := Classify(tt.sql); got != tt.want {
			t.Errorf("Classify(%q) = %v, want %v", tt.sql, got, tt.want)
		}
	}
}

func TestTranslate(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    string
		wantErr bool
	}{
		{
			name: "GetServicesFromLogs",
			sql:  "SELECT DISTINCT ServiceName FROM otel_logs_service_name_severity_text WHERE LastSeen >= '2026-09-10 00:00:00'",
			want: "SELECT DISTINCT ServiceName FROM otel_logs_service_name_severity_text WHERE LastSeen >= '2026-09-10 00:00:00'",
		},
		{
			name: "GetLogsHistogram",
			sql:  "SELECT multiIf(SeverityNumber=0, 0, intDiv(SeverityNumber, 4)+1), toStartOfInterval(Timestamp, INTERVAL 60 second), count(1) FROM otel_logs WHERE ServiceName = 'x' GROUP BY 1, 2",
			want: "SELECT CASE WHEN SeverityNumber=0 THEN 0 ELSE floor((SeverityNumber)/(4))+1 END, from_unixtime(floor(unix_timestamp(Timestamp)/60)*60), count(1) FROM otel_logs WHERE ServiceName = 'x' GROUP BY 1, 2",
		},
		{
			name: "GetServicesFromTraces",
			sql:  "SELECT DISTINCT ServiceName FROM otel_traces_service_name WHERE LastSeen >= '2026-09-10 00:00:00'",
			want: "SELECT DISTINCT ServiceName FROM otel_traces_service_name WHERE LastSeen >= '2026-09-10 00:00:00'",
		},
		{
			name: "GetProfileTypes",
			sql:  "SELECT DISTINCT ServiceName, Type FROM profiling_profiles WHERE LastSeen >= '2026-09-10 00:00:00'",
			want: "SELECT DISTINCT ServiceName, Type FROM profiling_profiles WHERE LastSeen >= '2026-09-10 00:00:00'",
		},
		{
			name: "getTraces count + groupArray distinct",
			sql:  "SELECT count(1), groupArray(distinct TraceId) FROM (SELECT TraceId FROM otel_traces WHERE ServiceName = 'x' ORDER BY Timestamp DESC LIMIT 10)",
			want: "SELECT count(1), array_agg(distinct TraceId) FROM (SELECT TraceId FROM otel_traces WHERE ServiceName = 'x' ORDER BY Timestamp DESC LIMIT 10)",
		},
		{
			name: "qProfile value + stack",
			sql:  "WITH samples AS (SELECT StackHash AS hash, sum(Value) AS value FROM profiling_samples GROUP BY StackHash), stacks AS (SELECT Hash AS hash, any(Stack) AS stack FROM profiling_stacks WHERE Hash GLOBAL IN (SELECT hash FROM samples) GROUP BY Hash) SELECT value, stack FROM stacks JOIN samples USING(hash)",
			want: "WITH samples AS (SELECT StackHash AS hash, sum(Value) AS value FROM profiling_samples GROUP BY StackHash), stacks AS (SELECT Hash AS hash, any_value(Stack) AS stack FROM profiling_stacks WHERE Hash IN (SELECT hash FROM samples) GROUP BY Hash) SELECT value, stack FROM stacks JOIN samples USING(hash)",
		},
		{
			name:    "unknown query trips the wire",
			sql:     "SELECT ServiceName, Timestamp FROM otel_logs LIMIT 5",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Translate(tt.sql)
			if tt.wantErr {
				if !errors.Is(err, ErrUnknownQuery) {
					t.Fatalf("got err %v, want ErrUnknownQuery", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.SQL != tt.want {
				t.Errorf("SQL mismatch\ngot:  %s\nwant: %s", got.SQL, tt.want)
			}
		})
	}
}

func TestMultiIfToCase(t *testing.T) {
	tests := []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{"a=1", "x", "y"}, "CASE WHEN a=1 THEN x ELSE y END", true},
		{[]string{"a=1", "x", "b=2", "y", "z"}, "CASE WHEN a=1 THEN x WHEN b=2 THEN y ELSE z END", true},
		{[]string{"a", "b"}, "", false}, // even count, unsupported
	}
	for _, tt := range tests {
		got, ok := multiIfToCase(tt.args)
		if ok != tt.ok || got != tt.want {
			t.Errorf("multiIfToCase(%v) = (%q,%v), want (%q,%v)", tt.args, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRewriteConstructs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"countIf", "countIf(StatusCode = 'X')", "count(if(StatusCode = 'X',1,null))"},
		{"groupArray distinct", "groupArray(distinct TraceId)", "array_agg(distinct TraceId)"},
		{"any", "any(Stack)", "any_value(Stack)"},
		{"has", "has(@containers, Labels['id'])", "array_contains(@containers, Labels['id'])"},
		{"empty", "empty(@containers)", "array_length(@containers) = 0"},
		{"toInt64", "toInt64(value/profiles)", "cast(value/profiles as bigint)"},
		{"global in", "Hash GLOBAL IN (SELECT hash FROM samples)", "Hash IN (SELECT hash FROM samples)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteConstructs(tt.in); got != tt.want {
				t.Errorf("rewriteConstructs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
