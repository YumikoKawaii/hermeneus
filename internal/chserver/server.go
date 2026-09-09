package chserver

import (
	"context"
	"errors"
	"io"
	"log"
	"net"

	"database/sql"
	"fmt"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/starrocks"
	"github.com/yumikokawaii/hermeneus/internal/system"
	"github.com/yumikokawaii/hermeneus/internal/translate"
)

type Server struct {
	cfg config.Config
	sr  *starrocks.Client
}

func New(cfg config.Config, sr *starrocks.Client) *Server {
	return &Server{cfg: cfg, sr: sr}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go s.handle(ctx, conn)
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	reader := proto.NewReader(conn)
	buf := new(proto.Buffer)

	ver, err := s.handshake(conn, reader, buf)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			log.Printf("handshake %s: %v", conn.RemoteAddr(), err)
		}
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}
		code, err := s.readCode(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("packet %s: %v", conn.RemoteAddr(), err)
			}
			return
		}
		switch code {
		case proto.ClientCodePing:
			buf.Reset()
			proto.ServerCodePong.Encode(buf)
			if err := s.flush(conn, buf); err != nil {
				return
			}
		case proto.ClientCodeQuery:
			if err := s.handleQuery(conn, reader, buf, ver); err != nil {
				log.Printf("query %s: %v", conn.RemoteAddr(), err)
				return
			}
		default:
			if err := s.sendException(conn, buf, ver, code.String()+" not implemented"); err != nil {
				return
			}
		}
	}
}

// handleQuery decodes a client query, drains any trailing empty data block, and
// routes it: system.* probe -> canned block; DDL -> swallow; else -> exception
// (translate/select and insert land in M2/M3).
func (s *Server) handleQuery(conn net.Conn, r *proto.Reader, buf *proto.Buffer, ver int) error {
	var q proto.Query
	if err := q.DecodeAware(r, ver); err != nil {
		return err
	}

	// Coroot's ch-go sends a trailing empty data block after the query body.
	if err := s.drainClientData(r, ver); err != nil {
		return err
	}

	body := q.Body
	if cols, ok := system.Match(body, s.cfg.Server.Database); ok {
		return s.sendResult(conn, buf, ver, cols)
	}
	if system.IsDDL(body) {
		return s.sendResult(conn, buf, ver, nil)
	}

	tr, err := translate.Translate(body)
	if err != nil {
		log.Printf("unrecognised query: %s", body)
		return s.sendException(conn, buf, ver, "hermeneus: unrecognised query")
	}

	rows, err := s.sr.Query(context.Background(), tr.SQL)
	if err != nil {
		log.Printf("starrocks query failed: %v (sql=%s)", err, tr.SQL)
		return s.sendException(conn, buf, ver, "hermeneus: starrocks query failed")
	}
	defer rows.Close()

	cols, err := encodeRows(rows, tr.Shape)
	if err != nil {
		log.Printf("encode rows: %v", err)
		return s.sendException(conn, buf, ver, "hermeneus: result encode failed")
	}
	return s.sendResult(conn, buf, ver, cols)
}

// drainClientData reads the empty ClientData block that follows a query body.
func (s *Server) drainClientData(r *proto.Reader, ver int) error {
	n, err := r.UVarInt()
	if err != nil {
		return err
	}
	if proto.ClientCode(n) != proto.ClientCodeData {
		return errors.New("expected data block after query")
	}
	var data proto.ClientData
	if err := data.DecodeAware(r, ver); err != nil {
		return err
	}
	var block proto.Block
	return block.DecodeBlock(r, ver, nil)
}

// sendResult writes one data block (cols, single row when present; header-only
// when cols is nil) followed by EndOfStream.
func (s *Server) sendResult(conn net.Conn, buf *proto.Buffer, ver int, cols []proto.InputColumn) error {
	rows := 0
	if len(cols) > 0 {
		rows = cols[0].Data.Rows()
	}
	buf.Reset()
	proto.ServerCodeData.Encode(buf)
	if proto.FeatureTempTables.In(ver) {
		buf.PutString("")
	}
	block := proto.Block{Columns: len(cols), Rows: rows}
	if err := block.EncodeBlock(buf, ver, cols); err != nil {
		return err
	}
	proto.ServerCodeEndOfStream.Encode(buf)
	return s.flush(conn, buf)
}

func (s *Server) handshake(conn net.Conn, r *proto.Reader, buf *proto.Buffer) (int, error) {
	code, err := s.readCode(r)
	if err != nil {
		return 0, err
	}
	if code != proto.ClientCodeHello {
		return 0, errors.New("expected hello, got " + code.String())
	}

	var hello proto.ClientHello
	if err := hello.Decode(r); err != nil {
		return 0, err
	}
	ver := hello.ProtocolVersion

	info := proto.ServerHello{
		Name:     s.cfg.Server.ServerName,
		Major:    s.cfg.Server.VersionMajor,
		Minor:    s.cfg.Server.VersionMinor,
		Patch:    s.cfg.Server.VersionPatch,
		Revision: s.cfg.Server.ProtocolVersion,
		Timezone: "UTC",
	}
	buf.Reset()
	info.EncodeAware(buf, ver)
	if err := s.flush(conn, buf); err != nil {
		return 0, err
	}
	log.Printf("handshake %s: client=%q proto=%d", conn.RemoteAddr(), hello.Name, ver)
	return ver, nil
}

func (s *Server) readCode(r *proto.Reader) (proto.ClientCode, error) {
	n, err := r.UVarInt()
	if err != nil {
		return 0, err
	}
	code := proto.ClientCode(n)
	if !code.IsAClientCode() {
		return 0, errors.New("bad client packet type")
	}
	return code, nil
}

func (s *Server) sendException(conn net.Conn, buf *proto.Buffer, ver int, msg string) error {
	buf.Reset()
	proto.ServerCodeException.Encode(buf)
	exc := proto.Exception{
		Code:    0,
		Name:    "DB::Exception",
		Message: msg,
	}
	exc.EncodeAware(buf, ver)
	return s.flush(conn, buf)
}

func (s *Server) flush(conn net.Conn, buf *proto.Buffer) error {
	_, err := conn.Write(buf.Buf)
	buf.Reset()
	return err
}

// chColumn builds an empty ch column for the declared CH type.
func chColumn(chType string) (proto.ColInput, func(v any) error, error) {
	switch chType {
	case "String":
		c := new(proto.ColStr)
		return c, func(v any) error { c.Append(toStr(v)); return nil }, nil
	case "Int64":
		c := new(proto.ColInt64)
		return c, func(v any) error {
			i, err := toInt64(v)
			if err != nil {
				return err
			}
			c.Append(i)
			return nil
		}, nil
	case "UInt64":
		c := new(proto.ColUInt64)
		return c, func(v any) error {
			i, err := toInt64(v)
			if err != nil {
				return err
			}
			c.Append(uint64(i))
			return nil
		}, nil
	case "DateTime":
		c := new(proto.ColDateTime)
		return c, func(v any) error {
			t, err := toTime(v)
			if err != nil {
				return err
			}
			c.Append(t)
			return nil
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported CH type %q", chType)
	}
}

// encodeRows maps StarRocks rows into ch columns per the declared shape.
func encodeRows(rows *sql.Rows, shape translate.ResultShape) ([]proto.InputColumn, error) {
	n := len(shape.Columns)
	cols := make([]proto.InputColumn, n)
	appends := make([]func(v any) error, n)
	for i, col := range shape.Columns {
		data, appendFn, err := chColumn(col.CHType)
		if err != nil {
			return nil, err
		}
		cols[i] = proto.InputColumn{Name: col.Name, Data: data}
		appends[i] = appendFn
	}

	for rows.Next() {
		scan := make([]any, n)
		holders := make([]sql.RawBytes, n)
		for i := range scan {
			scan[i] = &holders[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		for i := range holders {
			if err := appends[i]([]byte(holders[i])); err != nil {
				return nil, err
			}
		}
	}
	return cols, rows.Err()
}

func toStr(v any) string {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func toInt64(v any) (int64, error) {
	var i int64
	_, err := fmt.Sscan(toStr(v), &i)
	return i, err
}

func toTime(v any) (time.Time, error) {
	s := toStr(v)
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02 15:04:05.999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	// StarRocks from_unixtime may return an epoch-like numeric string.
	if i, err := toInt64(v); err == nil {
		return time.Unix(i, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}
