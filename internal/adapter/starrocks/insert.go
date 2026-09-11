package starrocks

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yumikokawaii/hermeneus/internal/extractor"
)

func (c *Client) insert(ctx context.Context, target string, records []extractor.Record) error {
	if c.db == nil {
		return fmt.Errorf("starrocks: insert mode needs a mysql DSN")
	}
	columns := records[0].Columns
	cols := make([]string, len(columns))
	for i, col := range columns {
		cols[i] = "`" + col + "`"
	}

	var b strings.Builder
	b.WriteString("INSERT INTO `")
	b.WriteString(c.cfg.Database)
	b.WriteString("`.`")
	b.WriteString(target)
	b.WriteString("` (")
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(") VALUES ")

	args := make([]any, 0, len(records)*len(columns))
	for i, r := range records {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for j := range columns {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("?")
			args = append(args, sqlArg(r.Values[j]))
		}
		b.WriteString(")")
	}

	if _, err := c.db.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("starrocks insert %s: %w", target, err)
	}
	return nil
}

func sqlArg(v extractor.Value) any {
	switch v.Kind {
	case extractor.KindNull:
		return nil
	case extractor.KindTime:
		if t, ok := v.V.(time.Time); ok {
			return t.UTC().Format("2006-01-02 15:04:05.000000000")
		}
		return nil
	case extractor.KindArray, extractor.KindMap:
		return literal(v)
	default:
		return v.V
	}
}

func literal(v extractor.Value) string {
	switch v.Kind {
	case extractor.KindNull:
		return "NULL"
	case extractor.KindString:
		s, _ := v.V.(string)
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	case extractor.KindBool:
		if b, _ := v.V.(bool); b {
			return "1"
		}
		return "0"
	case extractor.KindTime:
		if t, ok := v.V.(time.Time); ok {
			return "'" + t.UTC().Format("2006-01-02 15:04:05.000000000") + "'"
		}
		return "NULL"
	case extractor.KindArray:
		elems, _ := v.V.([]extractor.Value)
		parts := make([]string, len(elems))
		for i, e := range elems {
			parts[i] = literal(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case extractor.KindMap:
		m, _ := v.V.(map[string]extractor.Value)
		parts := make([]string, 0, len(m))
		for k, e := range m {
			parts = append(parts, "'"+strings.ReplaceAll(k, "'", "''")+"', "+literal(e))
		}
		return "map(" + strings.Join(parts, ", ") + ")"
	default:
		return fmt.Sprintf("%v", numText(v.V))
	}
}

func numText(v any) string {
	switch n := v.(type) {
	case int64:
		return strconv.FormatInt(n, 10)
	case uint64:
		return strconv.FormatUint(n, 10)
	case float64:
		return strconv.FormatFloat(n, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}
