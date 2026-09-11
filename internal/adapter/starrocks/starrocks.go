package starrocks

import (
	"fmt"
	"strings"

	"github.com/yumikokawaii/hermeneus/internal/extractor"
)

// UnnestRef is a StarRocks lateral UNNEST column. It is not produced by the
// extractor; lowerArrayJoin synthesises it (turning a CH arrayJoin(<arr>) into
// UNNEST) and the writer renders it. It satisfies extractor.FromItem so it can sit
// in the AST.
type UnnestRef struct {
	Arg      extractor.Expression
	ColAlias string
}

func (UnnestRef) IsFrom() {}

type StarRocks struct{}

func (StarRocks) Read(s *extractor.Statement) (string, error) {
	lowerArrayJoin(s)
	b := &builder{}
	b.selectStmt(s)
	if b.err != nil {
		return "", b.err
	}
	return b.sb.String(), nil
}

func (StarRocks) Write(target string, records []extractor.Record) error {
	return fmt.Errorf("starrocks: write unwired for target %q (%d records)", target, len(records))
}

type builder struct {
	sb  strings.Builder
	err error
}

// lowerArrayJoin rewrites a select-list arrayJoin into a StarRocks UNNEST
// lateral. Coroot emits arrayJoin only as the single output column of the log
// attribute-name / attribute-value list queries:
//
//	SELECT [DISTINCT] arrayJoin(<X>) [AS a] FROM t ...
//	  -> SELECT [DISTINCT] k FROM t, unnest(<X>) AS t(k) ...
//
// StarRocks has no arrayJoin; UNNEST is its lateral-explode form.
func lowerArrayJoin(s *extractor.Statement) {
	if len(s.Cols) != 1 {
		return
	}
	call, ok := s.Cols[0].Expression.(extractor.CallExpression)
	if !ok || call.Fn != "arrayJoin" || len(call.Args) != 1 {
		return
	}
	s.Cols = []extractor.Column{{Expression: extractor.IdentExpression{Name: "k"}}}
	s.From = extractor.JoinRef{
		Left:  s.From,
		Right: UnnestRef{Arg: call.Args[0], ColAlias: "k"},
		Comma: true,
	}
}

func (b *builder) fail(format string, a ...any) {
	if b.err == nil {
		b.err = fmt.Errorf("hermeneus build: "+format, a...)
	}
}

func (b *builder) w(s string) { b.sb.WriteString(s) }

func (b *builder) selectStmt(s *extractor.Statement) {
	if len(s.With) > 0 {
		b.w("WITH ")
		for i, c := range s.With {
			if i > 0 {
				b.w(", ")
			}
			b.w(c.Name)
			b.w(" AS (")
			b.selectStmt(c.Query)
			b.w(")")
		}
		b.w(" ")
	}
	b.w("SELECT ")
	if s.Distinct {
		b.w("DISTINCT ")
	}
	for i, c := range s.Cols {
		if i > 0 {
			b.w(", ")
		}
		b.expr(c.Expression)
		if c.Alias != "" {
			b.w(" AS ")
			b.w(c.Alias)
		}
	}
	if s.From != nil {
		b.w(" FROM ")
		b.fromItem(s.From)
	}
	if s.Where != nil {
		b.w(" WHERE ")
		b.expr(s.Where)
	}
	if len(s.GroupBy) > 0 {
		b.w(" GROUP BY ")
		for i, e := range s.GroupBy {
			if i > 0 {
				b.w(", ")
			}
			b.expr(e)
		}
	}
	if s.Having != nil {
		b.w(" HAVING ")
		b.expr(s.Having)
	}
	if len(s.OrderBy) > 0 {
		b.w(" ORDER BY ")
		for i, it := range s.OrderBy {
			if i > 0 {
				b.w(", ")
			}
			b.expr(it.Expression)
			if it.Desc {
				b.w(" DESC")
			}
		}
	}
	if s.Limit != "" {
		b.w(" LIMIT ")
		b.w(s.Limit)
	}
	// SETTINGS is intentionally dropped — StarRocks has no equivalent.
}

var derivedAliasSeq int

func (b *builder) fromItem(f extractor.FromItem) {
	switch v := f.(type) {
	case extractor.TableRef:
		b.w(v.Name)
		if v.Alias != "" {
			b.w(" ")
			b.w(v.Alias)
		}
	case extractor.SubqueryRef:
		b.w("(")
		b.selectStmt(v.Query)
		b.w(")")
		alias := v.Alias
		if alias == "" {
			// StarRocks requires every derived table to have an alias.
			derivedAliasSeq++
			alias = fmt.Sprintf("t%d", derivedAliasSeq)
		}
		b.w(" ")
		b.w(alias)
	case extractor.JoinRef:
		b.fromItem(v.Left)
		if v.Comma {
			b.w(", ")
			b.fromItem(v.Right)
			return
		}
		b.w(" JOIN ")
		b.fromItem(v.Right)
		b.w(" USING(")
		b.w(strings.Join(v.Using, ", "))
		b.w(")")
	case UnnestRef:
		// StarRocks lateral: unnest(<arr>) AS t(<col>)
		b.w("unnest(")
		b.expr(v.Arg)
		b.w(") AS t(")
		b.w(v.ColAlias)
		b.w(")")
	default:
		b.fail("unknown from item %T", f)
	}
}

func (b *builder) expr(e extractor.Expression) {
	switch v := e.(type) {
	case extractor.IdentExpression:
		b.w(v.Name)
	case extractor.MemberExpression:
		// CH Nested parallel-array column Events.X -> SR backtick Array column.
		if v.Base == "Events" && (v.Field == "Timestamp" || v.Field == "Name" || v.Field == "Attributes") {
			b.w("`Events." + v.Field + "`")
			return
		}
		b.w(v.Base + "." + v.Field)
	case extractor.NumberExpression:
		b.w(v.Text)
	case extractor.StringExpression:
		b.w(v.Text)
	case extractor.NullExpression:
		b.w("NULL")
	case extractor.StarExpression:
		b.w("*")
	case extractor.ArrayExpression:
		b.w("[")
		for i, el := range v.Elems {
			if i > 0 {
				b.w(", ")
			}
			b.expr(el)
		}
		b.w("]")
	case extractor.TupleExpression:
		b.w("(")
		for i, el := range v.Elems {
			if i > 0 {
				b.w(", ")
			}
			b.expr(el)
		}
		b.w(")")
	case extractor.IndexExpression:
		b.expr(v.Base)
		b.w("[")
		b.expr(v.Index)
		b.w("]")
	case extractor.BinaryExpression:
		b.binary(v)
	case extractor.NotExpression:
		b.w("NOT ")
		b.expr(v.X)
	case extractor.CaseExpression:
		b.caseExpr(v)
	case extractor.InExpression:
		b.inExpr(v)
	case extractor.CallExpression:
		b.call(v)
	case extractor.IntervalExpression:
		b.fail("bare INTERVAL not expected outside toStartOfInterval")
	default:
		b.fail("unknown expr %T", e)
	}
}

func (b *builder) binary(v extractor.BinaryExpression) {
	// max(End)+1 -> date_add(max(End), INTERVAL 1 SECOND)
	if v.Op == "+" {
		if c, ok := v.Left.(extractor.CallExpression); ok && c.Fn == "max" &&
			isNumber(v.Right, "1") && len(c.Args) == 1 {
			if id, ok := c.Args[0].(extractor.IdentExpression); ok && id.Name == "End" {
				b.w("date_add(max(End), INTERVAL 1 SECOND)")
				return
			}
		}
	}
	if v.Op == "AND" || v.Op == "OR" {
		b.w("(")
		b.expr(v.Left)
		b.w(" " + v.Op + " ")
		b.expr(v.Right)
		b.w(")")
		return
	}
	b.expr(v.Left)
	b.w(" " + v.Op + " ")
	b.expr(v.Right)
}

func (b *builder) caseExpr(v extractor.CaseExpression) {
	b.w("CASE")
	for _, w := range v.Whens {
		b.w(" WHEN ")
		b.expr(w.Cond)
		b.w(" THEN ")
		b.expr(w.Result)
	}
	if v.Els != nil {
		b.w(" ELSE ")
		b.expr(v.Els)
	}
	b.w(" END")
}

func (b *builder) inExpr(v extractor.InExpression) {
	// tuple-IN is rewritten to an OR chain, so it replaces the whole "lhs IN …".
	if v.Tuples != nil {
		b.tupleIn(v.Lhs, v.Tuples, v.Not)
		return
	}
	b.expr(v.Lhs)
	if v.Not {
		b.w(" NOT")
	}
	// GLOBAL IN -> IN
	b.w(" IN ")
	switch {
	case v.Sub != nil:
		b.w("(")
		b.selectStmt(v.Sub)
		b.w(")")
	case len(v.List) == 0:
		// Empty IN () -> IN (NULL): never matches, mirrors CH semantics.
		b.w("(NULL)")
	default:
		b.w("(")
		for i, e := range v.List {
			if i > 0 {
				b.w(", ")
			}
			b.expr(e)
		}
		b.w(")")
	}
}

// tupleIn rewrites (a,b) IN ((x,y),(z,w)) into an OR of equality pairs, since
// StarRocks does not support row-tuple IN. NOT (a,b) IN (...) negates the group.
func (b *builder) tupleIn(lhs extractor.Expression, tuples [][]extractor.Expression, not bool) {
	tup, ok := lhs.(extractor.TupleExpression)
	if !ok || len(tup.Elems) != 2 {
		b.fail("tuple IN lhs must be a 2-tuple")
		return
	}
	a, c := tup.Elems[0], tup.Elems[1]
	if len(tuples) == 0 {
		b.w("FALSE")
		return
	}
	if not {
		b.w("NOT ")
	}
	b.w("(")
	for i, t := range tuples {
		if len(t) != 2 {
			b.fail("tuple IN element must be a 2-tuple")
			return
		}
		if i > 0 {
			b.w(" OR ")
		}
		b.w("(")
		b.expr(a)
		b.w(" = ")
		b.expr(t[0])
		b.w(" AND ")
		b.expr(c)
		b.w(" = ")
		b.expr(t[1])
		b.w(")")
	}
	b.w(")")
}

func (b *builder) call(c extractor.CallExpression) {
	fn := c.Fn
	if fn == "intDiv" && len(c.Args) == 2 {
		b.w("floor((")
		b.expr(c.Args[0])
		b.w(")/(")
		b.expr(c.Args[1])
		b.w("))")
		return
	}
	if mapped, ok := simpleFnMap[fn]; ok {
		b.w(mapped)
		b.w("(")
		if c.Distinct {
			b.w("distinct ")
		}
		b.args(c.Args)
		b.w(")")
		return
	}
	switch fn {
	case "multiIf":
		b.multiIf(c.Args)
	case "countIf":
		b.needArgs(c, 1, func() {
			b.w("count(if(")
			b.expr(c.Args[0])
			b.w(",1,null))")
		})
	case "has":
		b.needArgs(c, 2, func() {
			b.w("array_contains(")
			b.expr(c.Args[0])
			b.w(", ")
			b.expr(c.Args[1])
			b.w(")")
		})
	case "empty":
		b.needArgs(c, 1, func() {
			b.w("array_length(")
			b.expr(c.Args[0])
			b.w(") = 0")
		})
	case "toInt64":
		b.needArgs(c, 1, func() {
			b.w("cast(")
			b.expr(c.Args[0])
			b.w(" as bigint)")
		})
	case "match", "hasToken":
		b.regexpFn(fn, c.Args)
	case "startsWith":
		b.needArgs(c, 2, func() {
			b.w("starts_with(")
			b.expr(c.Args[0])
			b.w(", ")
			b.expr(c.Args[1])
			b.w(")")
		})
	case "toStartOfInterval":
		b.toStartOfInterval(c.Args)
	case "toDateTime64":
		b.toDateTime64(c.Args)
	case "roundDown":
		b.roundDown(c.Args)
	case "arrayConcat":
		b.plainFn("array_concat", c.Args)
	case "mapKeys":
		b.plainFn("map_keys", c.Args)
	case "count", "sum", "min", "max", "floor", "if", "arrayJoin":
		// count/sum/min/max/floor/if pass through; arrayJoin handled at shape
		// level (log attr queries rewrite it to UNNEST) but a bare arrayJoin in
		// other positions passes through as-is for StarRocks lateral use.
		b.plainFn(fn, c.Args)
	default:
		b.fail("unmapped function %q", fn)
	}
}

var simpleFnMap = map[string]string{
	"groupArray": "array_agg",
	"any":        "any_value",
}

func (b *builder) plainFn(name string, args []extractor.Expression) {
	b.w(name)
	b.w("(")
	b.args(args)
	b.w(")")
}

func (b *builder) args(args []extractor.Expression) {
	for i, a := range args {
		if i > 0 {
			b.w(", ")
		}
		b.expr(a)
	}
}

func (b *builder) needArgs(c extractor.CallExpression, n int, fn func()) {
	if len(c.Args) != n {
		b.fail("%s expects %d args, got %d", c.Fn, n, len(c.Args))
		return
	}
	fn()
}

func (b *builder) multiIf(args []extractor.Expression) {
	if len(args) < 3 || len(args)%2 == 0 {
		b.fail("multiIf needs an odd arg count >= 3")
		return
	}
	b.w("CASE")
	i := 0
	for ; i+1 < len(args); i += 2 {
		b.w(" WHEN ")
		b.expr(args[i])
		b.w(" THEN ")
		b.expr(args[i+1])
	}
	b.w(" ELSE ")
	b.expr(args[i])
	b.w(" END")
}

func (b *builder) regexpFn(fn string, args []extractor.Expression) {
	if len(args) != 2 {
		b.fail("%s expects 2 args", fn)
		return
	}
	if fn == "match" {
		b.w("regexp(")
		b.expr(args[0])
		b.w(", ")
		b.expr(args[1])
		b.w(")")
		return
	}
	// hasToken(col, 'tok') -> regexp(col, '\btok\b')
	s, ok := args[1].(extractor.StringExpression)
	if !ok || len(s.Text) < 2 {
		b.fail("hasToken token must be a string literal")
		return
	}
	tok := s.Text[1 : len(s.Text)-1]
	b.w("regexp(")
	b.expr(args[0])
	b.w(fmt.Sprintf(`, '\\b%s\\b')`, tok))
}

func (b *builder) toStartOfInterval(args []extractor.Expression) {
	if len(args) != 2 {
		b.fail("toStartOfInterval expects 2 args")
		return
	}
	iv, ok := args[1].(extractor.IntervalExpression)
	if !ok || !strings.EqualFold(iv.Unit, "second") {
		b.fail("toStartOfInterval second arg must be INTERVAL n second")
		return
	}
	b.w("from_unixtime(floor(unix_timestamp(")
	b.expr(args[0])
	b.w(fmt.Sprintf(")/%s)*%s)", iv.N, iv.N))
}

func (b *builder) toDateTime64(args []extractor.Expression) {
	if len(args) < 1 {
		b.fail("toDateTime64 expects a literal")
		return
	}
	s, ok := args[0].(extractor.StringExpression)
	if !ok {
		b.fail("toDateTime64 first arg must be a string literal")
		return
	}
	inner := s.Text[1 : len(s.Text)-1]
	if dot := strings.IndexByte(inner, '.'); dot >= 0 {
		frac := inner[dot+1:]
		if len(frac) > 6 {
			frac = frac[:6]
		}
		inner = inner[:dot] + "." + frac
	}
	b.w("'" + inner + "'")
}

// roundDown(x, [b0..bn]) -> CASE ladder snapping x down to the largest bound<=x.
func (b *builder) roundDown(args []extractor.Expression) {
	if len(args) != 2 {
		b.fail("roundDown expects 2 args")
		return
	}
	arr, ok := args[1].(extractor.ArrayExpression)
	if !ok || len(arr.Elems) == 0 {
		b.fail("roundDown second arg must be a non-empty array literal")
		return
	}
	bounds := make([]string, len(arr.Elems))
	for i, el := range arr.Elems {
		bounds[i] = exprText(el)
		if bounds[i] == "" {
			b.fail("roundDown bound must be a literal")
			return
		}
	}
	expr0 := exprText(args[0])
	b.w("CASE")
	for i := len(bounds) - 1; i >= 1; i-- {
		b.w(fmt.Sprintf(" WHEN (%s) >= %s THEN %s", expr0, bounds[i], bounds[i]))
	}
	b.w(fmt.Sprintf(" ELSE %s END", bounds[0]))
}

func isNumber(e extractor.Expression, want string) bool {
	n, ok := e.(extractor.NumberExpression)
	return ok && n.Text == want
}

// exprText renders a simple expr back to text for literal-bound contexts
// (roundDown bounds, Duration/1000000). Returns "" for anything non-trivial.
func exprText(e extractor.Expression) string {
	sub := &builder{}
	sub.expr(e)
	if sub.err != nil {
		return ""
	}
	return sub.sb.String()
}
