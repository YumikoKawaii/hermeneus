package translate

import (
	"errors"
	"strings"
)

// ErrUnknownQuery is returned when a SELECT does not match any registered Coroot
// query. The server turns this into a ClickHouse exception AND logs the raw SQL —
// the upgrade tripwire (see docs/DESIGN.md §3).
var ErrUnknownQuery = errors.New("hermeneus: unrecognised query, no translation registered")

// Kind classifies an incoming statement so the server can route it.
type Kind int

const (
	KindUnknown     Kind = iota
	KindSystemProbe      // system.* / handshake probe -> internal/system
	KindDDL              // CREATE/ALTER/MV -> swallow or map
	KindInsert           // INSERT -> Stream Load
	KindSelect           // known Coroot SELECT -> Translate
)

// ResultShape declares the exact column order + ClickHouse types Coroot's
// Result.Auto() expects back, so the server can encode the block correctly.
type ResultShape struct {
	Columns []Column
}

type Column struct {
	Name   string
	CHType string // e.g. "String", "UInt64", "DateTime64(9)", "Array(String)", "Map(String,String)"
}

// Translated is the output of the rewriter: StarRocks SQL plus the result shape
// to encode the response with.
type Translated struct {
	SQL   string
	Shape ResultShape
}

// Classify inspects a raw statement body and decides its Kind.
// Coroot's clickhouse-go binds @named args client-side, so bodies arrive as
// concrete SQL. This is a cheap prefix/keyword pass, no full parse.
func Classify(sql string) Kind {
	s := strings.ToUpper(strings.TrimSpace(sql))
	switch {
	case strings.HasPrefix(s, "INSERT"):
		return KindInsert
	case strings.HasPrefix(s, "CREATE"), strings.HasPrefix(s, "ALTER"),
		strings.HasPrefix(s, "DROP"), strings.HasPrefix(s, "RENAME"),
		strings.HasPrefix(s, "TRUNCATE"), strings.HasPrefix(s, "OPTIMIZE"),
		strings.HasPrefix(s, "SET "), strings.HasPrefix(s, "USE "):
		return KindDDL
	case strings.Contains(s, "SYSTEM."), strings.Contains(s, "CURRENTDATABASE()"):
		return KindSystemProbe
	case strings.HasPrefix(s, "SELECT"), strings.HasPrefix(s, "WITH"):
		return KindSelect
	default:
		return KindUnknown
	}
}

// Translate rewrites a known Coroot ClickHouse SELECT into StarRocks SQL.
// Returns ErrUnknownQuery if the statement matches no registered pattern —
// the upgrade tripwire (docs/DESIGN.md §3).
func Translate(sql string) (Translated, error) {
	stmt, err := parseSelect(sql)
	if err != nil {
		return Translated{}, ErrUnknownQuery
	}
	shape, ok := recognise(stmt)
	if !ok {
		return Translated{}, ErrUnknownQuery
	}
	out, err := buildStarRocks(stmt)
	if err != nil {
		return Translated{}, ErrUnknownQuery
	}
	return Translated{SQL: out, Shape: shape}, nil
}
