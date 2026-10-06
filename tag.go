package normalize

import (
	"fmt"
	"strings"
)

// element is one comma-separated item of a tag: an operation or directive
// name, an optional argument after "=", or a parenthesized group for keys.
type element struct {
	name    string
	arg     string
	hasArg  bool
	group   []element
	isGroup bool
	raw     string // the element as written, for error messages
}

// splitTag splits a tag value into elements. Arguments may be wrapped in
// single quotes to contain commas or parentheses; a quote inside a quoted
// argument is written twice.
func splitTag(tag string) ([]element, error) {
	p := tagParser{s: tag}
	els, err := p.list(false)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedTag, err)
	}
	return els, nil
}

type tagParser struct {
	s string
	i int
}

func (p *tagParser) list(inGroup bool) ([]element, error) {
	var els []element
	for {
		el, err := p.element(inGroup)
		if err != nil {
			return nil, err
		}
		els = append(els, el)
		if p.i == len(p.s) {
			if inGroup {
				return nil, fmt.Errorf("unclosed group")
			}
			return els, nil
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case ')':
			if !inGroup {
				return nil, fmt.Errorf("unexpected %q at offset %d", ')', p.i)
			}
			p.i++
			return els, nil
		default:
			return nil, fmt.Errorf("unexpected %q at offset %d", p.s[p.i], p.i)
		}
	}
}

func (p *tagParser) element(inGroup bool) (element, error) {
	start := p.i
	for p.i < len(p.s) && !strings.ContainsRune(",=()'", rune(p.s[p.i])) {
		p.i++
	}
	el := element{name: p.s[start:p.i]}
	if el.name == "" {
		return el, fmt.Errorf("empty element at offset %d", start)
	}
	if p.i < len(p.s) {
		switch p.s[p.i] {
		case '=':
			p.i++
			arg, err := p.arg(inGroup)
			if err != nil {
				return el, err
			}
			el.arg, el.hasArg = arg, true
		case '(':
			if inGroup {
				return el, fmt.Errorf("nested group at offset %d", p.i)
			}
			p.i++
			group, err := p.list(true)
			if err != nil {
				return el, err
			}
			el.group, el.isGroup = group, true
		case '\'':
			return el, fmt.Errorf("unexpected quote at offset %d", p.i)
		}
	}
	el.raw = p.s[start:p.i]
	return el, nil
}

func (p *tagParser) arg(inGroup bool) (string, error) {
	if p.i < len(p.s) && p.s[p.i] == '\'' {
		return p.quoted()
	}
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == ',' || (c == ')' && inGroup) {
			break
		}
		if c == '\'' || c == '(' || c == ')' {
			return "", fmt.Errorf("unexpected %q at offset %d; quote the argument", c, p.i)
		}
		p.i++
	}
	return p.s[start:p.i], nil
}

func (p *tagParser) quoted() (string, error) {
	start := p.i
	p.i++ // opening quote
	var b strings.Builder
	for {
		if p.i >= len(p.s) {
			return "", fmt.Errorf("unterminated quote at offset %d", start)
		}
		c := p.s[p.i]
		if c == '\'' {
			if p.i+1 < len(p.s) && p.s[p.i+1] == '\'' {
				b.WriteByte('\'')
				p.i += 2
				continue
			}
			p.i++
			return b.String(), nil
		}
		b.WriteByte(c)
		p.i++
	}
}
