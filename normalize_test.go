package normalize_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/go-deps/normalize"
)

type Optional[T any] struct {
	Value T
	Set   bool
}

func (o *Optional[T]) NormalizeTarget() any {
	if !o.Set {
		return nil
	}
	return &o.Value
}

func some[T any](v T) Optional[T] { return Optional[T]{Value: v, Set: true} }

func ptr[T any](v T) *T { return &v }

type Email string

type Item struct {
	Name string  `normalize:"trim"`
	Unit *string `normalize:"trim,lower"`
}

type Base struct {
	ID string `normalize:"trim"`
}

type embedded struct {
	Note string `normalize:"trim"`
}

type Input struct {
	Base
	embedded

	Title    string             `normalize:"trim"`
	Email    Email              `normalize:"trim,lower"`
	Code     string             `normalize:"upper"`
	Bio      *string            `normalize:"trim,collapse"`
	Password string             // untouched
	Tags     []string           `normalize:"dive,trim,lower"`
	Raw      []string           // untouched without dive
	Fixed    [2]string          `normalize:"dive,trim"`
	Items    []Item             // traversed without a tag
	ItemPtrs []*Item            // traversed through pointers
	Child    *Item              // traversed through a pointer
	Skipped  Item               `normalize:"-"`
	Nick     Optional[string]   `normalize:"trim"`
	Alias    Optional[*string]  `normalize:"trim"`
	Labels   Optional[[]string] `normalize:"dive,trim"`
	Extra    map[string]string  // not visited
	Count    int                // not visited
	private  string             // must stay untouched
}

func TestStruct(t *testing.T) {
	in := Input{
		Base:     Base{ID: " id-1 "},
		embedded: embedded{Note: " note "},
		Title:    "  Pasta  ",
		Email:    " Alice@Example.COM ",
		Code:     "ab-1",
		Bio:      ptr("  one \t two\n\nthree "),
		Password: "  secret  ",
		Tags:     []string{" Go ", "RUST"},
		Raw:      []string{" raw "},
		Fixed:    [2]string{" a ", "b "},
		Items:    []Item{{Name: " n1 ", Unit: ptr(" KG ")}},
		ItemPtrs: []*Item{{Name: " n2 "}, nil},
		Child:    &Item{Name: " child "},
		Skipped:  Item{Name: " skipped "},
		Nick:     some(" nick "),
		Alias:    some(ptr(" alias ")),
		Labels:   some([]string{" l1 "}),
		Extra:    map[string]string{"k": " v "},
		private:  " private ",
	}
	if err := normalize.Struct(&in); err != nil {
		t.Fatalf("Struct: %v", err)
	}

	checks := []struct {
		name, got, want string
	}{
		{"embedded exported", in.ID, "id-1"},
		{"embedded unexported type", in.Note, "note"},
		{"trim", in.Title, "Pasta"},
		{"named string type", string(in.Email), "alice@example.com"},
		{"upper", in.Code, "AB-1"},
		{"pointer + collapse", *in.Bio, "one two three"},
		{"no tag", in.Password, "  secret  "},
		{"dive 0", in.Tags[0], "go"},
		{"dive 1", in.Tags[1], "rust"},
		{"slice without dive", in.Raw[0], " raw "},
		{"array dive 0", in.Fixed[0], "a"},
		{"array dive 1", in.Fixed[1], "b"},
		{"slice of structs", in.Items[0].Name, "n1"},
		{"slice of structs, pointer field", *in.Items[0].Unit, "kg"},
		{"slice of struct pointers", in.ItemPtrs[0].Name, "n2"},
		{"struct pointer", in.Child.Name, "child"},
		{"skip", in.Skipped.Name, " skipped "},
		{"unwrapper", in.Nick.Value, "nick"},
		{"unwrapper with pointer", *in.Alias.Value, "alias"},
		{"unwrapper with dive", in.Labels.Value[0], "l1"},
		{"map", in.Extra["k"], " v "},
		{"unexported", in.private, " private "},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
	if in.ItemPtrs[1] != nil {
		t.Errorf("nil element changed: %v", in.ItemPtrs[1])
	}
}

func TestStructLeavesAbsentValues(t *testing.T) {
	var in Input
	if err := normalize.Struct(&in); err != nil {
		t.Fatalf("Struct on zero value: %v", err)
	}
	if in.Bio != nil || in.Child != nil || in.Nick.Set || in.Alias.Set {
		t.Errorf("absent values changed: %+v", in)
	}

	in.Alias = Optional[*string]{Set: true} // present wrapper around a nil pointer
	if err := normalize.Struct(&in); err != nil {
		t.Fatalf("Struct with nil wrapped pointer: %v", err)
	}
}

func TestStructInvalidArgument(t *testing.T) {
	var nilPtr *Input
	for name, arg := range map[string]any{
		"nil":            nil,
		"struct value":   Input{},
		"nil pointer":    nilPtr,
		"pointer to int": ptr(1),
		"string":         "x",
	} {
		t.Run(name, func(t *testing.T) {
			if err := normalize.Struct(arg); !errors.Is(err, normalize.ErrInvalidArgument) {
				t.Fatalf("got %v, want ErrInvalidArgument", err)
			}
		})
	}
}

type (
	unknownOp struct {
		Name string `normalize:"trim,lowr"`
	}
	opOnInt struct {
		N int `normalize:"trim"`
	}
	diveOnString struct {
		S string `normalize:"dive,trim"`
	}
	opsBeforeDive struct {
		S []string `normalize:"trim,dive"`
	}
	opsOnStructElems struct {
		Items []Item `normalize:"dive,trim"`
	}
	diveOnInts struct {
		N []int `normalize:"dive"`
	}
	opOnStruct struct {
		Item Item `normalize:"trim"`
	}
	opOnMap struct {
		M map[string]string `normalize:"dive,trim"`
	}
	emptyElement struct {
		S string `normalize:"trim,,lower"`
	}
	repeatedDive struct {
		S []string `normalize:"dive,dive"`
	}
	skipNotAlone struct {
		S string `normalize:"trim,-"`
	}
	nestedTypo struct {
		Child *unknownOp // nil, still checked
	}
	nestedInSlice struct {
		Children []opOnInt
	}
)

func TestStructTagErrors(t *testing.T) {
	tests := []struct {
		name  string
		arg   any
		want  error
		field string
	}{
		{"unknown operation", &unknownOp{}, normalize.ErrUnknownOperation, "Name"},
		{"operation on int", &opOnInt{}, normalize.ErrUnsupportedField, "N"},
		{"dive on string", &diveOnString{}, normalize.ErrUnsupportedField, "S"},
		{"operation before dive", &opsBeforeDive{}, normalize.ErrUnsupportedField, "S"},
		{"operation on struct elements", &opsOnStructElems{}, normalize.ErrUnsupportedField, "Items"},
		{"dive on ints", &diveOnInts{}, normalize.ErrUnsupportedField, "N"},
		{"operation on struct", &opOnStruct{}, normalize.ErrUnsupportedField, "Item"},
		{"operation on map", &opOnMap{}, normalize.ErrUnsupportedField, "M"},
		{"empty element", &emptyElement{}, normalize.ErrMalformedTag, "S"},
		{"repeated dive", &repeatedDive{}, normalize.ErrMalformedTag, "S"},
		{"skip not alone", &skipNotAlone{}, normalize.ErrMalformedTag, "S"},
		{"nested nil pointer", &nestedTypo{}, normalize.ErrUnknownOperation, "Name"},
		{"nested in empty slice", &nestedInSlice{}, normalize.ErrUnsupportedField, "N"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := normalize.New()
			for range 2 { // the second call is served from the cache
				err := n.Struct(tt.arg)
				if !errors.Is(err, tt.want) {
					t.Fatalf("got %v, want %v", err, tt.want)
				}
				var te *normalize.TagError
				if !errors.As(err, &te) {
					t.Fatalf("got %T, want *TagError", err)
				}
				if te.Field != tt.field {
					t.Errorf("Field = %q, want %q", te.Field, tt.field)
				}
				if !strings.Contains(err.Error(), te.Type.Name()) {
					t.Errorf("message %q does not name the type", err)
				}
			}
		})
	}
}

type wrongTarget struct{ v string }

func (w *wrongTarget) NormalizeTarget() any { return w.v }

type selfTarget struct{ S string }

func (s *selfTarget) NormalizeTarget() any { return s }

type wrappedInt struct {
	N Optional[int] `normalize:"trim"`
}

func TestStructUnwrapperErrors(t *testing.T) {
	t.Run("non-pointer target", func(t *testing.T) {
		in := struct{ W wrongTarget }{W: wrongTarget{v: "x"}}
		if err := normalize.Struct(&in); !errors.Is(err, normalize.ErrInvalidTarget) {
			t.Fatalf("got %v, want ErrInvalidTarget", err)
		}
	})
	t.Run("returns itself", func(t *testing.T) {
		in := struct{ S selfTarget }{}
		if err := normalize.Struct(&in); !errors.Is(err, normalize.ErrInvalidTarget) {
			t.Fatalf("got %v, want ErrInvalidTarget", err)
		}
	})
	t.Run("operation on wrapped int", func(t *testing.T) {
		if err := normalize.Struct(&wrappedInt{}); err != nil {
			t.Fatalf("absent value: got %v, want nil", err)
		}
		if err := normalize.Struct(&wrappedInt{N: some(1)}); !errors.Is(err, normalize.ErrUnsupportedField) {
			t.Fatalf("present value: got %v, want ErrUnsupportedField", err)
		}
	})
}

type custom struct {
	Phone string `normalize:"digits"`
	Name  string `normalize:"trim"`
}

func TestRegister(t *testing.T) {
	n := normalize.New()
	if err := n.Struct(&custom{}); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Fatalf("before Register: got %v, want ErrUnknownOperation", err)
	}
	digits := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s)
	}
	if err := n.Register("digits", digits); err != nil {
		t.Fatalf("Register: %v", err)
	}
	in := custom{Phone: "+1 (555) 010-99", Name: " x "}
	if err := n.Struct(&in); err != nil {
		t.Fatalf("after Register (cache must be discarded): %v", err)
	}
	if in.Phone != "155501099" || in.Name != "x" {
		t.Errorf("got %+v", in)
	}

	if err := n.Register("trim", strings.ToUpper); err != nil {
		t.Fatalf("replacing a built-in: %v", err)
	}
	in.Name = "x"
	if err := n.Struct(&in); err != nil || in.Name != "X" {
		t.Errorf("replaced built-in: got %q, %v", in.Name, err)
	}

	if err := normalize.Struct(&custom{}); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Errorf("package-level Struct must not see another Normalizer's operations: %v", err)
	}
}

func TestRegisterRejects(t *testing.T) {
	n := normalize.New()
	for _, name := range []string{"", "-", "dive", "a,b", "a b", "a\tb"} {
		if err := n.Register(name, strings.TrimSpace); err == nil {
			t.Errorf("Register(%q): want error", name)
		}
	}
	if err := n.Register("ok", nil); err == nil {
		t.Error("Register with nil Func: want error")
	}
}

type otherTag struct {
	S string `clean:"trim" normalize:"upper"`
}

func TestWithTagName(t *testing.T) {
	in := otherTag{S: " x "}
	if err := normalize.New(normalize.WithTagName("clean")).Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.S != "x" {
		t.Errorf("got %q, want %q", in.S, "x")
	}
}

type node struct {
	Name string `normalize:"trim"`
	Next *node
	Kids []node
}

func TestRecursiveType(t *testing.T) {
	in := node{Name: " a ", Next: &node{Name: " b "}, Kids: []node{{Name: " c "}}}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.Name != "a" || in.Next.Name != "b" || in.Kids[0].Name != "c" {
		t.Errorf("got %q %q %q", in.Name, in.Next.Name, in.Kids[0].Name)
	}
}

type ring struct {
	Name  string `normalize:"trim"`
	Next  *ring
	Peers []*ring
	Back  Optional[*ring]
}

type selfSlice struct {
	Items []selfSlice
	Name  string `normalize:"trim"`
}

func TestCyclicValues(t *testing.T) {
	a := &ring{Name: " a "}
	b := &ring{Name: " b ", Next: a}
	a.Next = b
	a.Peers = []*ring{a, b}
	b.Back = some(a)
	if err := normalize.Struct(a); err != nil {
		t.Fatal(err)
	}
	if a.Name != "a" || b.Name != "b" {
		t.Errorf("got %q %q", a.Name, b.Name)
	}

	s := selfSlice{Name: " s "}
	s.Items = []selfSlice{{Name: " i "}}
	s.Items[0].Items = s.Items // the slice contains itself
	if err := normalize.Struct(&s); err != nil {
		t.Fatal(err)
	}
	if s.Name != "s" || s.Items[0].Name != "i" {
		t.Errorf("got %q %q", s.Name, s.Items[0].Name)
	}
}

// TestDenseCycleIsLinear guards against walking every path of a graph: each
// of the 50 nodes points at all others, so a walk without visit tracking
// would not finish.
func TestDenseCycleIsLinear(t *testing.T) {
	nodes := make([]*ring, 50)
	for i := range nodes {
		nodes[i] = &ring{Name: " x "}
	}
	for _, n := range nodes {
		n.Peers = nodes
		n.Next = nodes[0]
	}
	if err := normalize.Struct(nodes[0]); err != nil {
		t.Fatal(err)
	}
	for i, n := range nodes {
		if n.Name != "x" {
			t.Fatalf("node %d not normalized: %q", i, n.Name)
		}
	}
}

func TestDeepAcyclicValue(t *testing.T) {
	head := &ring{Name: " 0 "}
	cur := head
	for range 500 {
		cur.Next = &ring{Name: " n "}
		cur = cur.Next
	}
	if err := normalize.Struct(head); err != nil {
		t.Fatal(err)
	}
	for n := head; n != nil; n = n.Next {
		if strings.TrimSpace(n.Name) != n.Name {
			t.Fatalf("node not normalized: %q", n.Name)
		}
	}
}

type sharedPointer struct {
	A *string `normalize:"trim"`
	B *string `normalize:"upper"`
}

func TestSharedStringGetsEveryFieldsOperations(t *testing.T) {
	v := " x "
	in := sharedPointer{A: &v, B: &v}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if v != "X" {
		t.Errorf("got %q, want %q", v, "X")
	}
}

type withInterface struct {
	Any  any
	Err  error
	Fn   func()
	Ch   chan string
	Name string `normalize:"trim"`
}

func TestNonVisitedKindsDoNotPanic(t *testing.T) {
	in := withInterface{Any: &Item{Name: " x "}, Name: " n "}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.Name != "n" || in.Any.(*Item).Name != " x " {
		t.Errorf("got %q %q", in.Name, in.Any.(*Item).Name)
	}
}

func TestBuiltinsMatchStandardLibrary(t *testing.T) {
	type s struct {
		V string `normalize:"trim,lower"`
	}
	for _, in := range []string{"\u00a0A@B.C\u3000", "\t\nMiXeD\r ", "\u2028x\u2029", "İ", "\xff A "} {
		v := s{V: in}
		if err := normalize.Struct(&v); err != nil {
			t.Fatal(err)
		}
		if want := strings.ToLower(strings.TrimSpace(in)); v.V != want {
			t.Errorf("trim,lower(%q) = %q, want %q", in, v.V, want)
		}
	}
}

func TestCollapse(t *testing.T) {
	type s struct {
		V string `normalize:"collapse"`
	}
	for in, want := range map[string]string{
		"":               "",
		"a b":            "a b",
		"a  b":           "a b",
		"a\tb":           "a b",
		" a \n\n b ":     " a b ",
		"a\u00a0\u2003b": "a b",
		"привет   мир":   "привет мир",
		"no-space":       "no-space",
	} {
		v := s{V: in}
		if err := normalize.Struct(&v); err != nil {
			t.Fatal(err)
		}
		if v.V != want {
			t.Errorf("collapse(%q) = %q, want %q", in, v.V, want)
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	n := normalize.New()
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			if i%4 == 0 {
				if err := n.Register("noop", func(s string) string { return s }); err != nil {
					t.Error(err)
				}
			}
			in := Input{Title: " t ", Tags: []string{" A "}}
			if err := n.Struct(&in); err != nil {
				t.Error(err)
				return
			}
			if in.Title != "t" || in.Tags[0] != "a" {
				t.Errorf("got %q %q", in.Title, in.Tags[0])
			}
		})
	}
	wg.Wait()
}
