package translate

import (
	"errors"
	"strings"

	"github.com/yumikokawaii/hermeneus/internal/extractor"
)

type Reader interface {
	Read(*extractor.Statement) (string, error)
}

type Translator struct {
	r Reader
}

func New(r Reader) *Translator { return &Translator{r: r} }

// ErrUnknownQuery is returned when a SELECT does not match any registered Coroot
// query. The server turns this into a ClickHouse exception AND logs the raw SQL —
// the upgrade tripwire (see docs/DESIGN.md §3).
var ErrUnknownQuery = errors.New("hermeneus: unrecognised query, no translation registered")

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

// IsInsert reports whether a raw statement body is an INSERT, which the server
// routes to the Writer sink. Coroot's clickhouse-go binds @named args
// client-side, so bodies arrive as concrete SQL; this is a cheap prefix pass.
// DDL swallowing and system.* probes are classified in internal/system.
func IsInsert(sql string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "INSERT")
}

// Translate rewrites a known Coroot ClickHouse SELECT into the target SQL of the
// injected Writer. Returns ErrUnknownQuery if the statement matches no known
// Coroot query, or the Writer cannot map it — the upgrade tripwire.
func (t *Translator) Translate(sql string) (Translated, error) {
	stmt, err := extractor.ExtractLogicalIR(sql)
	if err != nil {
		return Translated{}, ErrUnknownQuery
	}
	shape, ok := recognise(stmt)
	if !ok {
		return Translated{}, ErrUnknownQuery
	}
	out, err := t.r.Read(stmt)
	if err != nil {
		return Translated{}, ErrUnknownQuery
	}
	return Translated{SQL: out, Shape: shape}, nil
}
