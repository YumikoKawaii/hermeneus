package chserver

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/yumikokawaii/hermeneus/internal/translate"
)

// appender is a column paired with the func that appends one StarRocks cell
// (as raw text, the wire form of a database/sql RawBytes) to it.
type appender struct {
	column proto.InputColumn
	append func(raw []byte) error
}

// newAppender builds an empty ch column and its cell appender for a CH type.
func newAppender(name, chType string) (appender, error) {
	switch chType {
	case "String":
		c := new(proto.ColStr)
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			c.Append(string(raw))
			return nil
		}}, nil
	case "Int64":
		c := new(proto.ColInt64)
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			v, err := strconv.ParseInt(string(raw), 10, 64)
			if err != nil {
				return err
			}
			c.Append(v)
			return nil
		}}, nil
	case "UInt64":
		c := new(proto.ColUInt64)
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			v, err := strconv.ParseUint(string(raw), 10, 64)
			if err != nil {
				return err
			}
			c.Append(v)
			return nil
		}}, nil
	case "Float64":
		c := new(proto.ColFloat64)
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			v, err := strconv.ParseFloat(string(raw), 64)
			if err != nil {
				return err
			}
			c.Append(v)
			return nil
		}}, nil
	case "DateTime":
		c := new(proto.ColDateTime)
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			t, err := parseTime(string(raw))
			if err != nil {
				return err
			}
			c.Append(t)
			return nil
		}}, nil
	case "DateTime64(9)":
		c := &proto.ColDateTime64{}
		c = c.WithPrecision(proto.PrecisionNano)
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			t, err := parseTime(string(raw))
			if err != nil {
				return err
			}
			c.Append(t)
			return nil
		}}, nil
	case "Array(String)":
		c := new(proto.ColStr).Array()
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			vals, err := parseStrArray(raw)
			if err != nil {
				return err
			}
			c.Append(vals)
			return nil
		}}, nil
	case "Map(String,String)":
		c := proto.NewMap(new(proto.ColStr), new(proto.ColStr))
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			m, err := parseStrMap(raw)
			if err != nil {
				return err
			}
			c.Append(m)
			return nil
		}}, nil
	case "Array(DateTime64(9))":
		inner := new(proto.ColDateTime64).WithPrecision(proto.PrecisionNano)
		c := proto.NewArray[time.Time](inner)
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			ss, err := parseStrArray(raw)
			if err != nil {
				return err
			}
			ts := make([]time.Time, len(ss))
			for i, s := range ss {
				t, err := parseTime(s)
				if err != nil {
					return err
				}
				ts[i] = t
			}
			c.Append(ts)
			return nil
		}}, nil
	case "Array(Map(String,String))":
		inner := proto.NewMap(new(proto.ColStr), new(proto.ColStr))
		c := proto.NewArray[map[string]string](inner)
		return appender{proto.InputColumn{Name: name, Data: c}, func(raw []byte) error {
			ms, err := parseMapArray(raw)
			if err != nil {
				return err
			}
			c.Append(ms)
			return nil
		}}, nil
	default:
		return appender{}, fmt.Errorf("unsupported CH type %q", chType)
	}
}

// StarRocks returns ARRAY / MAP columns over the MySQL wire as JSON text
// (["a","b"], {"k":"v"}). parseStrArray / parseStrMap decode that text; an empty
// cell is treated as an empty container.
func escapeControlChars(raw []byte) []byte {
	needs := false
	for _, b := range raw {
		if b < 0x20 {
			needs = true
			break
		}
	}
	if !needs {
		return raw
	}
	out := make([]byte, 0, len(raw)+16)
	const hex = "0123456789abcdef"
	for _, b := range raw {
		switch {
		case b == '\n':
			out = append(out, '\\', 'n')
		case b == '\t':
			out = append(out, '\\', 't')
		case b == '\r':
			out = append(out, '\\', 'r')
		case b < 0x20:
			out = append(out, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xf])
		default:
			out = append(out, b)
		}
	}
	return out
}

func parseStrArray(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var vals []string
	if err := json.Unmarshal(escapeControlChars(raw), &vals); err != nil {
		return nil, fmt.Errorf("array cell %q: %w", raw, err)
	}
	return vals, nil
}

func parseStrMap(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	m := map[string]string{}
	if err := json.Unmarshal(escapeControlChars(raw), &m); err != nil {
		return nil, fmt.Errorf("map cell %q: %w", raw, err)
	}
	return m, nil
}

func parseMapArray(raw []byte) ([]map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var ms []map[string]string
	if err := json.Unmarshal(escapeControlChars(raw), &ms); err != nil {
		return nil, fmt.Errorf("array-of-map cell %q: %w", raw, err)
	}
	return ms, nil
}

// encodeRows maps StarRocks rows into ch columns per the declared shape.
func encodeRows(rows *sql.Rows, shape translate.ResultShape) ([]proto.InputColumn, error) {
	appenders := make([]appender, len(shape.Columns))
	for i, col := range shape.Columns {
		a, err := newAppender(col.Name, col.CHType)
		if err != nil {
			return nil, err
		}
		appenders[i] = a
	}

	cells := make([]sql.RawBytes, len(appenders))
	dest := make([]any, len(appenders))
	for i := range cells {
		dest[i] = &cells[i]
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		for i, a := range appenders {
			if err := a.append(cells[i]); err != nil {
				return nil, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	cols := make([]proto.InputColumn, len(appenders))
	for i, a := range appenders {
		cols[i] = a.column
	}
	return cols, nil
}

var timeLayouts = []string{
	"2006-01-02 15:04:05.999999",
	"2006-01-02 15:04:05",
	time.RFC3339,
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	// StarRocks from_unixtime may return an epoch-like numeric string.
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(i, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}
