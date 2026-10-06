package normtext_test

import (
	"strings"
	"testing"

	"github.com/go-deps/normalize"
)

var fuzzTags = []string{
	"nfc", "nfd", "nfkc", "nfkd", "fold", "unaccent",
	"width=narrow", "width=wide", "width=fold",
	"title", "title=nl", "title=tr", "lower=tr", "lower=lt", "upper=el", "upper=tr",
	"precis=username", "precis=username-preserved", "precis=nickname", "precis=password",
}

// FuzzOperations checks that every operation is idempotent and that only
// precis rejects input.
func FuzzOperations(f *testing.F) {
	for _, s := range []string{"", " a ", "Crème brûlée", "İSTANBUL", "ijsselmeer", "ＡＢＣｶﾞ", "ﬁ①", "ǆ", "\xff\xfe", "a\u0301\u0301"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, tag := range fuzzTags {
			once, err := normalize.ApplyWith(n, s, tag)
			if err != nil {
				if !strings.HasPrefix(tag, "precis") {
					t.Fatalf("%s(%q): %v", tag, s, err)
				}
				continue
			}
			twice, err := normalize.ApplyWith(n, once, tag)
			if err != nil || once != twice {
				t.Errorf("%s not idempotent: %q -> %q -> %q, %v", tag, s, once, twice, err)
			}
			checkOracles(t, tag, s, once)
		}
	})
}
