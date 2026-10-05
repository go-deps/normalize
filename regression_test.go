package normalize_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/go-deps/normalize"
)

type cleanElem struct {
	S     string `normalize:"trim"`
	calls *int
}

func (e *cleanElem) AfterNormalize() error {
	if e.calls != nil {
		*e.calls++
	}
	return nil
}

func TestSharedMapWithHookElementsIsOnlyRead(t *testing.T) {
	shared := map[string]cleanElem{"a": {S: "a"}, "b": {S: "b"}}
	type holder struct{ M map[string]cleanElem }
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				h := holder{M: shared}
				if err := normalize.Struct(&h); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()

	calls := 0
	dirty := map[string]cleanElem{"a": {S: " a ", calls: &calls}}
	if err := normalize.Var(&dirty, ""); err != nil || dirty["a"].S != "a" || calls != 1 {
		t.Errorf("a changed element is stored back once: %+v %d %v", dirty, calls, err)
	}
}

func TestLongMapKeysDoNotAmplifyErrors(t *testing.T) {
	n := normalize.New()
	if err := n.RegisterCheck("reject", func(s string) (string, error) { return s, errNotAllowed }); err != nil {
		t.Fatal(err)
	}
	m := map[string]string{strings.Repeat("k", 1<<20): ""}
	for i := range 150 {
		m[fmt.Sprintf("%d%s", i, strings.Repeat("x", 8<<10))] = ""
	}
	err := n.Var(&m, "dive,reject")
	if !errors.Is(err, errNotAllowed) || !errors.Is(err, normalize.ErrTooManyErrors) {
		t.Fatalf("got %.200v", err)
	}
	if size := len(err.Error()); size >= 64<<10 {
		t.Errorf("error text is %d bytes", size)
	}
	var fe *normalize.FieldError
	if errors.As(err, &fe) && (len(fe.Path) > 520 || !strings.Contains(fe.Path, "…")) {
		t.Errorf("path not shortened: %d bytes", len(fe.Path))
	}
}

func TestOwnHookWithNilEmbeddedPointer(t *testing.T) {
	in := Override{}
	if err := normalize.Struct(&in); err != nil || in.Count != 1 {
		t.Errorf("a declared hook is called even if an embedded pointer is nil: %d %v", in.Count, err)
	}
}

type XHook struct{ calls *int }

func (x *XHook) AfterNormalize() error { *x.calls++; return nil }

type YNil struct{ *Recipe }

type shadowOuter struct {
	XHook // its hook, at depth 1, shadows the one of Recipe, at depth 2
	YNil
	S string `normalize:"trim"`
}

func TestShallowHookShadowsNilDeeperPath(t *testing.T) {
	calls := 0
	in := shadowOuter{XHook: XHook{calls: &calls}, S: " s "}
	if err := normalize.Struct(&in); err != nil || calls != 1 || in.S != "s" {
		t.Errorf("calls = %d, S = %q, err = %v", calls, in.S, err)
	}
}

type ifaceOuter struct {
	normalize.AfterNormalizer
	S string `normalize:"trim"`
}

func TestEmbeddedInterfaceHook(t *testing.T) {
	in := ifaceOuter{S: " s "}
	if err := normalize.Struct(&in); err != nil || in.S != "s" {
		t.Errorf("a nil embedded interface is skipped: %q %v", in.S, err)
	}
	r := &Recipe{Title: "Hot Soup"}
	set := ifaceOuter{AfterNormalizer: r}
	if err := normalize.Struct(&set); err != nil || r.Slug != "hot-soup" {
		t.Errorf("a set embedded interface is called: %q %v", r.Slug, err)
	}
}

type midHook struct{ *Recipe }

func TestHookGuardThroughTwoEmbeddedPointers(t *testing.T) {
	type top struct {
		*midHook
		X string `normalize:"trim"`
	}
	in := top{midHook: &midHook{}, X: " x "}
	if err := normalize.Struct(&in); err != nil || in.X != "x" {
		t.Errorf("%q %v", in.X, err)
	}
}

func TestZeroValues(t *testing.T) {
	var n normalize.Normalizer
	s := " s "
	in := struct{ S string }{}
	for name, err := range map[string]error{
		"Struct":        n.Struct(&in),
		"Var":           n.Var(&s, "trim"),
		"Register":      n.Register("x", strings.TrimSpace),
		"RegisterParam": n.RegisterParam("y", repeat),
	} {
		if !errors.Is(err, normalize.ErrInvalidArgument) {
			t.Errorf("%s on a zero Normalizer: %v", name, err)
		}
	}
	if _, err := normalize.CompileWith[string](&n, "trim"); !errors.Is(err, normalize.ErrInvalidArgument) {
		t.Errorf("CompileWith on a zero Normalizer: %v", err)
	}
	if _, err := normalize.CompileWith[string](nil, "trim"); !errors.Is(err, normalize.ErrInvalidArgument) {
		t.Errorf("CompileWith on nil: %v", err)
	}
	var r normalize.Rule[string]
	if err := r.Var(&s); !errors.Is(err, normalize.ErrInvalidArgument) || s != " s " {
		t.Errorf("zero Rule: %v", err)
	}
}

func TestMaxDepthIsCapped(t *testing.T) {
	type chain struct{ Next *chain }
	head := &chain{}
	cur := head
	for range 60000 {
		cur.Next = &chain{}
		cur = cur.Next
	}
	if err := normalize.New(normalize.WithMaxDepth(1 << 30)).Struct(head); !errors.Is(err, normalize.ErrTooDeep) {
		t.Errorf("got %v, want ErrTooDeep past 100000 levels", err)
	}
}

func TestMaxDepthBoundary(t *testing.T) {
	s := " s "
	p := &s
	n := normalize.New(normalize.WithMaxDepth(2))
	if err := n.Var(&p, "trim"); err != nil || s != "s" {
		t.Errorf("depth 2 within limit 2: %q %v", s, err)
	}
	pp := &p
	if err := n.Var(&pp, ""); !errors.Is(err, normalize.ErrTooDeep) {
		t.Errorf("depth 3 over limit 2: %v", err)
	}
}

func TestMustCompilePanicsWithTagError(t *testing.T) {
	defer func() {
		te, ok := recover().(*normalize.TagError)
		if !ok || !errors.Is(te, normalize.ErrUnknownOperation) {
			t.Errorf("got %v, want a *TagError", te)
		}
	}()
	normalize.MustCompile[string]("nosuchop")
}

func TestFieldErrorHasOnePrefix(t *testing.T) {
	in := struct {
		O lateOpt
	}{O: lateOpt{v: &lateBad{}}}
	err := normalize.Struct(&in)
	if err == nil || strings.Count(err.Error(), "normalize: ") != 1 {
		t.Errorf("got %v", err)
	}
}

func TestEmailUsesLastAt(t *testing.T) {
	if got, _ := normalize.Apply("Ab@Cd@Example.COM", "email"); got != "Ab@Cd@example.com" {
		t.Errorf("got %q", got)
	}
}

func TestSlugDropsOrphanMark(t *testing.T) {
	for in, want := range map[string]string{"a \u0301b": "a-b", "\u0301a": "a"} {
		if got, _ := normalize.Apply(in, "slug"); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugOfNearlyCleanInput(t *testing.T) {
	for in, want := range map[string]string{
		"crème-brûlée": "crème-brûlée",
		"a\u0301-b2":   "a\u0301-b2",
		"a--b":         "a-b",
		"-a":           "a",
		"a-":           "a",
		"a-\u0301b":    "a-b",
		"Ab":           "ab",
		"a_b":          "a-b",
		"a\xffb":       "a-b",
	} {
		if got, _ := normalize.Apply(in, "slug"); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCamelJoinsDigitRuns(t *testing.T) {
	if got, _ := normalize.Apply("0 00A", "case=camel"); got != "000a" {
		t.Errorf("got %q", got)
	}
}

func TestSubslicesOfOneArrayAreWalked(t *testing.T) {
	items := []Item{{Name: " a "}, {Name: " b "}}
	in := struct{ A, B []Item }{A: items[:1], B: items[:2]}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if items[0].Name != "a" || items[1].Name != "b" {
		t.Errorf("got %q %q", items[0].Name, items[1].Name)
	}
}

func TestSingleErrorIsFieldError(t *testing.T) {
	err := normalize.Struct(&Recipe{})
	if _, ok := err.(*normalize.FieldError); !ok { //nolint:errorlint // the error must not be wrapped
		t.Errorf("got %T", err)
	}
}

func TestRegisterDiscardsValuePlans(t *testing.T) {
	n := normalize.New()
	if _, err := normalize.ApplyWith(n, "x", "shout"); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Fatal(err)
	}
	if err := n.Register("shout", strings.ToUpper); err != nil {
		t.Fatal(err)
	}
	if got, err := normalize.ApplyWith(n, "x", "shout"); err != nil || got != "X" {
		t.Errorf("%q %v", got, err)
	}
}

func TestMoreTagNegatives(t *testing.T) {
	for _, tc := range []struct {
		ptr any
		tag string
	}{
		{new([]string), "nilempty,nilempty"},
		{new(string), "cutsuffix="},
		{new(string), "trim)"},
		{new(string), "trim'x'"},
		{new(bool), "default=maybe"},
		{new(uint8), "default=-1"},
		{new(float32), "default=x"},
	} {
		if err := normalize.Var(tc.ptr, tc.tag); !errors.Is(err, normalize.ErrMalformedTag) {
			t.Errorf("%s: %v", tc.tag, err)
		}
	}
	if err := normalize.New().RegisterParam("dive", repeat); err == nil {
		t.Error("RegisterParam reserved name: want error")
	}
	bad := testPlugin{name: "badp", param: map[string]normalize.ParamFunc{"a b": repeat}}
	if err := normalize.New().Use(bad); err == nil {
		t.Error("Registry.RegisterParam invalid name: want error")
	}
}
