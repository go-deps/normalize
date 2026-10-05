// Package normtext adds Unicode text operations to
// [github.com/go-deps/normalize]: normalization forms, case folding,
// language-aware casing, accent removal, width mapping and the PRECIS
// profiles of RFC 8264, RFC 8265 and RFC 8266. It depends on
// golang.org/x/text, which is why it is a separate module.
//
// The operations become available to a [normalize.Normalizer] through its
// plugin, with [normalize.Use] or [normalize.WithPlugins]:
//
//	normalize.Use(normtext.Plugin()) // the package-level Normalizer
//	n := normalize.New(normalize.WithPlugins(normtext.Plugin()))
//
// and are then used in tags like the built-in ones:
//
//	type Account struct {
//		Username string `normalize:"trim,precis=username"`
//		Name     string `normalize:"trim,nfc,collapse"`
//		City     string `normalize:"trim,unaccent,fold"`
//	}
//
// The operations are:
//
//	nfc, nfd, nfkc, nfkd   Unicode normalization forms
//	fold                   full Unicode case folding, for caseless matching
//	unaccent               remove non-spacing marks: "Crème brûlée" -> "Creme brulee"
//	width=narrow           map wide (full-width) characters to their narrow forms
//	width=wide             map narrow (half-width) characters to their wide forms
//	width=fold             map both to the canonical width, as East Asian text expects
//	title, title=<lang>    title case for the language; without one, language-neutral rules
//	lower=<lang>           lower case for the language, as in lower=tr
//	upper=<lang>           upper case for the language, as in upper=el
//	precis=<profile>       PRECIS enforcement: username, username-preserved,
//	                       nickname or password
//
// Languages are BCP 47 tags such as tr, nl or pt-BR; an invalid tag is a
// *[normalize.TagError]. The plain lower and upper of the core package are
// language-independent and stay available.
//
// precis is the only operation that rejects input: a string the profile does
// not allow is reported in a *[normalize.FieldError] and left unchanged. An
// empty string is left empty rather than rejected, so that optional fields
// stay optional; requiring a value is the job of validation. The password
// profile maps non-ASCII spaces to U+0020 and applies NFC but keeps case;
// apply it the same way when a password is set and when it is checked, and
// store only a hash of the result. The nickname profile keeps case too. The
// username profiles do not detect characters that look alike across
// scripts (Unicode TS #39); that needs a separate check.
//
// nfkc, nfkd and precis=nickname can make a string much longer: U+FDFA
// becomes 18 characters. Limit input sizes before normalizing.
//
// Language-aware casing is slower than the core's lower and upper; use it
// only where the language matters.
//
// Every operation is idempotent and safe for concurrent use.
package normtext

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"github.com/go-deps/normalize"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/secure/precis"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"
)

const pluginName = "github.com/go-deps/normalize/normtext"

// Plugin returns the plugin that adds the operations of this package to a
// [normalize.Normalizer]. Its name is the module path, and every value it
// returns is equal, so using it twice on the same Normalizer has no further
// effect.
func Plugin() normalize.Plugin { return plugin{} }

type plugin struct{}

func (plugin) Name() string { return pluginName }

func (plugin) Register(r *normalize.Registry) error {
	return errors.Join(
		r.Register("nfc", norm.NFC.String),
		r.Register("nfd", norm.NFD.String),
		r.Register("nfkc", norm.NFKC.String),
		r.Register("nfkd", norm.NFKD.String),
		r.Register("fold", pooled(func() cases.Caser { return cases.Fold() }, upperCherokee, true)),
		r.Register("unaccent", unaccent),
		r.Register("title", pooled(func() cases.Caser { return cases.Title(language.Und) }, nil, true)),
		r.RegisterParam("title", caserParam(func(t language.Tag) cases.Caser { return cases.Title(t) }, false)),
		r.RegisterParam("lower", caserParam(func(t language.Tag) cases.Caser { return cases.Lower(t) }, true)),
		r.RegisterParam("upper", caserParam(func(t language.Tag) cases.Caser { return cases.Upper(t) }, false)),
		r.RegisterParam("width", widthParam),
		r.RegisterParam("precis", precisParam),
	)
}

// pooled returns a Func backed by a pool of casers, as a cases.Caser keeps
// state while it converts a string and must not be shared between
// goroutines.
//
// Some language rules reach their final form only on a second pass: upper=el
// keeps the accent of a decomposed "ΐ" the first time and drops it the next.
// The caser therefore runs until the result is stable, which keeps the
// operation idempotent. post, if not nil, corrects each pass.
//
// With span set, a string the caser reports as already in its final form is
// returned without converting it. Span is only set for casers whose Span
// agrees with String; for some languages it reports text that String still
// changes.
func pooled(newCaser func() cases.Caser, post func(string) string, span bool) normalize.Func {
	pool := &sync.Pool{New: func() any {
		c := newCaser()
		return &c
	}}
	return func(s string) string {
		c := pool.Get().(*cases.Caser)
		defer pool.Put(c)
		if span {
			c.Reset()
			// Span only reads the bytes, so they may share memory with s.
			n, err := c.Span(unsafe.Slice(unsafe.StringData(s), len(s)), true)
			if err == nil && n == len(s) && (post == nil || post(s) == s) {
				return s
			}
			c.Reset()
		}
		for range maxCasePasses {
			next := c.String(s)
			if post != nil {
				next = post(next)
			}
			if next == s {
				break
			}
			s = next
		}
		return s
	}
}

const maxCasePasses = 4

// upperCherokee maps lower case Cherokee letters to upper case after
// cases.Fold. Unicode folds Cherokee to upper case, while cases.Fold in
// golang.org/x/text v0.42.0 swaps the case of Cherokee letters, so that
// folding twice would undo the first fold.
func upperCherokee(s string) string {
	if !strings.ContainsFunc(s, isLowerCherokee) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isLowerCherokee(r) {
			return unicode.ToUpper(r)
		}
		return r
	}, s)
}

func isLowerCherokee(r rune) bool { return unicode.Is(unicode.Cherokee, r) && unicode.IsLower(r) }

func caserParam(newCaser func(language.Tag) cases.Caser, span bool) normalize.ParamFunc {
	return func(arg string) (normalize.CheckFunc, error) {
		tag, err := language.Parse(arg)
		if err != nil {
			return nil, fmt.Errorf("language %q: %w", arg, err)
		}
		fn := pooled(func() cases.Caser { return newCaser(tag) }, nil, span)
		return func(s string) (string, error) { return fn(s), nil }, nil
	}
}

var widths = map[string]width.Transformer{
	"narrow": width.Narrow,
	"wide":   width.Widen,
	"fold":   width.Fold,
}

func widthParam(arg string) (normalize.CheckFunc, error) {
	t, found := widths[arg]
	if !found {
		return nil, fmt.Errorf("unknown width %q; known widths: fold, narrow, wide", arg)
	}
	return func(s string) (string, error) { return t.String(s), nil }, nil
}

var profiles = map[string]*precis.Profile{
	"username":           precis.UsernameCaseMapped,
	"username-preserved": precis.UsernameCasePreserved,
	"nickname":           precis.Nickname,
	"password":           precis.OpaqueString,
}

func precisParam(arg string) (normalize.CheckFunc, error) {
	p, found := profiles[arg]
	if !found {
		return nil, fmt.Errorf("unknown profile %q; known profiles: nickname, password, username, username-preserved", arg)
	}
	return func(s string) (string, error) {
		if s == "" {
			return s, nil
		}
		return p.String(s)
	}, nil
}

// unaccent decomposes s, drops the non-spacing marks (category Mn) and
// composes the rest again. Bytes that are not valid UTF-8 are dropped first:
// decomposition does not reach past them, and kept around a removed mark, two
// of them could join into a valid rune.
func unaccent(s string) string {
	if isASCII(s) || utf8.ValidString(s) && norm.NFD.QuickSpanString(s) == len(s) &&
		!strings.ContainsFunc(s, isMn) && norm.NFC.QuickSpanString(s) == len(s) {
		return s
	}
	d := norm.NFD.String(strings.ToValidUTF8(s, ""))
	var b strings.Builder
	b.Grow(len(d))
	for _, r := range d {
		if !isMn(r) {
			b.WriteRune(r)
		}
	}
	return norm.NFC.String(b.String())
}

func isMn(r rune) bool { return r >= 0x300 && unicode.Is(unicode.Mn, r) }

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
