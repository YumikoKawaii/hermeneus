package chserver

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"net"

	"github.com/ClickHouse/ch-go/compress"
	"github.com/ClickHouse/ch-go/proto"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/system"
	"github.com/yumikokawaii/hermeneus/internal/translate"
)

type Querier interface {
	Query(ctx context.Context, sqlText string) (*sql.Rows, error)
}

type Server struct {
	cfg config.Config
	sr  Querier
}

func New(cfg config.Config, sr Querier) *Server {
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

// connCtx holds the per-connection protocol state: the wire reader/writer, the
// negotiated protocol version, and whether the client negotiated LZ4 block
// compression (learned from each Query packet's compression field). The
// compressor is per-connection because it carries internal scratch buffers.
type connCtx struct {
	conn       net.Conn
	r          *proto.Reader
	buf        *proto.Buffer
	ver        int
	compressed bool
	compressor *compress.Writer
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	cc := &connCtx{
		conn:       conn,
		r:          proto.NewReader(conn),
		buf:        new(proto.Buffer),
		compressor: compress.NewWriter(),
	}

	ver, err := s.handshake(conn, cc.r, cc.buf)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			log.Printf("handshake %s: %v", conn.RemoteAddr(), err)
		}
		return
	}
	cc.ver = ver

	for {
		if ctx.Err() != nil {
			return
		}
		code, err := s.readCode(cc.r)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("packet %s: %v", conn.RemoteAddr(), err)
			}
			return
		}
		switch code {
		case proto.ClientCodePing:
			cc.buf.Reset()
			proto.ServerCodePong.Encode(cc.buf)
			if err := s.flush(conn, cc.buf); err != nil {
				return
			}
		case proto.ClientCodeQuery:
			if err := s.handleQuery(cc); err != nil {
				log.Printf("query %s: %v", conn.RemoteAddr(), err)
				return
			}
		default:
			if err := s.sendException(conn, cc.buf, ver, code.String()+" not implemented"); err != nil {
				return
			}
		}
	}
}

// handleQuery decodes a client query, drains any trailing empty data block, and
// routes it: system.* probe -> canned block; DDL -> swallow; known SELECT ->
// translate + StarRocks query -> result block; INSERT -> decode blocks +
// Stream Load; anything else -> exception (the tripwire).
func (s *Server) handleQuery(cc *connCtx) error {
	var q proto.Query
	if err := q.DecodeAware(cc.r, cc.ver); err != nil {
		return err
	}
	// The Query packet's compression field decides BOTH directions for this
	// query: the client sends its data blocks compressed and expects result
	// blocks compressed. (ClickHouse TCPHandler keys state.compression off this.)
	cc.compressed = q.Compression == proto.CompressionEnabled

	body := q.Body
	if translate.Classify(body) == translate.KindInsert {
		return s.handleInsert(cc, body)
	}

	// ch-go sends a trailing (empty) data block after the query body.
	if err := s.drainClientData(cc); err != nil {
		return err
	}

	if cols, ok := system.Match(body, s.cfg.Server.Database); ok {
		return s.sendResult(cc, cols)
	}
	if system.IsDDL(body) {
		return s.sendResult(cc, nil)
	}

	tr, err := translate.Translate(body)
	if err != nil {
		log.Printf("unrecognised query: %s", body)
		return s.sendException(cc.conn, cc.buf, cc.ver, "hermeneus: unrecognised query")
	}

	rows, err := s.sr.Query(context.Background(), tr.SQL)
	if err != nil {
		log.Printf("starrocks query failed: %v (sql=%s)", err, tr.SQL)
		return s.sendException(cc.conn, cc.buf, cc.ver, "hermeneus: starrocks query failed")
	}
	defer rows.Close()

	cols, err := encodeRows(rows, tr.Shape)
	if err != nil {
		log.Printf("encode rows: %v", err)
		return s.sendException(cc.conn, cc.buf, cc.ver, "hermeneus: result encode failed")
	}
	return s.sendResult(cc, cols)
}

// drainClientData reads the (empty) ClientData block that follows a query body.
// The block payload is LZ4-compressed when the client negotiated compression;
// the code and table name that precede it are always raw.
func (s *Server) drainClientData(cc *connCtx) error {
	n, err := cc.r.UVarInt()
	if err != nil {
		return err
	}
	if proto.ClientCode(n) != proto.ClientCodeData {
		return errors.New("expected data block after query")
	}
	var data proto.ClientData
	if err := data.DecodeAware(cc.r, cc.ver); err != nil {
		return err
	}
	if cc.compressed {
		cc.r.EnableCompression()
		defer cc.r.DisableCompression()
	}
	var block proto.Block
	return block.DecodeBlock(cc.r, cc.ver, nil)
}

func (s *Server) sendResult(cc *connCtx, cols []proto.InputColumn) error {
	return s.writeBlock(cc, cols, true)
}

func (s *Server) writeBlock(cc *connCtx, cols []proto.InputColumn, endOfStream bool) error {
	rows := 0
	if len(cols) > 0 {
		rows = cols[0].Data.Rows()
	}
	buf := cc.buf
	buf.Reset()
	proto.ServerCodeData.Encode(buf)
	if proto.FeatureTempTables.In(cc.ver) {
		buf.PutString("")
	}

	start := len(buf.Buf)
	block := proto.Block{Columns: len(cols), Rows: rows}
	if err := block.EncodeBlock(buf, cc.ver, cols); err != nil {
		return err
	}
	if cc.compressed {
		if err := cc.compressor.Compress(compress.LZ4, buf.Buf[start:]); err != nil {
			return err
		}
		buf.Buf = append(buf.Buf[:start], cc.compressor.Data...)
	}

	if endOfStream {
		proto.ServerCodeEndOfStream.Encode(buf)
	}
	return s.flush(cc.conn, buf)
}

func (s *Server) sendEndOfStream(cc *connCtx) error {
	cc.buf.Reset()
	proto.ServerCodeEndOfStream.Encode(cc.buf)
	return s.flush(cc.conn, cc.buf)
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

	// ch-go sends an addendum right after ServerHello: a quota-key string when
	// the negotiated protocol version supports it (FeatureQuotaKey). Consume it,
	// else its bytes are misread as the next packet code.
	if proto.FeatureQuotaKey.In(ver) {
		if _, err := r.Str(); err != nil {
			return 0, err
		}
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
