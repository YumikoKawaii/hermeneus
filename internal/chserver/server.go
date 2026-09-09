package chserver

import (
	"context"
	"errors"
	"io"
	"log"
	"net"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/starrocks"
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
		default:
			if err := s.sendException(conn, buf, ver, code.String()+" not implemented"); err != nil {
				return
			}
		}
	}
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
