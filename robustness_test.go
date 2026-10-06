package normalize_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/go-deps/normalize"
)

var hookCalls int

// ambiguous embeds two hooks at one depth, so neither is promoted and each
// embedded value is reached through an unexported field.
//
//nolint:unused // reached only through reflection
type (
	hookA     struct{}
	hookB     struct{}
	ambiguous struct {
		hookA
		hookB
		Name string `normalize:"trim"`
	}
)

func (*hookA) AfterNormalize() error { hookCalls++; return nil } //nolint:unused // see ambiguous

func (*hookB) AfterNormalize() error { hookCalls++; return nil } //nolint:unused // see ambiguous

func TestHookOfUnexportedEmbeddedValueIsSkipped(t *testing.T) {
	hookCalls = 0
	in := ambiguous{Name: " x "}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.Name != "x" || hookCalls != 0 {
		t.Errorf("Name = %q, hook calls = %d", in.Name, hookCalls)
	}
}

func TestHookPromotedThroughNilEmbeddedPointerIsSkipped(t *testing.T) {
	type outer struct {
		*Recipe
		X string `normalize:"trim"`
	}
	in := outer{X: " x "}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.X != "x" {
		t.Errorf("X = %q", in.X)
	}

	type deep struct {
		outer
		Y string `normalize:"trim"`
	}
	d := deep{Y: " y "}
	if err := normalize.Struct(&d); err != nil || d.Y != "y" {
		t.Errorf("through two levels: %q %v", d.Y, err)
	}

	set := outer{Recipe: &Recipe{Title: " Soup "}}
	if err := normalize.Struct(&set); err != nil || set.Title != "Soup" {
		t.Errorf("a non-nil embedded pointer keeps its hook: %+v %v", set.Recipe, err)
	}
}

type counted struct {
	S string `normalize:"trim"`
}

var countedCalls int

func (*counted) AfterNormalize() error { countedCalls++; return nil }

type CountedOpt struct{ v counted }

func (o *CountedOpt) NormalizeTarget() any { return &o.v }

type hookedOuter struct {
	CountedOpt
	Name string `normalize:"trim"`
}

func (*hookedOuter) AfterNormalize() error { return nil }

func TestEmbeddedUnwrapperDoesNotSuppressWrappedHook(t *testing.T) {
	countedCalls = 0
	in := hookedOuter{CountedOpt: CountedOpt{v: counted{S: " s "}}}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.v.S != "s" || countedCalls != 1 {
		t.Errorf("S = %q, hook calls = %d", in.v.S, countedCalls)
	}
}

type zeroHook struct{}

var zeroHookCalls int

func (*zeroHook) AfterNormalize() error { zeroHookCalls++; return nil }

func TestZeroSizeValuesAreNotTakenForRepeats(t *testing.T) {
	zeroHookCalls = 0
	in := struct{ A, B *zeroHook }{A: &zeroHook{}, B: &zeroHook{}}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if zeroHookCalls != 2 {
		t.Errorf("hook calls = %d, want 2", zeroHookCalls)
	}
}

type cyclicSlice struct {
	Name string        `normalize:"trim"`
	Kids []cyclicSlice `normalize:"dive"`
}

type cyclicMap struct {
	Name string                `normalize:"trim"`
	Kids map[string]*cyclicMap `normalize:"dive"`
}

type cyclicArray struct {
	Name string `normalize:"trim"`
	P    *[1]cyclicArray
}

func TestCyclesWithDive(t *testing.T) {
	s := cyclicSlice{Name: " root "}
	s.Kids = []cyclicSlice{{Name: " kid "}}
	s.Kids[0].Kids = s.Kids
	if err := normalize.Struct(&s); err != nil || s.Name != "root" || s.Kids[0].Name != "kid" {
		t.Errorf("slice: %q %q %v", s.Name, s.Kids[0].Name, err)
	}

	m := &cyclicMap{Name: " m "}
	m.Kids = map[string]*cyclicMap{"self": m}
	if err := normalize.Struct(m); err != nil || m.Name != "m" {
		t.Errorf("map: %q %v", m.Name, err)
	}

	var arr [1]cyclicArray
	arr[0] = cyclicArray{Name: " a ", P: &arr}
	in := cyclicArray{P: &arr}
	if err := normalize.Struct(&in); err != nil || arr[0].Name != "a" {
		t.Errorf("pointer to array: %q %v", arr[0].Name, err)
	}
}

func TestCycleBeyondInlineVisits(t *testing.T) {
	nodes := make([]*node, 40)
	for i := range nodes {
		nodes[i] = &node{Name: " n "}
	}
	for i := range 39 {
		nodes[i].Next = nodes[i+1]
	}
	nodes[39].Next = nodes[30]
	if err := normalize.Struct(nodes[0]); err != nil {
		t.Fatal(err)
	}
	for i, n := range nodes {
		if n.Name != "n" {
			t.Fatalf("nodes[%d].Name = %q", i, n.Name)
		}
	}
}

type viaRef struct {
	Name string `normalize:"trim"`
	Ref  ref
}

type ref struct{ p *viaRef }

func (r *ref) NormalizeTarget() any { return r.p }

func TestCycleThroughUnwrapper(t *testing.T) {
	a, b := &viaRef{Name: " a "}, &viaRef{Name: " b "}
	a.Ref.p, b.Ref.p = b, a
	if err := normalize.Struct(a); err != nil || a.Name != "a" || b.Name != "b" {
		t.Errorf("%q %q %v", a.Name, b.Name, err)
	}
}

type loopA struct{ p *loopB }

func (l *loopA) NormalizeTarget() any { return l.p }

type loopB struct{ p *loopA }

func (l *loopB) NormalizeTarget() any { return l.p }

func TestUnwrapperLoopTerminates(t *testing.T) {
	a, b := &loopA{}, &loopB{}
	a.p, b.p = b, a
	in := struct{ L *loopA }{L: a}
	if err := normalize.Struct(&in); err != nil {
		t.Errorf("got %v", err)
	}
}

type sharedDefaults struct {
	Units map[string]benchItem
	Cfg   *benchItem
}

func TestSharedCleanValuesAreOnlyRead(t *testing.T) {
	unit := "g"
	units := map[string]benchItem{"flour": {Name: "flour", Unit: &unit}, "sugar": {Name: "sugar"}}
	cfg := &benchItem{Name: "clean"}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				in := sharedDefaults{Units: units, Cfg: cfg}
				if err := normalize.Struct(&in); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
}

type planA struct {
	B   planB
	Bad string `normalize:"nosuchop"`
}

type planB struct {
	S string `normalize:"trim"`
	A *planA
}

func TestTagErrorDoesNotDependOnParseOrder(t *testing.T) {
	n := normalize.New()
	if err := n.Struct(&planA{}); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Fatalf("planA: %v", err)
	}
	b := planB{S: " x ", A: &planA{}}
	if err := n.Struct(&b); !errors.Is(err, normalize.ErrUnknownOperation) || b.S != " x " {
		t.Errorf("planB after planA: %q %v", b.S, err)
	}
	if err := n.Struct(&planB{}); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Errorf("planB zero value: %v", err)
	}
}

type lateBad struct {
	unknownOp
}

type lateOpt struct{ v *lateBad }

func (o *lateOpt) NormalizeTarget() any { return o.v }

func TestTagErrorBehindUnwrapperAtRunTime(t *testing.T) {
	in := struct {
		N string `normalize:"trim"`
		O lateOpt
	}{N: " n ", O: lateOpt{v: &lateBad{}}}
	err := normalize.Struct(&in)
	var te *normalize.TagError
	var fe *normalize.FieldError
	if !errors.As(err, &te) || !errors.As(err, &fe) || fe.Path != "O" {
		t.Fatalf("got %v, want a FieldError at O wrapping a TagError", err)
	}
	if in.N != "n" {
		t.Errorf("fields walked before the wrapper are normalized: %q", in.N)
	}
}

func TestMapKeyOpError(t *testing.T) {
	in := struct {
		M map[string]string `json:"m" normalize:"keys(trim,noadmin),dive,trim"`
	}{M: map[string]string{" admin ": " x ", " b ": " y "}}
	err := newChecked(t).Struct(&in)
	var fe *normalize.FieldError
	if !errors.As(err, &fe) || fe.Path != `m[" admin "]` || fe.Op != "noadmin" {
		t.Fatalf("got %v", err)
	}
	if want := map[string]string{" admin ": "x", " b ": "y"}; !reflect.DeepEqual(in.M, want) {
		t.Errorf("keys must be left unchanged and values normalized: %q", in.M)
	}
}

func TestQuotedArguments(t *testing.T) {
	for _, tt := range []struct{ tag, in, want string }{
		{"cutsuffix='''s'", "Ada's", "Ada"},
		{"cutsuffix=' (copy)'", "Ada's (copy)", "Ada's"},
	} {
		if got, err := normalize.Apply(tt.in, tt.tag); err != nil || got != tt.want {
			t.Errorf("%s(%q) = %q, %v; want %q", tt.tag, tt.in, got, err, tt.want)
		}
	}
}

func TestMapKeysAlreadyNormalized(t *testing.T) {
	m := map[string]string{"a": "1"}
	if err := normalize.Var(&m, "keys(trim,lower)"); err != nil || !reflect.DeepEqual(m, map[string]string{"a": "1"}) {
		t.Errorf("%v %v", m, err)
	}
}

func TestNonStringKeyPath(t *testing.T) {
	m := map[int]string{7: "admin"}
	err := newChecked(t).Var(&m, "dive,noadmin")
	var fe *normalize.FieldError
	if !errors.As(err, &fe) || fe.Path != "[7]" {
		t.Errorf("got %v", err)
	}
}

func TestNaNKeyIsNotStoredBack(t *testing.T) {
	m := map[float64]string{math.NaN(): " a ", 1: " b "}
	err := normalize.Var(&m, "dive,trim")
	if !errors.Is(err, normalize.ErrUnsupportedField) || len(m) != 2 || m[1] != "b" {
		t.Errorf("got %v %v", m, err)
	}
	clean := map[float64]string{math.NaN(): "a"}
	if err := normalize.Var(&clean, "dive,trim"); err != nil || len(clean) != 1 {
		t.Errorf("an unchanged NaN entry needs no write: %v %v", clean, err)
	}
}

func TestMaxDepth(t *testing.T) {
	type chain struct {
		S    string `normalize:"trim"`
		Next *chain
	}
	head := &chain{}
	cur := head
	for range 50 {
		cur.Next = &chain{S: " s "}
		cur = cur.Next
	}
	n := normalize.New(normalize.WithMaxDepth(20))
	if err := n.Struct(head); !errors.Is(err, normalize.ErrTooDeep) {
		t.Errorf("got %v, want ErrTooDeep", err)
	}
	if head.Next.S != "s" || cur.S != " s " {
		t.Errorf("values within the limit are normalized, beyond it not: %q %q", head.Next.S, cur.S)
	}
	if err := normalize.New(normalize.WithMaxDepth(0)).Struct(head); err != nil {
		t.Errorf("a limit below 1 keeps the default: %v", err)
	}
}

func TestMaxErrors(t *testing.T) {
	in := make([]string, 10)
	for i := range in {
		in[i] = "admin"
	}
	count := func(limit int) []error {
		t.Helper()
		n := normalize.New(normalize.WithMaxErrors(limit))
		if err := n.RegisterCheck("noadmin", func(s string) (string, error) { return s, errNotAllowed }); err != nil {
			t.Fatal(err)
		}
		return n.Var(&in, "dive,noadmin").(interface{ Unwrap() []error }).Unwrap()
	}
	errs := count(3)
	if len(errs) != 4 || !errors.Is(errs[3], normalize.ErrTooManyErrors) || !strings.Contains(errs[3].Error(), "7 more") {
		t.Errorf("got %d errors: %v", len(errs), errs)
	}
	if errs := count(-1); len(errs) != 10 {
		t.Errorf("a limit below 1 keeps the default: %d errors", len(errs))
	}
}

func TestStructCachedDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	in := newBenchInput()
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = normalize.Struct(&in) }); allocs != 0 {
		t.Errorf("Struct on clean input: %v allocations", allocs)
	}
}

func TestFieldErrorMessages(t *testing.T) {
	base := errors.New("boom")
	tests := []struct {
		e    normalize.FieldError
		want string
	}{
		{normalize.FieldError{Err: base}, "normalize: boom"},
		{normalize.FieldError{Op: "trim", Err: base}, "normalize: trim: boom"},
		{normalize.FieldError{Path: "A[1]", Err: base}, "normalize: A[1]: boom"},
		{normalize.FieldError{Path: "A[1]", Op: "trim", Err: base}, "normalize: A[1]: trim: boom"},
	}
	for _, tt := range tests {
		if got := tt.e.Error(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
		if !errors.Is(&tt.e, base) {
			t.Errorf("%q does not unwrap", tt.want)
		}
	}
}

func TestDirectiveEdgeCases(t *testing.T) {
	blank := ptr("  ")
	if err := normalize.Var(&blank, "trim,default=x"); err != nil || *blank != "" {
		t.Errorf("default keeps a present pointer even when blank: %q %v", *blank, err)
	}
	full := []string{"a"}
	if err := normalize.Var(&full, "nilempty"); err != nil || full == nil {
		t.Errorf("nilempty keeps a non-empty slice: %v %v", full, err)
	}
	empty := &[]string{}
	if err := normalize.Var(&empty, "nilempty"); err != nil || empty != nil {
		t.Errorf("nilempty on a pointer to an empty slice: %v %v", empty, err)
	}
	var b bool
	var u uint16
	var f float32
	if err := errors.Join(normalize.Var(&b, "default=true"), normalize.Var(&u, "default=0o17"), normalize.Var(&f, "default=1.5")); err != nil || !b || u != 15 || f != 1.5 {
		t.Errorf("scalar defaults: %v %v %v %v", b, u, f, err)
	}
}

type typedNilTarget struct{}

func (*typedNilTarget) NormalizeTarget() any { return (*string)(nil) }

type (
	tree    []tree
	nestMap map[string]nestMap
)

func TestMiscBranches(t *testing.T) {
	in := struct {
		T typedNilTarget `normalize:"trim"`
	}{}
	if err := normalize.Struct(&in); err != nil {
		t.Errorf("typed nil target: %v", err)
	}
	tr := tree{{}, {{}}}
	if err := normalize.Var(&tr, ""); err != nil {
		t.Errorf("recursive slice type: %v", err)
	}
	nm := nestMap{"a": {"b": nil}}
	if err := normalize.Var(&nm, ""); err != nil {
		t.Errorf("recursive map type: %v", err)
	}
	if got, _ := normalize.Apply("при", "truncate=4"); got != "при" {
		t.Errorf("truncate past the end of a multi-byte string: %q", got)
	}
}

func TestWithTagNameConflict(t *testing.T) {
	n := normalize.New(normalize.WithTagName("normalize", "norm", "n"))
	in := struct {
		S string `norm:"trim" n:"lower"`
	}{}
	if err := n.Struct(&in); !errors.Is(err, normalize.ErrMalformedTag) {
		t.Errorf("got %v", err)
	}
}
