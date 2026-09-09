package starrocks

import (
	"context"
	"database/sql"

	"github.com/yumikokawaii/hermeneus/internal/config"
)

// Client wraps StarRocks' two access paths:
//   - MySQL wire (:9030) for SELECT / DDL       -> database/sql + go-sql-driver
//   - Stream Load (HTTP :8030) for bulk INSERT  -> loadHTTP
type Client struct {
	cfg config.StarRocks
	db  *sql.DB
}

func New(cfg config.StarRocks) (*Client, error) {
	// TODO(M2): sql.Open("mysql", cfg.MySQLDSN), Ping.
	return &Client{cfg: cfg}, nil
}

// Query runs a translated StarRocks SELECT with bound args and returns rows for
// the server to re-encode as a ClickHouse block.
// TODO(M2): implement; map @name placeholders -> positional args.
func (c *Client) Query(ctx context.Context, sqlText string, args map[string]any) (*sql.Rows, error) {
	panic("TODO: Query")
}

// Batch is a decoded set of INSERT rows destined for one StarRocks table.
type Batch struct {
	Table   string
	Columns []string
	Rows    [][]any
}

// StreamLoad ingests a decoded CH INSERT block via StarRocks Stream Load.
// Ack the upstream CH insert only after this returns nil (backpressure).
// TODO(M3): PUT to /api/{db}/{table}/_stream_load, JSON format, check status.
func (c *Client) StreamLoad(ctx context.Context, b Batch) error {
	panic("TODO: StreamLoad")
}
