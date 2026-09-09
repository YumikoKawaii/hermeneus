package chserver

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/starrocks"
)

// fakeSR is a StarRocks backend backed by sqlmock. It records the last SQL the
// server handed it and returns a canned (empty) result so the ch-go client can
// decode a well-formed block. This soaks handshake + classify + translate +
// encode + wire against a real ch-go client, no external infra.
type fakeSR struct {
	db   *sql.DB
	mock sqlmock.Sqlmock

	mu      sync.Mutex
	lastSQL string
}

func newFakeSR(t *testing.T) *fakeSR {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	mock.MatchExpectationsInOrder(false)
	return &fakeSR{db: db, mock: mock}
}

func (f *fakeSR) Query(ctx context.Context, sqlText string) (*sql.Rows, error) {
	f.mu.Lock()
	f.lastSQL = sqlText
	f.mu.Unlock()
	// Return an empty result for any SQL; the server encodes 0 rows and the
	// client decodes cleanly. Column identity does not affect classify/translate.
	f.mock.ExpectQuery(".*").WillReturnRows(sqlmock.NewRows([]string{"c"}))
	return f.db.QueryContext(ctx, sqlText)
}

func (f *fakeSR) StreamLoad(ctx context.Context, b starrocks.Batch) error { return nil }

func (f *fakeSR) recorded() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSQL
}

// startServer boots a real chserver on an ephemeral port and returns its addr.
func startServer(t *testing.T, sr StarRocks) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	cfg := config.Default()
	cfg.ListenAddr = ln.Addr().String()
	ln.Close() // ListenAndServe reopens; we only needed a free port

	srv := New(cfg, sr)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.ListenAndServe(ctx)

	waitReady(t, cfg.ListenAddr)
	return cfg.ListenAddr
}

func waitReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s never came up", addr)
}

func dial(t *testing.T, addr string) *ch.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := ch.Dial(ctx, ch.Options{Address: addr})
	if err != nil {
		t.Fatalf("ch.Dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestSoakKnownQueries drives the real Coroot query strings through a real
// ch-go client and asserts each reaches the backend as translated StarRocks SQL.
func TestSoakKnownQueries(t *testing.T) {
	sr := newFakeSR(t)
	addr := startServer(t, sr)
	client := dial(t, addr)

	tests := []struct {
		name       string
		body       string
		wantInSQL  string // substring that proves translation ran
	}{
		{
			name:      "GetServicesFromTraces",
			body:      "SELECT DISTINCT ServiceName FROM otel_traces_service_name WHERE LastSeen >= '2026-09-10 00:00:00'",
			wantInSQL: "SELECT DISTINCT ServiceName FROM otel_traces_service_name",
		},
		{
			name:      "GetLogsHistogram translates multiIf+toStartOfInterval",
			body:      "SELECT multiIf(SeverityNumber=0, 0, intDiv(SeverityNumber, 4)+1), toStartOfInterval(Timestamp, INTERVAL 60 second), count(1) FROM otel_logs WHERE ServiceName = 'x' GROUP BY 1, 2",
			wantInSQL: "from_unixtime(floor(unix_timestamp(Timestamp)/60)*60)",
		},
		{
			name:      "getSpansHistogram raw branch translates roundDown",
			body:      "SELECT toStartOfInterval(Timestamp, INTERVAL 60 second), roundDown(Duration/1000000, [0, 5, 10]), count(1), countIf(StatusCode = 'STATUS_CODE_ERROR') FROM otel_traces WHERE ServiceName = 'x' GROUP BY 1, 2",
			wantInSQL: "CASE WHEN (Duration/1000000) >= 10 THEN 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var res proto.Results
			if err := client.Do(ctx, ch.Query{Body: tt.body, Result: res.Auto()}); err != nil {
				t.Fatalf("Do: %v", err)
			}
			if got := sr.recorded(); !strings.Contains(got, tt.wantInSQL) {
				t.Errorf("backend SQL missing %q\ngot: %s", tt.wantInSQL, got)
			}
		})
	}
}

// TestSoakTripwire asserts an unregistered query fails LOUD (CH exception),
// never silently — the upgrade tripwire (docs/DESIGN.md §3).
func TestSoakTripwire(t *testing.T) {
	sr := newFakeSR(t)
	addr := startServer(t, sr)
	client := dial(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.Do(ctx, ch.Query{Body: "SELECT something_unknown FROM mystery_table LIMIT 5"})
	if err == nil {
		t.Fatal("expected an exception for an unregistered query, got nil")
	}
	if !strings.Contains(err.Error(), "unrecognised") {
		t.Errorf("expected 'unrecognised' in error, got: %v", err)
	}
}

// TestSoakPing asserts the ch-go client's health-check ping round-trips.
func TestSoakPing(t *testing.T) {
	sr := newFakeSR(t)
	addr := startServer(t, sr)
	client := dial(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
