package extractor

type Statement struct {
	With     []CTE
	Distinct bool
	Cols     []Column
	From     FromItem
	Where    Expression
	GroupBy  []Expression
	Having   Expression
	OrderBy  []OrderItem
	Limit    string
	Settings string
}

type CTE struct {
	Name  string
	Query *Statement
}

type Column struct {
	Expression Expression
	Alias      string
}

type OrderItem struct {
	Expression Expression
	Desc       bool
}

// FromItem is a table, an aliased subquery, or a JOIN/comma-join tree. Writer
// packages may define additional FromItem implementations (e.g. UNNEST).
type FromItem interface{ IsFrom() }

type TableRef struct {
	Name  string
	Alias string
}

type SubqueryRef struct {
	Query *Statement
	Alias string
}

type JoinRef struct {
	Left  FromItem
	Right FromItem
	Using []string
	Comma bool
}

func (TableRef) IsFrom()    {}
func (SubqueryRef) IsFrom() {}
func (JoinRef) IsFrom()     {}

// Expression nodes ---------------------------------------------------------------

type Expression interface{ IsExpression() }

type IdentExpression struct{ Name string }         // bare column: ServiceName
type MemberExpression struct{ Base, Field string } // nested column: Events.Timestamp
type NumberExpression struct{ Text string }        // 42, 3.14
type StringExpression struct{ Text string }        // 'x' (raw, quotes included)
type NullExpression struct{}                       // NULL
type StarExpression struct{}                       // * (only inside count(*)/count(1) region)

type CallExpression struct {
	Fn       string
	Distinct bool // groupArray(distinct x) / count(distinct x)
	Args     []Expression
}

type IndexExpression struct { // map/array subscript: LogAttributes['k']
	Base  Expression
	Index Expression
}

type ArrayExpression struct{ Elems []Expression } // [a, b, c]
type TupleExpression struct{ Elems []Expression } // (a, b)

type BinaryExpression struct {
	Op          string // = != < <= > >= + - * / % AND OR
	Left, Right Expression
}

type NotExpression struct{ X Expression }

type BetweenExpression struct { // x [NOT] BETWEEN lo AND hi
	Not      bool
	X        Expression
	Lo, Hi   Expression
}

type SubqueryExpression struct{ Sub *Statement } // scalar (SELECT ...) as an expression

type InExpression struct {
	Global bool
	Not    bool
	Lhs    Expression
	// exactly one of the following is set:
	List   []Expression   // IN (v1, v2, ...) or IN [a,b] (array flattened)
	Sub    *Statement     // IN (SELECT ...)
	Tuples [][]Expression // (a,b) IN ((x,y),(z,w)) — Lhs is a TupleExpression
}

type IntervalExpression struct { // INTERVAL n second
	N    string
	Unit string
}

type CaseExpression struct {
	Whens []WhenClause
	Els   Expression // may be nil
}

type WhenClause struct{ Cond, Result Expression }

func (IdentExpression) IsExpression()    {}
func (MemberExpression) IsExpression()   {}
func (NumberExpression) IsExpression()   {}
func (StringExpression) IsExpression()   {}
func (NullExpression) IsExpression()     {}
func (StarExpression) IsExpression()     {}
func (CallExpression) IsExpression()     {}
func (IndexExpression) IsExpression()    {}
func (ArrayExpression) IsExpression()    {}
func (TupleExpression) IsExpression()    {}
func (BinaryExpression) IsExpression()   {}
func (NotExpression) IsExpression()      {}
func (BetweenExpression) IsExpression()  {}
func (SubqueryExpression) IsExpression() {}
func (InExpression) IsExpression()       {}
func (IntervalExpression) IsExpression() {}
func (CaseExpression) IsExpression()     {}
