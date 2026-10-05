package normalize_test

import (
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
