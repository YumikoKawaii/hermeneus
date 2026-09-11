package reader

// CH AST — only the node kinds the Coroot query set uses. The parser rejects
// anything outside this vocabulary (fail-loud at parse time). The AST is the
// contract between reader (this package) and writer: exported so the writer can
// consume and extend it.

type SelectStmt struct {
	With     []CTE
	Distinct bool
	Cols     []SelectCol
	From     FromItem
	Where    Expr
	GroupBy  []Expr
	Having   Expr
	OrderBy  []OrderItem
	Limit    string // raw number text, "" if absent
	Settings string // raw SETTINGS tail (without the keyword), "" if absent
}

type CTE struct {
	Name  string
	Query *SelectStmt
}

type SelectCol struct {
	Expr  Expr
	Alias string // "" if none
}

type OrderItem struct {
	Expr Expr
	Desc bool
}

// FromItem is a table, an aliased subquery, or a JOIN/comma-join tree. Writer
// packages may define additional FromItem implementations (e.g. UNNEST).
type FromItem interface{ IsFrom() }

type TableRef struct {
	Name  string
	Alias string
}

type SubqueryRef struct {
	Query *SelectStmt
	Alias string // may be "" (CH permits it; StarRocks needs one — writer fills)
}

type JoinRef struct {
	Left  FromItem
	Right FromItem
	Using []string // USING(a, b); nil for comma-join
	Comma bool     // "a, b" cross join
}

func (TableRef) IsFrom()    {}
func (SubqueryRef) IsFrom() {}
func (JoinRef) IsFrom()     {}

// Expr nodes ---------------------------------------------------------------

type Expr interface{ IsExpr() }

type IdentExpr struct{ Name string }         // bare column: ServiceName
type MemberExpr struct{ Base, Field string } // nested column: Events.Timestamp
type NumberExpr struct{ Text string }        // 42, 3.14
type StringExpr struct{ Text string }        // 'x' (raw, quotes included)
type NullExpr struct{}                       // NULL
type StarExpr struct{}                       // * (only inside count(*)/count(1) region)

type CallExpr struct {
	Fn       string
	Distinct bool // groupArray(distinct x) / count(distinct x)
	Args     []Expr
}

type IndexExpr struct { // map/array subscript: LogAttributes['k']
	Base  Expr
	Index Expr
}

type ArrayExpr struct{ Elems []Expr } // [a, b, c]
type TupleExpr struct{ Elems []Expr } // (a, b)

type BinaryExpr struct {
	Op          string // = != < <= > >= + - * / % AND OR
	Left, Right Expr
}

type NotExpr struct{ X Expr }

type InExpr struct {
	Global bool
	Not    bool
	Lhs    Expr
	// exactly one of the following is set:
	List   []Expr      // IN (v1, v2, ...) or IN [a,b] (array flattened)
	Sub    *SelectStmt // IN (SELECT ...)
	Tuples [][]Expr    // (a,b) IN ((x,y),(z,w)) — Lhs is a TupleExpr
}

type IntervalExpr struct { // INTERVAL n second
	N    string
	Unit string
}

type CaseExpr struct {
	Whens []WhenClause
	Els   Expr // may be nil
}

type WhenClause struct{ Cond, Result Expr }

func (IdentExpr) IsExpr()    {}
func (MemberExpr) IsExpr()   {}
func (NumberExpr) IsExpr()   {}
func (StringExpr) IsExpr()   {}
func (NullExpr) IsExpr()     {}
func (StarExpr) IsExpr()     {}
func (CallExpr) IsExpr()     {}
func (IndexExpr) IsExpr()    {}
func (ArrayExpr) IsExpr()    {}
func (TupleExpr) IsExpr()    {}
func (BinaryExpr) IsExpr()   {}
func (NotExpr) IsExpr()      {}
func (InExpr) IsExpr()       {}
func (IntervalExpr) IsExpr() {}
func (CaseExpr) IsExpr()     {}
