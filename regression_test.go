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
		{new(float64), "default=NaN"},
		{new(float64), "default=Inf"},
		{new(float64), "default=-Inf"},
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
	type taggedUnexportedEmbedded struct {
		*embedded `normalize:"nilempty"`
	}
	if err := normalize.Struct(&taggedUnexportedEmbedded{}); !errors.Is(err, normalize.ErrUnsupportedField) {
		t.Errorf("tagged unexported embedded field: %v", err)
	}
}

func TestUnwrapperDiveDirectives(t *testing.T) {
	type in struct {
		Ints    Optional[[]int]                `normalize:"dive,default=42"`
		Strs    Optional[[]string]             `normalize:"dive,trim,default=n/a"`
		Ptrs    Optional[[]*string]            `normalize:"dive,default=ok"`
		NilPtrs Optional[[]*string]            `normalize:"dive,trim,nilempty"`
		Labels  Optional[[1]map[string]string] `normalize:"dive,keys(trim,lower),dive,trim"`
	}
	v := in{
		Ints:    some([]int{0, 7}),
		Strs:    some([]string{"  ", "x"}),
		Ptrs:    some([]*string{nil, ptr("y")}),
		NilPtrs: some([]*string{ptr("  "), ptr(" z ")}),
		Labels:  some([1]map[string]string{{" EN ": " Hi "}}),
	}
	if err := normalize.Struct(&v); err != nil {
		t.Fatal(err)
	}
	if v.Ints.Value[0] != 42 || v.Ints.Value[1] != 7 {
		t.Errorf("Ints = %v", v.Ints.Value)
	}
	if v.Strs.Value[0] != "n/a" || v.Strs.Value[1] != "x" {
		t.Errorf("Strs = %q", v.Strs.Value)
	}
	if *v.Ptrs.Value[0] != "ok" || *v.Ptrs.Value[1] != "y" {
		t.Errorf("Ptrs = %v", v.Ptrs.Value)
	}
	if v.NilPtrs.Value[0] != nil || *v.NilPtrs.Value[1] != "z" {
		t.Errorf("NilPtrs = %v", v.NilPtrs.Value)
	}
	if v.Labels.Value[0]["en"] != "Hi" {
		t.Errorf("Labels = %v", v.Labels.Value)
	}

	type badKeys struct {
		M Optional[[1]map[int]string] `normalize:"dive,keys(lower)"`
	}
	if err := normalize.Struct(&badKeys{M: some([1]map[int]string{{1: "a"}})}); !errors.Is(err, normalize.ErrUnsupportedField) {
		t.Errorf("dive,keys on int-keyed map in Unwrapper: %v", err)
	}
	type badNilEmpty struct {
		S Optional[[]string] `normalize:"dive,nilempty"`
	}
	if err := normalize.Struct(&badNilEmpty{S: some([]string{"a"})}); !errors.Is(err, normalize.ErrUnsupportedField) {
		t.Errorf("dive,nilempty on strings in Unwrapper: %v", err)
	}
	type badDef struct {
		N Optional[[]int] `normalize:"dive,default=notanint"`
	}
	if err := normalize.Struct(&badDef{N: some([]int{0})}); !errors.Is(err, normalize.ErrMalformedTag) {
		t.Errorf("dive,default=notanint on ints in Unwrapper: %v", err)
	}
}

func TestSharedMapWithIndirectHooksIsOnlyRead(t *testing.T) {
	type ptrHolder struct{ C *cleanElem }
	sharedPtr := map[string]ptrHolder{"a": {C: &cleanElem{S: "a"}}}
	sharedSlice := map[string][]cleanElem{"a": {{S: "a"}}}
	sharedMap := map[string]map[string]cleanElem{"a": {"b": {S: "a"}}}
	sharedOpt := map[string]Optional[cleanElem]{"a": some(cleanElem{S: "a"})}
	type root struct {
		P map[string]ptrHolder
		S map[string][]cleanElem
		M map[string]map[string]cleanElem
		O map[string]Optional[cleanElem]
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				r := root{P: sharedPtr, S: sharedSlice, M: sharedMap, O: sharedOpt}
				if err := normalize.Struct(&r); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestEmbeddedInterfaceTypedNilHook(t *testing.T) {
	var r *Recipe
	in := ifaceOuter{AfterNormalizer: r, S: " s "}
	if err := normalize.Struct(&in); err != nil || in.S != "s" {
		t.Errorf("typed nil in embedded interface must be skipped: %q %v", in.S, err)
	}
	type outerNilPtr struct{ *Recipe }
	in2 := ifaceOuter{AfterNormalizer: &outerNilPtr{}, S: " s "}
	if err := normalize.Struct(&in2); err != nil || in2.S != "s" {
		t.Errorf("struct with nil promoted hook in embedded interface must be skipped: %q %v", in2.S, err)
	}
}

type (
	expHookA struct{ calls *int }
	expHookB struct{ calls *int }
	ambigMid struct {
		expHookA
		expHookB
	}
	outerOverAmbig struct {
		ambigMid
		calls *int
	}
)

func (h *expHookA) AfterNormalize() error { *h.calls++; return nil }
func (h *expHookB) AfterNormalize() error { *h.calls++; return nil }
func (o *outerOverAmbig) AfterNormalize() error {
	*o.calls++
	return nil
}

func TestOuterHookSuppressesDeeperAmbiguousHooks(t *testing.T) {
	aCalls, bCalls, outCalls := 0, 0, 0
	in := outerOverAmbig{
		ambigMid: ambigMid{expHookA: expHookA{calls: &aCalls}, expHookB: expHookB{calls: &bCalls}},
		calls:    &outCalls,
	}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if aCalls != 0 || bCalls != 0 || outCalls != 1 {
		t.Errorf("got calls a=%d b=%d outer=%d, want 0 0 1", aCalls, bCalls, outCalls)
	}
}

func TestSharedEmbeddedPointerHooks(t *testing.T) {
	type promoted struct{ *Recipe }
	calls := 0
	r := &Recipe{Title: " Soup ", calls: &calls}
	slice := []promoted{{Recipe: r}, {Recipe: r}}
	if err := normalize.Var(&slice, ""); err != nil || calls != 1 || r.Title != "Soup" {
		t.Errorf("shared promoted pointer across slice: calls=%d title=%q err=%v", calls, r.Title, err)
	}

	calls = 0
	r = &Recipe{Title: " Soup ", calls: &calls}
	in := struct {
		Override
		Other *Recipe
	}{Override: Override{Recipe: r}, Other: r}
	if err := normalize.Struct(&in); err != nil || calls != 1 || in.Count != 1 {
		t.Errorf("shared pointer in Override and sibling field: recipeCalls=%d overrideCount=%d err=%v", calls, in.Count, err)
	}

	calls = 0
	r = &Recipe{Title: " Soup ", calls: &calls}
	in2 := struct {
		Other *Recipe
		*Recipe
	}{Other: r, Recipe: r}
	if err := normalize.Struct(&in2); err != nil || calls != 1 {
		t.Errorf("shared pointer in sibling then promoted field: calls=%d err=%v", calls, err)
	}
}

type (
	provA struct {
		B1, B2 *provB
		Bad    string `normalize:"nosuchop"`
	}
	provB struct {
		A *provA
	}
)

func TestCyclicPointerToPointer(t *testing.T) {
	type cyclicPtr *cyclicPtr
	var p cyclicPtr
	p = &p
	if err := normalize.Var(&p, ""); err != nil {
		t.Errorf("cyclic pointer: %v", err)
	}
	if err := normalize.Struct(&provA{}); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Errorf("provisional plan reuse: %v", err)
	}
}

func TestCaseCleanDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	type cased struct {
		Snake    string `normalize:"case=snake"`
		Kebab    string `normalize:"case=kebab"`
		Dot      string `normalize:"case=dot"`
		Constant string `normalize:"case=constant"`
		Train    string `normalize:"case=train"`
		Pascal   string `normalize:"case=pascal"`
		Camel    string `normalize:"case=camel"`
	}
	in := cased{
		Snake:    "prep_time_v2",
		Kebab:    "prep-time-v2",
		Dot:      "prep.time.v2",
		Constant: "PREP_TIME_V2",
		Train:    "Prep-Time-V2",
		Pascal:   "PrepTimeID",
		Camel:    "prepTimeID",
	}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = normalize.Struct(&in) }); allocs != 0 {
		t.Errorf("case= on clean input: %v allocations", allocs)
	}
}
