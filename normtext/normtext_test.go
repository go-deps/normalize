package normtext_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/go-deps/normalize"
	"github.com/go-deps/normalize/normtext"
)

var n = normalize.New(normalize.WithPlugins(normtext.Plugin()))

func TestOperations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag, in, want string
	}{
		{"nfc", "e\u0301", "\u00e9"},
		{"nfd", "\u00e9", "e\u0301"},
		{"nfkc", "ﬁ①", "fi1"},
		{"nfkd", "\u00e9ﬁ", "e\u0301fi"},
		{"fold", "Straße ΣΑΣ", "strasse σασ"},
		{"fold", "Ꮼꮼᏸ", "ᏬᏬᏰ"},
		{"unaccent", "Crème brûlée, Ångström", "Creme brulee, Angstrom"},
		{"unaccent", "plain ascii", "plain ascii"},
		{"unaccent", "Ёлка й", "Елка и"},
		{"unaccent", "a\u0301\xffb", "ab"},
		{"width=narrow", "ＡＢＣ　１２３", "ABC 123"},
		{"width=wide", "ABC 123", "ＡＢＣ　１２３"},
		{"width=fold", "ＡＢＣｶﾞ", "ABCカ\u3099"},
		{"title", "hello wORLD", "Hello World"},
		{"title=nl", "ijsselmeer", "IJsselmeer"},
		{"lower=tr", "İSTANBUL", "istanbul"},
		{"lower=en", "İSTANBUL", "i̇stanbul"},
		{"upper=tr", "istanbul", "İSTANBUL"},
		{"upper=el", "άδεια", "ΑΔΕΙΑ"},
		{"upper=el", "ΐ", "Ι"},
		{"precis=username", "Ａlice", "alice"},
		{"precis=username-preserved", "Ａlice", "Alice"},
		{"precis=nickname", "  Ada\u00a0 Lovelace ", "Ada Lovelace"},
		{"precis=password", "pass\u00a0word", "pass word"},
		{"precis=username", "", ""},
	}
	for _, tt := range tests {
		got, err := normalize.ApplyWith(n, tt.in, tt.tag)
		if err != nil || got != tt.want {
			t.Errorf("%s(%q) = %q, %v; want %q", tt.tag, tt.in, got, err, tt.want)
			continue
		}
		if again, _ := normalize.ApplyWith(n, got, tt.tag); again != got {
			t.Errorf("%s not idempotent: %q -> %q -> %q", tt.tag, tt.in, got, again)
		}
	}
}

func TestPrecisRejects(t *testing.T) {
	type Account struct {
		Username string `normalize:"trim,precis=username"`
	}
	in := Account{Username: " ali ce "}
	err := n.Struct(&in)
	var fe *normalize.FieldError
	if !errors.As(err, &fe) || fe.Path != "Username" || fe.Op != "precis=username" {
		t.Fatalf("err = %v, want a FieldError for Username", err)
	}
	if in.Username != " ali ce " {
		t.Errorf("Username = %q, want the field left as it was", in.Username)
	}
}

func TestTagErrors(t *testing.T) {
	for _, tag := range []string{"width=half", "title=", "lower=not a language", "upper=x-1234567890", "precis=email", "nfc=x"} {
		if _, err := normalize.CompileWith[string](n, tag); !errors.Is(err, normalize.ErrMalformedTag) {
			t.Errorf("%s: err = %v, want ErrMalformedTag", tag, err)
		}
	}
}

func TestPlugin(t *testing.T) {
	if got := normtext.Plugin().Name(); got != "github.com/go-deps/normalize/normtext" {
		t.Errorf("Name() = %q", got)
	}
	if err := n.Use(normtext.Plugin()); err != nil {
		t.Errorf("using the plugin again: %v", err)
	}
	if _, err := normalize.Compile[string]("unaccent"); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Errorf("the package-level Normalizer must not have the plugin before Use: %v", err)
	}
	if _, err := normalize.Compile[string]("lower,upper"); err != nil {
		t.Errorf("core plain forms: %v", err)
	}
}

func TestConcurrentCasers(t *testing.T) {
	rule := normalize.MustCompileWith[string](n, "title=tr,lower=tr,fold")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				got, err := rule.Apply("İSTANBUL ışık")
				if err != nil || got != "istanbul ışık" {
					t.Errorf("got %q, %v", got, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
