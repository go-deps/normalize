package normalize

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// collapse replaces every run of Unicode white space in s with a single
// ASCII space. It does not trim; combine it with trim for that. Bytes that are
// not white space, including invalid UTF-8, are copied unchanged.
func collapse(s string) string {
	if !needsCollapse(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
		} else {
			inSpace = false
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// needsCollapse reports whether collapse would change s, so the common case
// returns without allocating.
func needsCollapse(s string) bool {
	prevSpace := false
	for _, r := range s {
		space := isSpace(r)
		if space && (prevSpace || r != ' ') {
			return true
		}
		prevSpace = space
	}
	return false
}

func isSpace(r rune) bool { return unicode.IsSpace(r) }
