package translate

import (
	"fmt"
	"strings"
)

// build prints a parsed CH SELECT as StarRocks SQL, applying every CH→SR
// construct mapping structurally over the AST. Any function or construct it
// cannot map returns an error — fail-loud tripwire #2 (the real IR seam).
type builder struct {
	sb  strings.Builder
	err error
}

func buildStarRocks(s *selectStmt) (string, error) {
	b := &builder{}
	b.selectStmt(s)
	if b.err != nil {
		return "", b.err
	}
	return b.sb.String(), nil
}

func (b *builder) fail(format string, a ...any) {
	if b.err == nil {
		b.err = fmt.Errorf("hermeneus build: "+format, a...)
	}
}

func (b *builder) w(s string) { b.sb.WriteString(s) }

func (b *builder) selectStmt(s *selectStmt) {
	if len(s.with) > 0 {
		b.w("WITH ")
		for i, c := range s.with {
			if i > 0 {
				b.w(", ")
			}
			b.w(c.name)
			b.w(" AS (")
			b.selectStmt(c.query)
			b.w(")")
		}
		b.w(" ")
	}
	b.w("SELECT ")
	if s.distinct {
		b.w("DISTINCT ")
	}
	for i, c := range s.cols {
		if i > 0 {
			b.w(", ")
		}
		b.expr(c.expr)
		if c.alias != "" {
			b.w(" AS ")
			b.w(c.alias)
		}
	}
	if s.from != nil {
		b.w(" FROM ")
		b.fromItem(s.from)
	}
	if s.where != nil {
		b.w(" WHERE ")
		b.expr(s.where)
	}
	if len(s.groupBy) > 0 {
		b.w(" GROUP BY ")
		for i, e := range s.groupBy {
			if i > 0 {
				b.w(", ")
			}
			b.expr(e)
		}
	}
	if s.having != nil {
		b.w(" HAVING ")
		b.expr(s.having)
	}
	if len(s.orderBy) > 0 {
		b.w(" ORDER BY ")
		for i, it := range s.orderBy {
			if i > 0 {
				b.w(", ")
			}
			b.expr(it.expr)
			if it.desc {
				b.w(" DESC")
			}
		}
	}
	if s.limit != "" {
		b.w(" LIMIT ")
		b.w(s.limit)
	}
	// SETTINGS is intentionally dropped — StarRocks has no equivalent.
}

var derivedAliasSeq int

func (b *builder) fromItem(f fromItem) {
	switch v := f.(type) {
	case tableRef:
		b.w(v.name)
		if v.alias != "" {
			b.w(" ")
			b.w(v.alias)
		}
	case subqueryRef:
		b.w("(")
		b.selectStmt(v.query)
		b.w(")")
		alias := v.alias
		if alias == "" {
			// StarRocks requires every derived table to have an alias.
			derivedAliasSeq++
			alias = fmt.Sprintf("t%d", derivedAliasSeq)
		}
		b.w(" ")
		b.w(alias)
	case joinRef:
		b.fromItem(v.left)
		if v.comma {
			b.w(", ")
			b.fromItem(v.right)
			return
		}
		b.w(" JOIN ")
		b.fromItem(v.right)
		b.w(" USING(")
		b.w(strings.Join(v.using, ", "))
		b.w(")")
	case unnestRef:
		// StarRocks lateral: unnest(<arr>) AS t(<col>)
		b.w("unnest(")
		b.expr(v.arg)
		b.w(") AS t(")
		b.w(v.colAlias)
		b.w(")")
	default:
		b.fail("unknown from item %T", f)
	}
}

func (b *builder) expr(e expr) {
	switch v := e.(type) {
	case identExpr:
		b.w(v.name)
	case memberExpr:
		// CH Nested parallel-array column Events.X -> SR backtick Array column.
		if v.base == "Events" && (v.field == "Timestamp" || v.field == "Name" || v.field == "Attributes") {
			b.w("`Events." + v.field + "`")
			return
		}
		b.w(v.base + "." + v.field)
	case numberExpr:
		b.w(v.text)
	case stringExpr:
		b.w(v.text)
	case nullExpr:
		b.w("NULL")
	case starExpr:
		b.w("*")
	case arrayExpr:
		b.w("[")
		for i, el := range v.elems {
			if i > 0 {
				b.w(", ")
			}
			b.expr(el)
		}
		b.w("]")
	case tupleExpr:
		b.w("(")
		for i, el := range v.elems {
			if i > 0 {
				b.w(", ")
			}
			b.expr(el)
		}
		b.w(")")
	case indexExpr:
		b.expr(v.base)
		b.w("[")
		b.expr(v.index)
		b.w("]")
	case binaryExpr:
		b.binary(v)
	case notExpr:
		b.w("NOT ")
		b.expr(v.x)
	case caseExpr:
		b.caseExpr(v)
	case inExpr:
		b.inExpr(v)
	case callExpr:
		b.call(v)
	case intervalExpr:
		b.fail("bare INTERVAL not expected outside toStartOfInterval")
	default:
		b.fail("unknown expr %T", e)
	}
}

func (b *builder) binary(v binaryExpr) {
	// max(End)+1 -> date_add(max(End), INTERVAL 1 SECOND)
	if v.op == "+" {
		if c, ok := v.left.(callExpr); ok && c.fn == "max" &&
			isNumber(v.right, "1") && len(c.args) == 1 {
			if id, ok := c.args[0].(identExpr); ok && id.name == "End" {
				b.w("date_add(max(End), INTERVAL 1 SECOND)")
				return
			}
		}
	}
	if v.op == "AND" || v.op == "OR" {
		b.w("(")
		b.expr(v.left)
		b.w(" " + v.op + " ")
		b.expr(v.right)
		b.w(")")
		return
	}
	b.expr(v.left)
	b.w(" " + v.op + " ")
	b.expr(v.right)
}

func (b *builder) caseExpr(v caseExpr) {
	b.w("CASE")
	for _, w := range v.whens {
		b.w(" WHEN ")
		b.expr(w.cond)
		b.w(" THEN ")
		b.expr(w.result)
	}
	if v.els != nil {
		b.w(" ELSE ")
		b.expr(v.els)
	}
	b.w(" END")
}

func (b *builder) inExpr(v inExpr) {
	// tuple-IN is rewritten to an OR chain, so it replaces the whole "lhs IN …".
	if v.tuples != nil {
		b.tupleIn(v.lhs, v.tuples, v.not)
		return
	}
	b.expr(v.lhs)
	if v.not {
		b.w(" NOT")
	}
	// GLOBAL IN -> IN
	b.w(" IN ")
	switch {
	case v.sub != nil:
		b.w("(")
		b.selectStmt(v.sub)
		b.w(")")
	case len(v.list) == 0:
		// Empty IN () -> IN (NULL): never matches, mirrors CH semantics.
		b.w("(NULL)")
	default:
		b.w("(")
		for i, e := range v.list {
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
func (b *builder) tupleIn(lhs expr, tuples [][]expr, not bool) {
	tup, ok := lhs.(tupleExpr)
	if !ok || len(tup.elems) != 2 {
		b.fail("tuple IN lhs must be a 2-tuple")
		return
	}
	a, c := tup.elems[0], tup.elems[1]
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

func (b *builder) call(c callExpr) {
	fn := c.fn
	if fn == "intDiv" && len(c.args) == 2 {
		b.w("floor((")
		b.expr(c.args[0])
		b.w(")/(")
		b.expr(c.args[1])
		b.w("))")
		return
	}
	if mapped, ok := simpleFnMap[fn]; ok {
		b.w(mapped)
		b.w("(")
		if c.distinct {
			b.w("distinct ")
		}
		b.args(c.args)
		b.w(")")
		return
	}
	switch fn {
	case "multiIf":
		b.multiIf(c.args)
	case "countIf":
		b.needArgs(c, 1, func() {
			b.w("count(if(")
			b.expr(c.args[0])
			b.w(",1,null))")
		})
	case "has":
		b.needArgs(c, 2, func() {
			b.w("array_contains(")
			b.expr(c.args[0])
			b.w(", ")
			b.expr(c.args[1])
			b.w(")")
		})
	case "empty":
		b.needArgs(c, 1, func() {
			b.w("array_length(")
			b.expr(c.args[0])
			b.w(") = 0")
		})
	case "toInt64":
		b.needArgs(c, 1, func() {
			b.w("cast(")
			b.expr(c.args[0])
			b.w(" as bigint)")
		})
	case "match", "hasToken":
		b.regexpFn(fn, c.args)
	case "startsWith":
		b.needArgs(c, 2, func() {
			b.w("starts_with(")
			b.expr(c.args[0])
			b.w(", ")
			b.expr(c.args[1])
			b.w(")")
		})
	case "toStartOfInterval":
		b.toStartOfInterval(c.args)
	case "toDateTime64":
		b.toDateTime64(c.args)
	case "roundDown":
		b.roundDown(c.args)
	case "arrayConcat":
		b.plainFn("array_concat", c.args)
	case "mapKeys":
		b.plainFn("map_keys", c.args)
	case "count", "sum", "min", "max", "floor", "if", "arrayJoin":
		// count/sum/min/max/floor/if pass through; arrayJoin handled at shape
		// level (log attr queries rewrite it to UNNEST) but a bare arrayJoin in
		// other positions passes through as-is for StarRocks lateral use.
		b.plainFn(fn, c.args)
	default:
		b.fail("unmapped function %q", fn)
	}
}

var simpleFnMap = map[string]string{
	"groupArray": "array_agg",
	"any":        "any_value",
}

func (b *builder) plainFn(name string, args []expr) {
	b.w(name)
	b.w("(")
	b.args(args)
	b.w(")")
}

func (b *builder) args(args []expr) {
	for i, a := range args {
		if i > 0 {
			b.w(", ")
		}
		b.expr(a)
	}
}

func (b *builder) needArgs(c callExpr, n int, fn func()) {
	if len(c.args) != n {
		b.fail("%s expects %d args, got %d", c.fn, n, len(c.args))
		return
	}
	fn()
}

func (b *builder) multiIf(args []expr) {
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

func (b *builder) regexpFn(fn string, args []expr) {
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
	s, ok := args[1].(stringExpr)
	if !ok || len(s.text) < 2 {
		b.fail("hasToken token must be a string literal")
		return
	}
	tok := s.text[1 : len(s.text)-1]
	b.w("regexp(")
	b.expr(args[0])
	b.w(fmt.Sprintf(`, '\\b%s\\b')`, tok))
}

func (b *builder) toStartOfInterval(args []expr) {
	if len(args) != 2 {
		b.fail("toStartOfInterval expects 2 args")
		return
	}
	iv, ok := args[1].(intervalExpr)
	if !ok || !strings.EqualFold(iv.unit, "second") {
		b.fail("toStartOfInterval second arg must be INTERVAL n second")
		return
	}
	b.w("from_unixtime(floor(unix_timestamp(")
	b.expr(args[0])
	b.w(fmt.Sprintf(")/%s)*%s)", iv.n, iv.n))
}

func (b *builder) toDateTime64(args []expr) {
	if len(args) < 1 {
		b.fail("toDateTime64 expects a literal")
		return
	}
	s, ok := args[0].(stringExpr)
	if !ok {
		b.fail("toDateTime64 first arg must be a string literal")
		return
	}
	inner := s.text[1 : len(s.text)-1]
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
func (b *builder) roundDown(args []expr) {
	if len(args) != 2 {
		b.fail("roundDown expects 2 args")
		return
	}
	arr, ok := args[1].(arrayExpr)
	if !ok || len(arr.elems) == 0 {
		b.fail("roundDown second arg must be a non-empty array literal")
		return
	}
	bounds := make([]string, len(arr.elems))
	for i, el := range arr.elems {
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

func isNumber(e expr, want string) bool {
	n, ok := e.(numberExpr)
	return ok && n.text == want
}

// exprText renders a simple expr back to text for literal-bound contexts
// (roundDown bounds, Duration/1000000). Returns "" for anything non-trivial.
func exprText(e expr) string {
	sub := &builder{}
	sub.expr(e)
	if sub.err != nil {
		return ""
	}
	return sub.sb.String()
}
