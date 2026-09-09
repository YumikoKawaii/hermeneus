package starrocks

import (
	"context"
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
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
	if cfg.MySQLDSN == "" {
		return &Client{cfg: cfg}, nil
	}
	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, db: db}, nil
}

// Query runs a translated StarRocks SELECT and returns rows for the server to
// re-encode as a ClickHouse block. Coroot binds args client-side, so sqlText is
// already concrete — no placeholder binding needed here.
func (c *Client) Query(ctx context.Context, sqlText string) (*sql.Rows, error) {
	return c.db.QueryContext(ctx, sqlText)
}

// Batch is a decoded set of INSERT rows destined for one StarRocks table.
type Batch struct {
	Table   string
	Columns []string
	Rows    [][]any
}

// streamLoadResp is the JSON body StarRocks returns from a Stream Load.
type streamLoadResp struct {
	Status  string `json:"Status"`
	Message string `json:"Message"`
}

// StreamLoad ingests a decoded CH INSERT block via StarRocks Stream Load.
// Ack the upstream CH insert only after this returns nil (backpressure).
// Rows are sent as a JSON array (format=json, strip_outer_array) PUT to
// /api/{db}/{table}/_stream_load; StarRocks 307-redirects to a BE, which
// net/http follows, re-sending body and headers.
// TODO: recheck
func (c *Client) StreamLoad(ctx context.Context, b Batch) error {
	return nil
	//if len(b.Rows) == 0 {
	//	return nil
	//}
	//
	//objs := make([]map[string]any, len(b.Rows))
	//for i, row := range b.Rows {
	//	obj := make(map[string]any, len(b.Columns))
	//	for j, col := range b.Columns {
	//		obj[col] = row[j]
	//	}
	//	objs[i] = obj
	//}
	//payload, err := json.Marshal(objs)
	//if err != nil {
	//	return err
	//}
	//
	//url := fmt.Sprintf("http://%s/api/%s/%s/_stream_load", c.cfg.StreamLoadHost, c.cfg.Database, b.Table)
	//req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	//if err != nil {
	//	return err
	//}
	//req.SetBasicAuth(c.cfg.StreamLoadUser, c.cfg.StreamLoadPass)
	//req.Header.Set("format", "json")
	//req.Header.Set("strip_outer_array", "true")
	//req.Header.Set("Expect", "100-continue")
	//
	//resp, err := http.DefaultClient.Do(req)
	//if err != nil {
	//	return err
	//}
	//defer resp.Body.Close()
	//body, _ := io.ReadAll(resp.Body)
	//if resp.StatusCode != http.StatusOK {
	//	return fmt.Errorf("stream load %s: http %d: %s", b.Table, resp.StatusCode, body)
	//}
	//var sr streamLoadResp
	//if err := json.Unmarshal(body, &sr); err != nil {
	//	return fmt.Errorf("stream load %s: bad response: %s", b.Table, body)
	//}
	//if sr.Status != "Success" && sr.Status != "Publish Timeout" {
	//	return fmt.Errorf("stream load %s: %s: %s", b.Table, sr.Status, sr.Message)
	//}
	//return nil
}
