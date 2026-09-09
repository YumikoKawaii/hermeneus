package chserver

import (
	"database/sql"
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
	default:
		return appender{}, fmt.Errorf("unsupported CH type %q", chType)
	}
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
