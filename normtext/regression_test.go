package normtext_test

import (
	"testing"
	"unicode"

	"github.com/go-deps/normalize"
	"github.com/go-deps/normalize/normtext"
	"golang.org/x/text/unicode/norm"
)

func TestNFCKeepsCompatibilityCharacters(t *testing.T) {
	if got, _ := normalize.ApplyWith(n, "ﬁ①e\u0301", "nfc"); got != "ﬁ①\u00e9" {
		t.Errorf("got %q", got)
	}
}

// x/text rejects "" for nickname and password, so username alone does not
// test the guard.
func TestPrecisEmptyForEveryProfile(t *testing.T) {
	for _, p := range []string{"username", "username-preserved", "nickname", "password"} {
		if got, err := normalize.ApplyWith(n, "", "precis="+p); err != nil || got != "" {
			t.Errorf("%s: %q %v", p, got, err)
		}
	}
}

func TestUsePluginTwice(t *testing.T) {
	m := normalize.New()
	if err := m.Use(normtext.Plugin(), normtext.Plugin()); err != nil {
		t.Fatal(err)
	}
	if err := m.Use(normtext.Plugin()); err != nil {
		t.Errorf("using the plugin again: %v", err)
	}
}

var forms = map[string]norm.Form{"nfc": norm.NFC, "nfd": norm.NFD, "nfkc": norm.NFKC, "nfkd": norm.NFKD}

// checkOracles states what the result of an operation must look like, beyond
// idempotence.
func checkOracles(t *testing.T, tag, in, out string) {
	t.Helper()
	if f, ok := forms[tag]; ok && out != f.String(in) {
		t.Errorf("%s(%q) = %q, want %q", tag, in, out, f.String(in))
	}
	if tag == "unaccent" {
		for _, r := range out {
			if unicode.Is(unicode.Mn, r) {
				t.Errorf("unaccent(%q) = %q keeps the mark %U", in, out, r)
				break
			}
		}
	}
}

func TestFormsMatchXText(t *testing.T) {
	for _, s := range []string{"ﬁ①e\u0301", "Å", "\u1e9b\u0323", "Crème brûlée"} {
		for _, tag := range []string{"nfc", "nfd", "nfkc", "nfkd", "unaccent"} {
			out, err := normalize.ApplyWith(n, s, tag)
			if err != nil {
				t.Fatal(err)
			}
			checkOracles(t, tag, s, out)
		}
	}
}
