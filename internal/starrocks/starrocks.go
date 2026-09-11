package starrocks

import (
	"context"
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
	"github.com/yumikokawaii/hermeneus/internal/config"
)

type Client struct {
	cfg config.StarRocks
	db  *sql.DB
}

func New(cfg config.StarRocks) (*Client, error) {
	c := &Client{cfg: cfg}
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

