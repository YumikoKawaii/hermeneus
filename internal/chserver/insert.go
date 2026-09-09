package chserver

import (
	"fmt"
	"regexp"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/yumikokawaii/hermeneus/internal/starrocks"
)

// insertTableRe pulls the target table out of an INSERT statement. Coroot emits
// `INSERT INTO otel_logs (...) VALUES` / `... FORMAT Native`; the column list and
// tail are ignored — column identity comes from the streamed block header.
var insertTableRe = regexp.MustCompile(`(?is)^\s*INSERT\s+INTO\s+` + "`?" + `([A-Za-z_][A-Za-z0-9_.]*)` + "`?")

// handleInsert consumes the ClientData blocks that follow an INSERT query,
// decodes them into a Batch, ships the Batch via StarRocks Stream Load, and only
// then acks with an empty result. Stream Load failure surfaces as a CH exception
// so Coroot retries (backpressure, docs/DESIGN.md §5).
//
// TODO(M3): incomplete — still to do:
//   - bound in-flight batch size / flush by rows|bytes instead of buffering the
//     whole insert stream in memory
//   - nested/typed columns Coroot actually streams (Map, Array, DateTime64,
//     Nested Events.*) verified against real block headers, not assumed
//   - per-table column→StarRocks mapping; today table name + column names are
//     passed through verbatim
//   - LZ4-compressed block path
//   - dedicated Stream Load label for idempotent retry
func (s *Server) handleInsert(cc *connCtx, body string) error {
	return nil
	// When implemented, decode each block compression-aware:
	//   if cc.compressed { cc.r.EnableCompression(); defer cc.r.DisableCompression() }
	// around block.DecodeBlock, mirroring drainClientData.
	//
	//m := insertTableRe.FindStringSubmatch(body)
	//if m == nil {
	//	return nil
	//}
	//batch := starrocks.Batch{Table: m[1]}
	//
	//for {
	//	n, err := cc.r.UVarInt()
	//	if err != nil {
	//		return nil
	//	}
	//	if proto.ClientCode(n) != proto.ClientCodeData {
	//		return nil
	//	}
	//	var data proto.ClientData
	//	if err := data.DecodeAware(cc.r, cc.ver); err != nil {
	//		return nil
	//	}
	//	var results proto.Results
	//	var block proto.Block
	//	if err := block.DecodeBlock(cc.r, cc.ver, results.Auto()); err != nil {
	//		return nil
	//	}
	//	if block.Rows == 0 {
	//		break // trailing empty block ends the stream
	//	}
	//	if err := appendBlock(&batch, results); err != nil {
	//		return nil
	//	}
	//}
	//
	//if err := s.sr.StreamLoad(context.Background(), batch); err != nil {
	//	return nil
	//}
	//return s.sendResult(cc, nil)
}

// appendBlock reads decoded columns from a ClientData block into the Batch,
// converting each cell to a JSON-marshalable Go value.
func appendBlock(b *starrocks.Batch, cols proto.Results) error {
	if len(cols) == 0 {
		return nil
	}
	if b.Columns == nil {
		b.Columns = make([]string, len(cols))
		for i, c := range cols {
			b.Columns[i] = c.Name
		}
	}
	rows := cols[0].Data.Rows()
	for i := range rows {
		row := make([]any, len(cols))
		for j, c := range cols {
			v, err := cellValue(c.Data, i)
			if err != nil {
				return fmt.Errorf("column %s: %w", c.Name, err)
			}
			row[j] = v
		}
		b.Rows = append(b.Rows, row)
	}
	return nil
}

// cellValue extracts row i from a decoded column as a JSON-marshalable value.
// Covers the concrete column types Coroot streams for the OTLP tables.
// Results.Auto() yields *proto.ColAuto wrapping the concrete column in .Data.
func cellValue(col proto.ColResult, i int) (any, error) {
	switch c := col.(type) {
	case *proto.ColAuto:
		return cellValue(c.Data, i)
	case *proto.ColStr:
		return c.Row(i), nil
	case *proto.ColInt8:
		return c.Row(i), nil
	case *proto.ColInt16:
		return c.Row(i), nil
	case *proto.ColInt32:
		return c.Row(i), nil
	case *proto.ColInt64:
		return c.Row(i), nil
	case *proto.ColUInt8:
		return c.Row(i), nil
	case *proto.ColUInt16:
		return c.Row(i), nil
	case *proto.ColUInt32:
		return c.Row(i), nil
	case *proto.ColUInt64:
		return c.Row(i), nil
	case *proto.ColFloat32:
		return c.Row(i), nil
	case *proto.ColFloat64:
		return c.Row(i), nil
	case *proto.ColDateTime:
		return c.Row(i).UTC().Format("2006-01-02 15:04:05"), nil
	case *proto.ColDateTime64:
		return c.Row(i).UTC().Format("2006-01-02 15:04:05.000000000"), nil
	case *proto.ColMap[string, string]:
		return c.Row(i), nil
	case *proto.ColArr[string]:
		return c.Row(i), nil
	default:
		return nil, fmt.Errorf("unsupported column type %T", col)
	}
}
