package normalize_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/go-deps/normalize"
)

type fuzzInput struct {
	Trimmed   string   `normalize:"trim,lower"`
	Collapsed string   `normalize:"collapse"`
	Elems     []string `normalize:"dive,trim"`
}

func FuzzStruct(f *testing.F) {
	for _, s := range []string{"", " a ", "A\tB\n", "\u00a0x\u2003", "привет  мир", "\xff\xfe"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		in := fuzzInput{Trimmed: s, Collapsed: s, Elems: []string{s}}
		if err := normalize.Struct(&in); err != nil {
			t.Fatal(err)
		}
		if want := strings.ToLower(strings.TrimSpace(s)); in.Trimmed != want {
			t.Errorf("trim,lower(%q) = %q, want %q", s, in.Trimmed, want)
		}
		if want := strings.TrimSpace(s); in.Elems[0] != want {
			t.Errorf("dive,trim(%q) = %q, want %q", s, in.Elems[0], want)
		}
		if utf8.ValidString(s) {
			if strings.Contains(in.Collapsed, "  ") || strings.ContainsFunc(in.Collapsed, func(r rune) bool {
				return r != ' ' && unicode.IsSpace(r)
			}) {
				t.Errorf("collapse(%q) = %q still has a run or a non-ASCII space", s, in.Collapsed)
			}
			if strings.Join(strings.Fields(s), " ") != strings.TrimSpace(in.Collapsed) {
				t.Errorf("collapse(%q) = %q changed non-space content", s, in.Collapsed)
			}
		}

		again := in
		again.Elems = []string{in.Elems[0]}
		if err := normalize.Struct(&again); err != nil {
			t.Fatal(err)
		}
		if again.Trimmed != in.Trimmed || again.Collapsed != in.Collapsed || again.Elems[0] != in.Elems[0] {
			t.Errorf("not idempotent: %+v then %+v", in, again)
		}
	})
}

var fuzzTags = []string{
	"trim", "trim=-_", "trimleft", "trimright=.", "cutprefix=ab", "cutsuffix=.0",
	"lower", "upper", "collapse", "collapse=inline", "newline", "capfirst",
	"slug", "email", "digits", "letters", "alnum", "nocontrol",
	"keep=letter|digit", "remove=punct|symbol|invisible", "truncate=5",
	"case=snake", "case=kebab", "case=dot", "case=constant", "case=camel", "case=pascal", "case=train",
}

// fuzzOracles states what the result of an operation must look like, beyond
// idempotence.
var fuzzOracles = map[string]func(in, out string) bool{
	"trim":        func(in, out string) bool { return out == strings.TrimSpace(in) },
	"trim=-_":     func(in, out string) bool { return out == strings.Trim(in, "-_") },
	"trimleft":    func(in, out string) bool { return out == strings.TrimLeftFunc(in, unicode.IsSpace) },
	"trimright=.": func(in, out string) bool { return out == strings.TrimRight(in, ".") },
	"lower":       func(in, out string) bool { return out == strings.ToLower(in) },
	"upper":       func(in, out string) bool { return out == strings.ToUpper(in) },
	"truncate=5":  func(in, out string) bool { return strings.HasPrefix(in, out) && utf8.RuneCountInString(out) <= 5 },
	"digits":      func(in, out string) bool { return isSubsequence(out, in) && onlyRunes(out, unicode.IsDigit) },
	"letters":     func(in, out string) bool { return isSubsequence(out, in) && onlyRunes(out, unicode.IsLetter) },
	"alnum": func(in, out string) bool {
		return isSubsequence(out, in) && onlyRunes(out, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
	},
	"newline": func(in, out string) bool {
		return !strings.ContainsAny(out, "\r\u0085\u2028\u2029") && dropBreaks(in) == dropBreaks(out)
	},
	"nocontrol": func(in, out string) bool {
		return isSubsequence(out, in) && onlyRunes(out, func(r rune) bool { return !unicode.IsControl(r) || unicode.IsSpace(r) })
	},
	"slug":          func(_, out string) bool { return out == strings.ToLower(out) && separated(out, "-") },
	"case=snake":    func(_, out string) bool { return out == strings.ToLower(out) && separated(out, "_") },
	"case=kebab":    func(_, out string) bool { return out == strings.ToLower(out) && separated(out, "-") },
	"case=dot":      func(_, out string) bool { return out == strings.ToLower(out) && separated(out, ".") },
	"case=constant": func(_, out string) bool { return out == strings.ToUpper(out) && separated(out, "_") },
}

func dropBreaks(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("\r\n\u0085\u2028\u2029", r) {
			return -1
		}
		return r
	}, s)
}

// separated reports whether s is words of letters, digits and marks joined
// by single separators.
func separated(s, sep string) bool {
	if s == "" {
		return true
	}
	for w := range strings.SplitSeq(s, sep) {
		if w == "" || !onlyRunes(w, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) }) {
			return false
		}
	}
	return true
}

func isSubsequence(sub, s string) bool {
	for _, r := range sub {
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+utf8.RuneLen(r):]
	}
	return true
}

func onlyRunes(s string, ok func(rune) bool) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return !ok(r) })
}

// FuzzOperations checks that every built-in operation is idempotent, never
// fails on arbitrary input, keeps valid UTF-8 valid and, where an oracle is
// known, produces the expected result.
func FuzzOperations(f *testing.F) {
	for _, s := range []string{"", " a ", "HTTPServer v2Api", "a\r\nb", "café\u0301", "\xff\xfe", "Ǆ x", "ﬁ"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		valid := utf8.ValidString(s)
		for _, tag := range fuzzTags {
			once, err := normalize.Apply(s, tag)
			if err != nil {
				t.Fatalf("%s(%q): %v", tag, s, err)
			}
			twice, err := normalize.Apply(once, tag)
			if err != nil {
				t.Fatalf("%s(%q): %v", tag, once, err)
			}
			if once != twice {
				t.Errorf("%s not idempotent: %q -> %q -> %q", tag, s, once, twice)
			}
			if valid && !utf8.ValidString(once) {
				t.Errorf("%s(%q) = %q is not valid UTF-8", tag, s, once)
			}
			if oracle := fuzzOracles[tag]; oracle != nil && valid && !oracle(s, once) {
				t.Errorf("%s(%q) = %q breaks its oracle", tag, s, once)
			}
		}
	})
}

var tagErrors = []error{
	normalize.ErrUnknownOperation, normalize.ErrMalformedTag, normalize.ErrUnsupportedField,
}

// FuzzTag checks that any tag either compiles or fails with a TagError that
// wraps one of the documented sentinels, that a compiled tag applies without
// failing, and that a quoted argument reaches the operation unchanged.
func FuzzTag(f *testing.F) {
	for _, s := range []string{"trim", "dive,keys(trim),lower", "cutsuffix='''s'", "default='a,b'", "truncate=0", "(", "'", "keys(", ",,", "-"} {
		f.Add(s, "x,y")
	}
	f.Fuzz(func(t *testing.T, tag, arg string) {
		rule, err := normalize.Compile[string](tag)
		if err != nil {
			var te *normalize.TagError
			if !errors.As(err, &te) || !slices.ContainsFunc(tagErrors, func(e error) bool { return errors.Is(err, e) }) {
				t.Errorf("Compile(%q): %v is not a TagError with a known cause", tag, err)
			}
		} else if _, err := rule.Apply(arg); err != nil {
			t.Errorf("Compile(%q).Apply(%q): %v", tag, arg, err)
		}

		quoted := "'" + strings.ReplaceAll(arg, "'", "''") + "'"
		got, err := normalize.Apply("", "default="+quoted)
		if arg == "" || strings.TrimSpace(arg) == "" {
			return
		}
		if err != nil || got != arg {
			t.Errorf("default=%s: got %q, %v; want %q", quoted, got, err, arg)
		}
	})
}
