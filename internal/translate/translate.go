package translate

import "errors"

// ErrUnknownQuery is returned when a SELECT does not match any registered Coroot
// query. The server turns this into a ClickHouse exception AND logs the raw SQL —
// the upgrade tripwire (see docs/DESIGN.md §3).
var ErrUnknownQuery = errors.New("hermeneus: unrecognised query, no translation registered")

// Kind classifies an incoming statement so the server can route it.
type Kind int

const (
	KindUnknown Kind = iota
	KindSystemProbe // system.* / handshake probe -> internal/system
	KindDDL         // CREATE/ALTER/MV -> swallow or map
	KindInsert      // INSERT -> Stream Load
	KindSelect      // known Coroot SELECT -> Translate
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
// TODO(M1): implement — cheap prefix/keyword pass, no full parse needed here.
func Classify(sql string) Kind {
	panic("TODO: Classify")
}

// Translate rewrites a known Coroot ClickHouse SELECT into StarRocks SQL.
// Bound arg placeholders (@name) are preserved for the StarRocks layer to bind.
// Returns ErrUnknownQuery if the statement matches no registered pattern.
// TODO(M2+): register the ~15 queries from docs/DESIGN.md §4, AST-level match.
func Translate(sql string) (Translated, error) {
	return Translated{}, ErrUnknownQuery
}
