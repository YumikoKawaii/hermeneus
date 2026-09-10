package starrocks

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/sink"
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

type streamLoadResp struct {
	Status         string `json:"Status"`
	Message        string `json:"Message"`
	NumberLoaded   int64  `json:"NumberLoadedRows"`
	NumberFiltered int64  `json:"NumberFilteredRows"`
	ErrorURL       string `json:"ErrorURL"`
}

func jsonKey(col string) string {
	return strings.NewReplacer(".", "_", "`", "").Replace(col)
}

func (c *Client) Write(ctx context.Context, b sink.Batch) error {
	if len(b.Rows) == 0 {
		return nil
	}
	keys := make([]string, len(b.Columns))
	paths := make([]string, len(b.Columns))
	cols := make([]string, len(b.Columns))
	for i, col := range b.Columns {
		keys[i] = jsonKey(col)
		paths[i] = `"$.` + keys[i] + `"`
		cols[i] = "`" + col + "`"
	}
	objs := make([]map[string]any, len(b.Rows))
	for i, row := range b.Rows {
		obj := make(map[string]any, len(keys))
		for j, k := range keys {
			obj[k] = row[j]
		}
		objs[i] = obj
	}
	payload, err := json.Marshal(objs)
	if err != nil {
		return err
	}

	headers := map[string]string{
		"label":             "hermeneus-" + b.Table + "-" + uuid.NewString(),
		"format":            "json",
		"strip_outer_array": "true",
		"jsonpaths":         "[" + strings.Join(paths, ",") + "]",
		"columns":           strings.Join(cols, ","),
		"Expect":            "100-continue",
	}
	url := fmt.Sprintf("http://%s/api/%s/%s/_stream_load", c.cfg.StreamLoadHost, c.cfg.Database, b.Table)
	resp, err := c.put(ctx, url, payload, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stream load %s: http %d: %s", b.Table, resp.StatusCode, body)
	}
	var sr streamLoadResp
	if err := json.Unmarshal(body, &sr); err != nil {
		return fmt.Errorf("stream load %s: bad response: %s", b.Table, body)
	}
	if sr.Status != "Success" && sr.Status != "Publish Timeout" {
		return fmt.Errorf("stream load %s: %s: %s %s", b.Table, sr.Status, sr.Message, sr.ErrorURL)
	}
	return nil
}

func (c *Client) put(ctx context.Context, url string, payload []byte, headers map[string]string) (*http.Response, error) {
	for hop := 0; hop < 10; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(c.cfg.StreamLoadUser, c.cfg.StreamLoadPass)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTemporaryRedirect && resp.StatusCode != http.StatusPermanentRedirect {
			return resp, nil
		}
		loc, err := resp.Location()
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("stream load: redirect without location: %w", err)
		}
		url = loc.String()
	}
	return nil, errors.New("stream load: too many redirects")
}
