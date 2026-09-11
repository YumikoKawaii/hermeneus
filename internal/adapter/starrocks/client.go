package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/extractor"
)

type Client struct {
	cfg  config.StarRocks
	db   *sql.DB
	http *http.Client
}

func New(cfg config.StarRocks) (*Client, error) {
	c := &Client{cfg: cfg, http: &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
	if cfg.MySQLDSN == "" {
		return c, nil
	}
	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		return nil, err
	}
	c.db = db
	return c, nil
}

func (c *Client) Query(ctx context.Context, sqlText string) (*sql.Rows, error) {
	return c.db.QueryContext(ctx, sqlText)
}

func (c *Client) Write(target string, records []extractor.Record) error {
	if len(records) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch c.cfg.WriteMode {
	case "", "stream_load":
		return c.streamLoad(ctx, target, records)
	case "insert":
		return c.insert(ctx, target, records)
	default:
		return fmt.Errorf("starrocks: unknown write mode %q", c.cfg.WriteMode)
	}
}
