package translate

import (
	"fmt"
	"strings"
)

// parser is a recursive-descent parser that accepts ONLY the ClickHouse SELECT
// grammar Coroot emits. Any construct outside that vocabulary is a parse error —
// fail-loud tripwire #1. Params are already client-bound, so literals are
// concrete by the time we parse.
type parser struct {
	toks []token
	pos  int
}

func parseSelect(sql string) (*selectStmt, error) {
	toks, err := newLexer(sql).tokenize()
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	stmt, err := p.selectStmt()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tEOF {
		return nil, p.errf("trailing tokens after statement: %q", p.cur().text)
	}
	return stmt, nil
}

func (p *parser) cur() token  { return p.toks[p.pos] }
func (p *parser) peek() token {
	if p.pos+1 < len(p.toks) {
		return p.toks[p.pos+1]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) advance() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) errf(format string, a ...any) error {
	return fmt.Errorf("hermeneus parse: "+format, a...)
}

func (p *parser) isKw(kw string) bool {
	t := p.cur()
	return t.kind == tKeyword && t.text == kw
}

// isIdentKw reports whether the current token is an identifier whose uppercased
// text equals word — used for CASE/WHEN/THEN/ELSE/END, which are not reserved.
func (p *parser) isIdentKw(word string) bool {
	t := p.cur()
	return t.kind == tIdent && strings.EqualFold(t.text, word)
}

func (p *parser) isPunct(s string) bool {
	t := p.cur()
	return t.kind == tPunct && t.text == s
}

func (p *parser) eatKw(kw string) error {
	if !p.isKw(kw) {
		return p.errf("expected %s, got %q", kw, p.cur().text)
	}
	p.advance()
	return nil
}

func (p *parser) eatPunct(s string) error {
	if !p.isPunct(s) {
		return p.errf("expected %q, got %q", s, p.cur().text)
	}
	p.advance()
	return nil
}

// selectStmt := [WITH cte {, cte}] SELECT [DISTINCT] cols FROM from
//               [WHERE expr] [GROUP BY exprs] [HAVING expr]
//               [ORDER BY items] [LIMIT n] [SETTINGS tail]
func (p *parser) selectStmt() (*selectStmt, error) {
	s := &selectStmt{}
	if p.isKw("WITH") {
		ctes, err := p.withClause()
		if err != nil {
			return nil, err
		}
		s.with = ctes
	}
	if err := p.eatKw("SELECT"); err != nil {
		return nil, err
	}
	if p.isKw("DISTINCT") {
		p.advance()
		s.distinct = true
	}
	cols, err := p.selectCols()
	if err != nil {
		return nil, err
	}
	s.cols = cols
	if p.isKw("FROM") {
		p.advance()
		from, err := p.fromClause()
		if err != nil {
			return nil, err
		}
		s.from = from
	}
	if p.isKw("WHERE") {
		p.advance()
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		s.where = e
	}
	if p.isKw("GROUP") {
		p.advance()
		if err := p.eatKw("BY"); err != nil {
			return nil, err
		}
		exprs, err := p.exprList()
		if err != nil {
			return nil, err
		}
		s.groupBy = exprs
	}
	if p.isKw("HAVING") {
		p.advance()
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		s.having = e
	}
	if p.isKw("ORDER") {
		p.advance()
		if err := p.eatKw("BY"); err != nil {
			return nil, err
		}
		items, err := p.orderItems()
		if err != nil {
			return nil, err
		}
		s.orderBy = items
	}
	if p.isKw("LIMIT") {
		p.advance()
		if p.cur().kind != tNumber {
			return nil, p.errf("expected LIMIT number, got %q", p.cur().text)
		}
		s.limit = p.advance().text
	}
	if p.isKw("SETTINGS") {
		p.advance()
		s.settings = p.consumeSettingsTail()
	}
	return s, nil
}

// consumeSettingsTail swallows everything after SETTINGS to EOF as raw text; the
// builder drops it (StarRocks has no SETTINGS). Kept only so parsing succeeds.
func (p *parser) consumeSettingsTail() string {
	var parts []string
	for p.cur().kind != tEOF {
		parts = append(parts, p.advance().text)
	}
	return strings.Join(parts, " ")
}

func (p *parser) withClause() ([]cte, error) {
	if err := p.eatKw("WITH"); err != nil {
		return nil, err
	}
	var out []cte
	for {
		if p.cur().kind != tIdent {
			return nil, p.errf("expected CTE name, got %q", p.cur().text)
		}
		name := p.advance().text
		if err := p.eatKw("AS"); err != nil {
			return nil, err
		}
		if err := p.eatPunct("("); err != nil {
			return nil, err
		}
		q, err := p.selectStmt()
		if err != nil {
			return nil, err
		}
		if err := p.eatPunct(")"); err != nil {
			return nil, err
		}
		out = append(out, cte{name: name, query: q})
		if p.isPunct(",") {
			p.advance()
			continue
		}
		return out, nil
	}
}

func (p *parser) selectCols() ([]selectCol, error) {
	var out []selectCol
	for {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		col := selectCol{expr: e}
		if p.isKw("AS") {
			p.advance()
			if p.cur().kind != tIdent {
				return nil, p.errf("expected alias, got %q", p.cur().text)
			}
			col.alias = p.advance().text
		}
		out = append(out, col)
		if p.isPunct(",") {
			p.advance()
			continue
		}
		return out, nil
	}
}

// fromClause := fromPrimary { (JOIN fromPrimary USING(...) | , fromPrimary) }
func (p *parser) fromClause() (fromItem, error) {
	left, err := p.fromPrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.isKw("JOIN"):
			p.advance()
			right, err := p.fromPrimary()
			if err != nil {
				return nil, err
			}
			if err := p.eatKw("USING"); err != nil {
				return nil, err
			}
			if err := p.eatPunct("("); err != nil {
				return nil, err
			}
			var cols []string
			for {
				if p.cur().kind != tIdent {
					return nil, p.errf("expected USING column, got %q", p.cur().text)
				}
				cols = append(cols, p.advance().text)
				if p.isPunct(",") {
					p.advance()
					continue
				}
				break
			}
			if err := p.eatPunct(")"); err != nil {
				return nil, err
			}
			left = joinRef{left: left, right: right, using: cols}
		case p.isPunct(","):
			p.advance()
			right, err := p.fromPrimary()
			if err != nil {
				return nil, err
			}
			left = joinRef{left: left, right: right, comma: true}
		default:
			return left, nil
		}
	}
}

func (p *parser) fromPrimary() (fromItem, error) {
	if p.isPunct("(") {
		p.advance()
		q, err := p.selectStmt()
		if err != nil {
			return nil, err
		}
		if err := p.eatPunct(")"); err != nil {
			return nil, err
		}
		alias := p.optAlias()
		return subqueryRef{query: q, alias: alias}, nil
	}
	if p.cur().kind != tIdent {
		return nil, p.errf("expected table name, got %q", p.cur().text)
	}
	name := p.advance().text
	alias := p.optAlias()
	return tableRef{name: name, alias: alias}, nil
}

// optAlias reads an optional table alias (bare ident or AS ident).
func (p *parser) optAlias() string {
	if p.isKw("AS") {
		p.advance()
		if p.cur().kind == tIdent {
			return p.advance().text
		}
		return ""
	}
	if p.cur().kind == tIdent {
		return p.advance().text
	}
	return ""
}

func (p *parser) orderItems() ([]orderItem, error) {
	var out []orderItem
	for {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		it := orderItem{expr: e}
		if p.isKw("ASC") {
			p.advance()
		} else if p.isKw("DESC") {
			p.advance()
			it.desc = true
		}
		out = append(out, it)
		if p.isPunct(",") {
			p.advance()
			continue
		}
		return out, nil
	}
}

func (p *parser) exprList() ([]expr, error) {
	var out []expr
	for {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		if p.isPunct(",") {
			p.advance()
			continue
		}
		return out, nil
	}
}

// Expression grammar (lowest to highest precedence):
//   expr    := orExpr
//   orExpr  := andExpr { OR andExpr }
//   andExpr := notExpr { AND notExpr }
//   notExpr := NOT notExpr | cmpExpr
//   cmpExpr := addExpr [ (= != < <= > >=) addExpr | [GLOBAL] [NOT] IN inRHS ]
//   addExpr := mulExpr { (+|-) mulExpr }
//   mulExpr := unary { (*|/|%) unary }
//   unary   := postfix
//   postfix := primary { [ index ] | . field }
func (p *parser) expr() (expr, error) { return p.orExpr() }

func (p *parser) orExpr() (expr, error) {
	left, err := p.andExpr()
	if err != nil {
		return nil, err
	}
	for p.isKw("OR") {
		p.advance()
		right, err := p.andExpr()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: "OR", left: left, right: right}
	}
	return left, nil
}

func (p *parser) andExpr() (expr, error) {
	left, err := p.notExpr()
	if err != nil {
		return nil, err
	}
	for p.isKw("AND") {
		p.advance()
		right, err := p.notExpr()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: "AND", left: left, right: right}
	}
	return left, nil
}

func (p *parser) notExpr() (expr, error) {
	if p.isKw("NOT") {
		p.advance()
		x, err := p.notExpr()
		if err != nil {
			return nil, err
		}
		return notExpr{x: x}, nil
	}
	return p.cmpExpr()
}

func (p *parser) cmpExpr() (expr, error) {
	left, err := p.addExpr()
	if err != nil {
		return nil, err
	}
	// [GLOBAL] [NOT] IN
	global := false
	if p.isKw("GLOBAL") {
		global = true
		p.advance()
	}
	not := false
	if p.isKw("NOT") && p.peek().kind == tKeyword && p.peek().text == "IN" {
		not = true
		p.advance()
	}
	if p.isKw("IN") {
		p.advance()
		return p.inRHS(left, global, not)
	}
	if global {
		return nil, p.errf("GLOBAL not followed by IN")
	}
	switch {
	case p.isPunct("="), p.isPunct("!="), p.isPunct("<>"),
		p.isPunct("<"), p.isPunct("<="), p.isPunct(">"), p.isPunct(">="):
		op := p.advance().text
		if op == "<>" {
			op = "!="
		}
		right, err := p.addExpr()
		if err != nil {
			return nil, err
		}
		return binaryExpr{op: op, left: left, right: right}, nil
	}
	return left, nil
}

// inRHS parses the right side of IN: (SELECT...), (v,...), [a,...], or bare [a,..].
// A tuple lhs with a parenthesised list of tuples becomes inExpr.tuples.
func (p *parser) inRHS(lhs expr, global, not bool) (expr, error) {
	if p.isPunct("[") {
		arr, err := p.arrayLiteral()
		if err != nil {
			return nil, err
		}
		return inExpr{global: global, not: not, lhs: lhs, list: arr.(arrayExpr).elems}, nil
	}
	if err := p.eatPunct("("); err != nil {
		return nil, err
	}
	if p.isKw("SELECT") || p.isKw("WITH") {
		q, err := p.selectStmt()
		if err != nil {
			return nil, err
		}
		if err := p.eatPunct(")"); err != nil {
			return nil, err
		}
		return inExpr{global: global, not: not, lhs: lhs, sub: q}, nil
	}
	// Empty IN () — Coroot binds an empty array to this.
	if p.isPunct(")") {
		p.advance()
		return inExpr{global: global, not: not, lhs: lhs, list: nil}, nil
	}
	// tuple-IN if lhs is a tuple and first element is a '(' group
	if _, ok := lhs.(tupleExpr); ok && p.isPunct("(") {
		var tuples [][]expr
		for {
			if err := p.eatPunct("("); err != nil {
				return nil, err
			}
			elems, err := p.exprList()
			if err != nil {
				return nil, err
			}
			if err := p.eatPunct(")"); err != nil {
				return nil, err
			}
			tuples = append(tuples, elems)
			if p.isPunct(",") {
				p.advance()
				continue
			}
			break
		}
		if err := p.eatPunct(")"); err != nil {
			return nil, err
		}
		return inExpr{global: global, not: not, lhs: lhs, tuples: tuples}, nil
	}
	list, err := p.exprList()
	if err != nil {
		return nil, err
	}
	if err := p.eatPunct(")"); err != nil {
		return nil, err
	}
	return inExpr{global: global, not: not, lhs: lhs, list: list}, nil
}

func (p *parser) addExpr() (expr, error) {
	left, err := p.mulExpr()
	if err != nil {
		return nil, err
	}
	for p.isPunct("+") || p.isPunct("-") {
		op := p.advance().text
		right, err := p.mulExpr()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) mulExpr() (expr, error) {
	left, err := p.postfix()
	if err != nil {
		return nil, err
	}
	for p.isPunct("*") || p.isPunct("/") || p.isPunct("%") {
		op := p.advance().text
		right, err := p.postfix()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) postfix() (expr, error) {
	e, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		if p.isPunct("[") {
			p.advance()
			idx, err := p.expr()
			if err != nil {
				return nil, err
			}
			if err := p.eatPunct("]"); err != nil {
				return nil, err
			}
			e = indexExpr{base: e, index: idx}
			continue
		}
		return e, nil
	}
}

func (p *parser) primary() (expr, error) {
	t := p.cur()
	switch {
	case p.isKw("NULL"):
		p.advance()
		return nullExpr{}, nil
	case p.isIdentKw("CASE"):
		return p.caseExpr()
	case p.isKw("INTERVAL"):
		p.advance()
		if p.cur().kind != tNumber {
			return nil, p.errf("expected INTERVAL number, got %q", p.cur().text)
		}
		n := p.advance().text
		if p.cur().kind != tIdent {
			return nil, p.errf("expected INTERVAL unit, got %q", p.cur().text)
		}
		unit := p.advance().text
		return intervalExpr{n: n, unit: unit}, nil
	case p.isPunct("*"):
		p.advance()
		return starExpr{}, nil
	case p.isPunct("["):
		return p.arrayLiteral()
	case p.isPunct("("):
		// parenthesised expr or tuple
		p.advance()
		first, err := p.expr()
		if err != nil {
			return nil, err
		}
		if p.isPunct(",") {
			elems := []expr{first}
			for p.isPunct(",") {
				p.advance()
				e, err := p.expr()
				if err != nil {
					return nil, err
				}
				elems = append(elems, e)
			}
			if err := p.eatPunct(")"); err != nil {
				return nil, err
			}
			return tupleExpr{elems: elems}, nil
		}
		if err := p.eatPunct(")"); err != nil {
			return nil, err
		}
		return first, nil
	case t.kind == tNumber:
		p.advance()
		return numberExpr{text: t.text}, nil
	case t.kind == tString:
		p.advance()
		return stringExpr{text: t.text}, nil
	case t.kind == tIdent:
		return p.identOrCall()
	}
	return nil, p.errf("unexpected token %q in expression", t.text)
}

// identOrCall parses ident, ident.field (nested column), or a function call.
func (p *parser) identOrCall() (expr, error) {
	name := p.advance().text
	if p.isPunct("(") {
		return p.call(name)
	}
	if p.isPunct(".") {
		p.advance()
		if p.cur().kind != tIdent {
			return nil, p.errf("expected field after '.', got %q", p.cur().text)
		}
		field := p.advance().text
		return memberExpr{base: name, field: field}, nil
	}
	return identExpr{name: name}, nil
}

func (p *parser) call(fn string) (expr, error) {
	if err := p.eatPunct("("); err != nil {
		return nil, err
	}
	c := callExpr{fn: fn}
	if p.isPunct(")") { // zero-arg call
		p.advance()
		return c, nil
	}
	if p.isKw("DISTINCT") {
		p.advance()
		c.distinct = true
	}
	args, err := p.exprList()
	if err != nil {
		return nil, err
	}
	c.args = args
	if err := p.eatPunct(")"); err != nil {
		return nil, err
	}
	return c, nil
}

func (p *parser) arrayLiteral() (expr, error) {
	if err := p.eatPunct("["); err != nil {
		return nil, err
	}
	if p.isPunct("]") {
		p.advance()
		return arrayExpr{}, nil
	}
	elems, err := p.exprList()
	if err != nil {
		return nil, err
	}
	if err := p.eatPunct("]"); err != nil {
		return nil, err
	}
	return arrayExpr{elems: elems}, nil
}

func (p *parser) caseExpr() (expr, error) {
	if !p.isIdentKw("CASE") {
		return nil, p.errf("expected CASE, got %q", p.cur().text)
	}
	p.advance()
	var c caseExpr
	for p.isIdentKw("WHEN") {
		p.advance()
		cond, err := p.expr()
		if err != nil {
			return nil, err
		}
		if !p.isIdentKw("THEN") {
			return nil, p.errf("expected THEN, got %q", p.cur().text)
		}
		p.advance()
		res, err := p.expr()
		if err != nil {
			return nil, err
		}
		c.whens = append(c.whens, whenClause{cond: cond, result: res})
	}
	if p.isIdentKw("ELSE") {
		p.advance()
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		c.els = e
	}
	if !p.isIdentKw("END") {
		return nil, p.errf("expected END, got %q", p.cur().text)
	}
	p.advance()
	return c, nil
}
