package starrocks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/yumikokawaii/hermeneus/internal/extractor"
)

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

func (c *Client) streamLoad(ctx context.Context, target string, records []extractor.Record) error {
	columns := records[0].Columns
	keys := make([]string, len(columns))
	paths := make([]string, len(columns))
	cols := make([]string, len(columns))
	for i, col := range columns {
		keys[i] = jsonKey(col)
		paths[i] = `"$.` + keys[i] + `"`
		cols[i] = "`" + col + "`"
	}
	objs := make([]map[string]any, len(records))
	for i, r := range records {
		obj := make(map[string]any, len(keys))
		for j, k := range keys {
			obj[k] = jsonValue(r.Values[j])
		}
		objs[i] = obj
	}
	payload, err := json.Marshal(objs)
	if err != nil {
		return err
	}

	headers := map[string]string{
		"label":             "hermeneus-" + target + "-" + uuid.NewString(),
		"format":            "json",
		"strip_outer_array": "true",
		"jsonpaths":         "[" + strings.Join(paths, ",") + "]",
		"columns":           strings.Join(cols, ","),
		"Expect":            "100-continue",
	}
	url := fmt.Sprintf("http://%s/api/%s/%s/_stream_load", c.cfg.StreamLoadHost, c.cfg.Database, target)
	resp, err := c.put(ctx, url, payload, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stream load %s: http %d: %s", target, resp.StatusCode, body)
	}
	var sr streamLoadResp
	if err := json.Unmarshal(body, &sr); err != nil {
		return fmt.Errorf("stream load %s: bad response: %s", target, body)
	}
	if sr.Status != "Success" && sr.Status != "Publish Timeout" {
		return fmt.Errorf("stream load %s: %s: %s %s", target, sr.Status, sr.Message, sr.ErrorURL)
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
