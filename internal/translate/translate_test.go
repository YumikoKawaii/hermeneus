package translate

import (
	"strings"
	"testing"
)

func TestGetLogs(t *testing.T) {
	q := "SELECT ServiceName, Timestamp, multiIf(SeverityNumber=0, 0, intDiv(SeverityNumber, 4)+1), Body, TraceId, ResourceAttributes, LogAttributes FROM otel_logs WHERE Timestamp BETWEEN toDateTime64('2026-09-10 05:17:15.000000000', 9) AND toDateTime64('2026-09-10 06:17:15.000000000', 9) AND (ServiceName = 'KubernetesEvents') AND (SeverityNumber NOT BETWEEN 9 AND 12) AND Timestamp >= (SELECT min(Timestamp) FROM (SELECT Timestamp FROM otel_logs WHERE Timestamp BETWEEN toDateTime64('2026-09-10 05:17:15.000000000', 9) AND toDateTime64('2026-09-10 06:17:15.000000000', 9) AND (ServiceName = 'KubernetesEvents') AND (SeverityNumber NOT BETWEEN 9 AND 12) ORDER BY Timestamp DESC LIMIT 10000)) ORDER BY Timestamp DESC LIMIT 10000"
	tr, err := Translate(q)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"toDateTime64", "multiIf", "intDiv"} {
		if strings.Contains(tr.SQL, bad) {
			t.Errorf("still contains %s: %s", bad, tr.SQL)
		}
	}
	if !strings.Contains(tr.SQL, "BETWEEN '2026-09-10 05:17:15.000000' AND '2026-09-10 06:17:15.000000'") {
		t.Errorf("bad datetime rewrite: %s", tr.SQL)
	}
	if len(tr.Shape.Columns) != 7 {
		t.Errorf("shape = %d cols", len(tr.Shape.Columns))
	}
	t.Log(tr.SQL)
}
