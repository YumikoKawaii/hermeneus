package translate

// CH AST — only the node kinds the Coroot query set uses. The parser rejects
// anything outside this vocabulary (fail-loud at parse time); extraction rejects
// any function/construct it cannot map to StarRocks (fail-loud at the IR seam).

type selectStmt struct {
	with     []cte
	distinct bool
	cols     []selectCol
	from     fromItem
	where    expr
	groupBy  []expr
	having   expr
	orderBy  []orderItem
	limit    string // raw number text, "" if absent
	settings string // raw SETTINGS tail (without the keyword), "" if absent
}

type cte struct {
	name  string
	query *selectStmt
}

type selectCol struct {
	expr  expr
	alias string // "" if none
}

type orderItem struct {
	expr expr
	desc bool
}

// fromItem is a table, an aliased subquery, or a JOIN/comma-join tree.
type fromItem interface{ isFrom() }

type tableRef struct {
	name  string
	alias string
}

type subqueryRef struct {
	query *selectStmt
	alias string // may be "" (CH permits it; StarRocks needs one — builder fills)
}

type joinRef struct {
	left  fromItem
	right fromItem
	using []string // USING(a, b); nil for comma-join
	comma bool     // "a, b" cross join
}

// unnestRef is not parsed from CH — it is synthesised by extraction to turn a
// CH arrayJoin(<arr>) into a StarRocks UNNEST lateral column.
type unnestRef struct {
	arg      expr
	colAlias string
}

func (tableRef) isFrom()    {}
func (subqueryRef) isFrom() {}
func (joinRef) isFrom()     {}
func (unnestRef) isFrom()   {}

// expr nodes ---------------------------------------------------------------

type expr interface{ isExpr() }

type identExpr struct{ name string }          // bare column: ServiceName
type memberExpr struct{ base, field string }  // nested column: Events.Timestamp
type numberExpr struct{ text string }         // 42, 3.14
type stringExpr struct{ text string }         // 'x' (raw, quotes included)
type nullExpr struct{}                        // NULL
type starExpr struct{}                        // * (only inside count(*)/count(1) region)

type callExpr struct {
	fn       string
	distinct bool   // groupArray(distinct x) / count(distinct x)
	args     []expr
}

type indexExpr struct { // map/array subscript: LogAttributes['k']
	base  expr
	index expr
}

type arrayExpr struct{ elems []expr } // [a, b, c]
type tupleExpr struct{ elems []expr } // (a, b)

type binaryExpr struct {
	op          string // = != < <= > >= + - * / % AND OR
	left, right expr
}

type notExpr struct{ x expr }

type inExpr struct {
	global bool
	not    bool
	lhs    expr
	// exactly one of the following is set:
	list    []expr       // IN (v1, v2, ...) or IN [a,b] (array flattened)
	sub     *selectStmt  // IN (SELECT ...)
	tuples  [][]expr     // (a,b) IN ((x,y),(z,w)) — lhs is a tupleExpr
}

type intervalExpr struct { // INTERVAL n second
	n    string
	unit string
}

type caseExpr struct {
	whens []whenClause
	els   expr // may be nil
}

type whenClause struct{ cond, result expr }

func (identExpr) isExpr()    {}
func (memberExpr) isExpr()   {}
func (numberExpr) isExpr()   {}
func (stringExpr) isExpr()   {}
func (nullExpr) isExpr()     {}
func (starExpr) isExpr()     {}
func (callExpr) isExpr()     {}
func (indexExpr) isExpr()    {}
func (arrayExpr) isExpr()    {}
func (tupleExpr) isExpr()    {}
func (binaryExpr) isExpr()   {}
func (notExpr) isExpr()      {}
func (inExpr) isExpr()       {}
func (intervalExpr) isExpr() {}
func (caseExpr) isExpr()     {}
