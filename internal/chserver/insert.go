package chserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/yumikokawaii/hermeneus/internal/sink"
)

var insertRe = regexp.MustCompile("(?is)^\\s*INSERT\\s+INTO\\s+[`\"]?([A-Za-z_][A-Za-z0-9_.]*)[`\"]?\\s*\\(([^)]*)\\)")

const srDateTime = "2006-01-02 15:04:05.000000"

func parseInsert(body string) (table string, cols []string, ok bool) {
	m := insertRe.FindStringSubmatch(body)
	if m == nil {
		return "", nil, false
	}
	table = m[1]
	if i := strings.LastIndex(table, "."); i >= 0 {
		table = table[i+1:]
	}
	for _, c := range strings.Split(m[2], ",") {
		c = strings.Trim(strings.TrimSpace(c), "`\"")
		if c != "" {
			cols = append(cols, c)
		}
	}
	return table, cols, len(cols) > 0
}

func (s *Server) handleInsert(cc *connCtx, body string) error {
	if err := s.drainClientData(cc); err != nil {
		return err
	}
	table, names, ok := parseInsert(body)
	if !ok {
		return s.sendException(cc.conn, cc.buf, cc.ver, "hermeneus: cannot parse INSERT")
	}
	schema, ok := insertSchemas[table]
	if !ok {
		log.Printf("insert into unknown table %q", table)
		return s.sendException(cc.conn, cc.buf, cc.ver, "hermeneus: unknown insert table "+table)
	}
	results := make(proto.Results, len(names))
	header := make([]proto.InputColumn, len(names))
	for i, n := range names {
		f, ok := schema[n]
		if !ok {
			log.Printf("insert into %s: unknown column %q", table, n)
			return s.sendException(cc.conn, cc.buf, cc.ver, "hermeneus: unknown column "+n)
		}
		col := f()
		results[i] = proto.ResultColumn{Name: n, Data: col}
		header[i] = proto.InputColumn{Name: n, Data: col}
	}

	if err := s.writeBlock(cc, header, false); err != nil {
		return err
	}

	batch := sink.Batch{Table: table, Columns: names}
	peerIdx := -1
	if table == "otel_traces" {
		for i, n := range names {
			if n == "SpanAttributes" {
				peerIdx = i
			}
		}
		if peerIdx >= 0 {
			batch.Columns = append(batch.Columns, "NetSockPeerAddr")
		}
	}
	for {
		code, err := s.readCode(cc.r)
		if err != nil {
			return err
		}
		if code == proto.ClientCodeCancel {
			return s.sendEndOfStream(cc)
		}
		if code != proto.ClientCodeData {
			return fmt.Errorf("insert: expected data packet, got %s", code)
		}
		var data proto.ClientData
		if err := data.DecodeAware(cc.r, cc.ver); err != nil {
			return err
		}
		block, err := s.decodeBlock(cc, results)
		if err != nil {
			return err
		}
		if block.End() {
			break
		}
		if err := appendBlock(&batch, results, peerIdx); err != nil {
			return err
		}
	}

	if err := s.sink.Write(context.Background(), batch); err != nil {
		log.Printf("insert %s (%d rows): sink failed: %v", table, len(batch.Rows), err)
		return s.sendException(cc.conn, cc.buf, cc.ver, "hermeneus: insert sink failed: "+err.Error())
	}
	return s.sendEndOfStream(cc)
}

func (s *Server) decodeBlock(cc *connCtx, target proto.Result) (proto.Block, error) {
	if cc.compressed {
		cc.r.EnableCompression()
		defer cc.r.DisableCompression()
	}
	var block proto.Block
	err := block.DecodeBlock(cc.r, cc.ver, target)
	return block, err
}

func appendBlock(b *sink.Batch, cols proto.Results, peerIdx int) error {
	if len(cols) == 0 {
		return errors.New("empty block")
	}
	rows := cols[0].Data.Rows()
	for i := 0; i < rows; i++ {
		row := make([]any, len(cols), len(b.Columns))
		for j, c := range cols {
			v, err := cellValue(c.Data, i)
			if err != nil {
				return fmt.Errorf("column %s: %w", c.Name, err)
			}
			row[j] = v
		}
		if peerIdx >= 0 {
			addr := ""
			if m, ok := row[peerIdx].(map[string]string); ok {
				addr = m["net.sock.peer.addr"]
			}
			row = append(row, addr)
		}
		b.Rows = append(b.Rows, row)
	}
	return nil
}

func cellValue(col proto.ColResult, i int) (any, error) {
	switch c := col.(type) {
	case *proto.ColStr:
		return c.Row(i), nil
	case *proto.ColLowCardinality[string]:
		return c.Row(i), nil
	case *proto.ColInt32:
		return c.Row(i), nil
	case *proto.ColInt64:
		return c.Row(i), nil
	case *proto.ColUInt32:
		return c.Row(i), nil
	case *proto.ColUInt64:
		return int64(c.Row(i)), nil
	case *proto.ColDateTime:
		return c.Row(i).UTC().Format(srDateTime), nil
	case *proto.ColDateTime64:
		return c.Row(i).UTC().Format(srDateTime), nil
	case *proto.ColMap[string, string]:
		return c.Row(i), nil
	case *proto.ColArr[string]:
		return c.Row(i), nil
	case *proto.ColArr[time.Time]:
		ts := c.Row(i)
		out := make([]string, len(ts))
		for k, t := range ts {
			out[k] = t.UTC().Format(srDateTime)
		}
		return out, nil
	case *proto.ColArr[map[string]string]:
		return c.Row(i), nil
	default:
		return nil, fmt.Errorf("unsupported column type %T", col)
	}
}
