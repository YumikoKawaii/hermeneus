package reader

import (
	"fmt"
	"strings"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tNumber
	tString
	tPunct
	tKeyword
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

var keywords = map[string]bool{
	"SELECT":   true,
	"DISTINCT": true,
	"FROM":     true,
	"WHERE":    true,
	"GROUP":    true,
	"BY":       true,
	"HAVING":   true,
	"ORDER":    true,
	"LIMIT":    true,
	"SETTINGS": true,
	"WITH":     true,
	"AS":       true,
	"JOIN":     true,
	"USING":    true,
	"ON":       true,
	"AND":      true,
	"OR":       true,
	"NOT":      true,
	"IN":       true,
	"GLOBAL":   true,
	"INTERVAL": true,
	"ASC":      true,
	"DESC":     true,
	"NULL":     true,
}

type lexer struct {
	src string
	pos int
}

func newLexer(src string) *lexer { return &lexer{src: src} }

func (l *lexer) errf(format string, a ...any) error {
	return fmt.Errorf("hermeneus lex: "+format+" (at %d)", append(a, l.pos)...)
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func (l *lexer) tokenize() ([]token, error) {
	var out []token
	for {
		l.skipSpace()
		if l.pos >= len(l.src) {
			out = append(out, token{kind: tEOF, pos: l.pos})
			return out, nil
		}
		start := l.pos
		b := l.src[l.pos]
		switch {
		case b == '\'':
			s, err := l.lexString()
			if err != nil {
				return nil, err
			}
			out = append(out, token{tString, s, start})
		case isDigit(b) || (b == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1])):
			out = append(out, token{tNumber, l.lexNumber(), start})
		case isIdentStart(b):
			word := l.lexIdent()
			if keywords[strings.ToUpper(word)] {
				out = append(out, token{tKeyword, strings.ToUpper(word), start})
			} else {
				out = append(out, token{tIdent, word, start})
			}
		default:
			p, err := l.lexPunct()
			if err != nil {
				return nil, err
			}
			out = append(out, token{tPunct, p, start})
		}
	}
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\n', '\r':
			l.pos++
		default:
			return
		}
	}
}

func (l *lexer) lexString() (string, error) {
	start := l.pos
	l.pos++ // opening '
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			l.pos += 2
			continue
		}
		if c == '\'' {
			l.pos++
			return l.src[start:l.pos], nil
		}
		l.pos++
	}
	return "", l.errf("unterminated string literal")
}

func (l *lexer) lexNumber() string {
	start := l.pos
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if isDigit(c) || c == '.' {
			l.pos++
			continue
		}
		break
	}
	return l.src[start:l.pos]
}

func (l *lexer) lexIdent() string {
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	return l.src[start:l.pos]
}

func (l *lexer) lexPunct() (string, error) {
	two := ""
	if l.pos+1 < len(l.src) {
		two = l.src[l.pos : l.pos+2]
	}
	switch two {
	case ">=", "<=", "!=", "<>", "::":
		l.pos += 2
		return two, nil
	}
	b := l.src[l.pos]
	switch b {
	case '(', ')', '[', ']', ',', '.', '+', '-', '*', '/', '%', '=', '<', '>':
		l.pos++
		return string(b), nil
	}
	return "", l.errf("unexpected character %q", string(b))
}
