package chserver

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/ClickHouse/ch-go/proto"
)

var insertRe = regexp.MustCompile("(?is)^\\s*INSERT\\s+INTO\\s+[`\"]?([A-Za-z_][A-Za-z0-9_.]*)[`\"]?\\s*\\(([^)]*)\\)")

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

	// Sink wiring is removed for now: decode the incoming blocks to keep the
	// wire in sync, then discard them and ack. Ingestion is silently a no-op
	// until a sink is wired back in.
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
