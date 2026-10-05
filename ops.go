package normalize

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// builtinPlain are the operations used without an argument.
var builtinPlain = map[string]Func{
	"trim":      strings.TrimSpace,
	"trimleft":  func(s string) string { return strings.TrimLeftFunc(s, unicode.IsSpace) },
	"trimright": func(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) },
	"lower":     strings.ToLower,
	"upper":     strings.ToUpper,
	"collapse":  func(s string) string { return collapse(s, isSpace) },
	"newline":   newline,
	"capfirst":  capfirst,
	"slug":      slug,
	"email":     email,
	"digits":    func(s string) string { return filter(s, unicode.IsDigit, true) },
	"letters":   func(s string) string { return filter(s, unicode.IsLetter, true) },
	"alnum":     func(s string) string { return filter(s, isAlnum, true) },
	"nocontrol": func(s string) string { return filter(s, isControl, false) },
}

// builtinParam are the operations that take an argument.
var builtinParam = map[string]ParamFunc{
	"trim":      cutset(strings.Trim),
	"trimleft":  cutset(strings.TrimLeft),
	"trimright": cutset(strings.TrimRight),
	"cutprefix": cutPrefix,
	"cutsuffix": cutSuffix,
	"keep":      classFilter(true),
	"remove":    classFilter(false),
	"collapse":  collapseParam,
	"case":      caseParam,
	"truncate":  truncateParam,
}

func ok(fn Func) (CheckFunc, error) { return wrap(fn), nil }

func cutset(trim func(string, string) string) ParamFunc {
	return func(arg string) (CheckFunc, error) {
		if arg == "" {
			return nil, fmt.Errorf("empty set of characters")
		}
		return ok(func(s string) string { return trim(s, arg) })
	}
}

// cutPrefix removes every leading repetition of arg, so the result is the
// same when applied twice.
func cutPrefix(arg string) (CheckFunc, error) {
	if arg == "" {
		return nil, fmt.Errorf("empty prefix")
	}
	return ok(func(s string) string {
		for strings.HasPrefix(s, arg) {
			s = s[len(arg):]
		}
		return s
	})
}

func cutSuffix(arg string) (CheckFunc, error) {
	if arg == "" {
		return nil, fmt.Errorf("empty suffix")
	}
	return ok(func(s string) string {
		for strings.HasSuffix(s, arg) {
			s = s[:len(s)-len(arg)]
		}
		return s
	})
}

// classes are the character classes accepted by keep and remove.
var classes = map[string]func(rune) bool{
	"letter":    unicode.IsLetter,
	"upper":     unicode.IsUpper,
	"lower":     unicode.IsLower,
	"digit":     unicode.IsDigit,
	"number":    unicode.IsNumber,
	"space":     unicode.IsSpace,
	"punct":     unicode.IsPunct,
	"symbol":    unicode.IsSymbol,
	"mark":      unicode.IsMark,
	"control":   isControl,
	"invisible": func(r rune) bool { return unicode.Is(unicode.Cf, r) },
	"ascii":     func(r rune) bool { return r < utf8.RuneSelf },
	"print":     unicode.IsPrint,
}

func classNames() string {
	names := make([]string, 0, len(classes))
	for name := range classes {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func classFilter(keep bool) ParamFunc {
	return func(arg string) (CheckFunc, error) {
		var preds []func(rune) bool
		for name := range strings.SplitSeq(arg, "|") {
			pred, found := classes[name]
			if !found {
				return nil, fmt.Errorf("unknown class %q; known classes: %s", name, classNames())
			}
			preds = append(preds, pred)
		}
		match := func(r rune) bool {
			for _, p := range preds {
				if p(r) {
					return true
				}
			}
			return false
		}
		return ok(func(s string) string { return filter(s, match, keep) })
	}
}

// filter keeps the runes that match (keep) or drops the runes that match
// (!keep). Bytes that are not valid UTF-8 are always dropped: kept around a
// removed rune, two of them could join into a valid one.
func filter(s string, match func(rune) bool, keep bool) string {
	drop := func(r rune, size int) bool {
		if r == utf8.RuneError && size == 1 {
			return true
		}
		return match(r) != keep
	}
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if drop(r, size) {
			break
		}
		i += size
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !drop(r, size) {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

func isSpace(r rune) bool { return unicode.IsSpace(r) }

// isControl reports control characters (category Cc) other than white space,
// so that tab and line breaks belong to the space class only.
func isControl(r rune) bool { return unicode.IsControl(r) && !unicode.IsSpace(r) }

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// isLineBreak reports the white space characters that end a line.
func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\v', '\f', '\r', '\u0085', '\u2028', '\u2029':
		return true
	}
	return false
}

func collapseParam(arg string) (CheckFunc, error) {
	if arg != "inline" {
		return nil, fmt.Errorf("unknown mode %q; the only mode is inline", arg)
	}
	return ok(func(s string) string {
		return collapse(s, func(r rune) bool { return isSpace(r) && !isLineBreak(r) })
	})
}

// collapse replaces every run of runes matching space with a single ASCII
// space. It does not trim; combine it with trim for that. Other bytes,
// including invalid UTF-8, are copied unchanged.
func collapse(s string, space func(rune) bool) string {
	if !needsCollapse(s, space) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if space(r) {
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
func needsCollapse(s string, space func(rune) bool) bool {
	prevSpace := false
	for _, r := range s {
		sp := space(r)
		if sp && (prevSpace || r != ' ') {
			return true
		}
		prevSpace = sp
	}
	return false
}

// newline converts CRLF, CR, NEL, LINE SEPARATOR and PARAGRAPH SEPARATOR to
// LF.
func newline(s string) string {
	if !strings.ContainsAny(s, "\r\u0085\u2028\u2029") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\r':
			b.WriteByte('\n')
			if i+1 < len(s) && s[i+1] == '\n' {
				size++
			}
		case '\u0085', '\u2028', '\u2029':
			b.WriteByte('\n')
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// capfirst converts the first rune to title case and leaves the rest as is.
func capfirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 || (r == utf8.RuneError && size == 1) {
		return s
	}
	t := unicode.ToTitle(r)
	if t == r {
		return s
	}
	return string(t) + s[size:]
}

// slug lower-cases s, keeps letters, digits and the marks attached to them,
// and replaces every other run of characters with a single hyphen, without
// leading or trailing hyphens.
func slug(s string) string {
	if isSlug(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	pending := false
	for _, r := range s {
		word := unicode.IsLetter(r) || unicode.IsDigit(r)
		attached := unicode.IsMark(r) && b.Len() > 0 && !pending
		if !word && !attached {
			pending = true
			continue
		}
		if pending && b.Len() > 0 {
			b.WriteByte('-')
		}
		pending = false
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// isSlug reports whether slug would return s unchanged: lower-case words of
// letters, digits and attached marks, joined by single hyphens.
func isSlug(s string) bool {
	prev := '-' // a mark or hyphen may not start s
	for _, r := range s {
		switch {
		case r == '-' || unicode.IsMark(r):
			if prev == '-' {
				return false
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
		default:
			return false
		}
		if unicode.ToLower(r) != r {
			return false
		}
		prev = r
	}
	return prev != '-'
}

// email trims s and lower-cases the domain after the last "@". The local part
// is left as is: it is case-sensitive by the standard, though many providers
// ignore case. Internationalized domains are not converted to their ASCII
// (IDNA) form, and the address is not validated.
func email(s string) string {
	s = strings.TrimSpace(s)
	at := strings.LastIndexByte(s, '@')
	if at < 0 {
		return s
	}
	domain := strings.ToLower(s[at+1:])
	if domain == s[at+1:] {
		return s
	}
	return s[:at+1] + domain
}

func truncateParam(arg string) (CheckFunc, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 {
		return nil, fmt.Errorf("want a positive number of characters, got %q", arg)
	}
	return ok(func(s string) string { return truncate(s, n) })
}

// truncate keeps the first n runes of s. A byte that is not valid UTF-8
// counts as one rune.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := 0
	for range n {
		if i >= len(s) {
			return s
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

// caseStyle is an identifier style accepted by case: the separator between
// words and how each word is written, given its position.
type caseStyle struct {
	sep   string
	write func(b *strings.Builder, i int, w string)
}

var caseStyles = map[string]caseStyle{
	"snake":    {"_", writeLower},
	"kebab":    {"-", writeLower},
	"dot":      {".", writeLower},
	"constant": {"_", writeUpper},
	"train":    {"-", writeTitle},
	"pascal":   {"", writeInitial},
	"camel": {"", func(b *strings.Builder, i int, w string) {
		if i == 0 {
			writeLower(b, i, w)
		} else {
			writeInitial(b, i, w)
		}
	}},
}

func writeLower(b *strings.Builder, _ int, w string) {
	for _, r := range w {
		b.WriteRune(unicode.ToLower(r))
	}
}

func writeUpper(b *strings.Builder, _ int, w string) {
	for _, r := range w {
		b.WriteRune(unicode.ToUpper(r))
	}
}

func writeTitle(b *strings.Builder, i int, w string) {
	r, size := utf8.DecodeRuneInString(w)
	b.WriteRune(unicode.ToUpper(r))
	writeLower(b, i, w[size:])
}

// writeInitial title-cases a word for camel and pascal but keeps a word with
// no lower case letters, such as ID or HTTP, in upper case. Without
// separators "AB" could be one word or two, so lower-casing it would not
// survive a second pass.
func writeInitial(b *strings.Builder, i int, w string) {
	if strings.ContainsFunc(w, unicode.IsLower) {
		writeTitle(b, i, w)
	} else {
		writeUpper(b, i, w)
	}
}

func caseParam(arg string) (CheckFunc, error) {
	style, found := caseStyles[arg]
	if !found {
		names := make([]string, 0, len(caseStyles))
		for name := range caseStyles {
			names = append(names, name)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("unknown style %q; known styles: %s", arg, strings.Join(names, ", "))
	}
	return ok(func(s string) string {
		// Without separators, words can read differently once converted
		// ("000 A" becomes "000A", one word), so convert until the result
		// is stable, which keeps the operation idempotent.
		for range maxCasePasses {
			next := style.apply(s)
			if next == s {
				break
			}
			s = next
		}
		return s
	})
}

const maxCasePasses = 4

func (st caseStyle) apply(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	eachWord(s, func(i int, w string) {
		if i > 0 {
			b.WriteString(st.sep)
		}
		st.write(&b, i, w)
	})
	return b.String()
}

// eachWord calls fn with each word of s, in order, for case. Any character
// other than a letter, digit or mark separates words. If s has lower case
// letters, changes of case separate words too: a lower case letter or digit
// followed by an upper case letter ("prepTime", "v2Api"), and the last upper
// case letter of a run followed by a lower case one ("HTTPServer" splits
// into "HTTP" and "Server"). Text without lower case letters, such as
// IN_PROGRESS, has no case boundaries. Words are substrings of s, so
// splitting does not allocate.
func eachWord(s string, fn func(i int, w string)) {
	byCase := strings.ContainsFunc(s, unicode.IsLower)
	n := 0
	emit := func(w string) {
		fn(n, w)
		n++
	}
	start := -1               // byte offset of the current word, or -1
	var prev, prev2 rune      // the two runes before r within the word
	prevAt, prev2At := -1, -1 // their byte offsets
	for i, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
			if start >= 0 {
				emit(s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start, prev, prevAt, prev2At = i, r, i, -1
			continue
		}
		if byCase {
			switch {
			case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
				emit(s[start:i])
				start = i
			case unicode.IsLower(r) && unicode.IsUpper(prev) && prev2At >= start && unicode.IsUpper(prev2):
				emit(s[start:prevAt])
				start = prevAt
			}
		}
		prev2, prev2At = prev, prevAt
		prev, prevAt = r, i
	}
	if start >= 0 {
		emit(s[start:])
	}
}
