package normalize_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/go-deps/normalize"
)

func TestOperations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag, in, want string
	}{
		{"trim", " \t a b \n", "a b"},
		{"trim=-_", "-_a-b_-", "a-b"},
		{"trimleft", "  a  ", "a  "},
		{"trimleft=0", "007", "7"},
		{"trimright", "  a  ", "  a"},
		{"trimright=/", "/a//", "/a"},
		{"cutprefix=+", "++1", "1"},
		{"cutprefix='ab'", "ababc", "c"},
		{"cutsuffix=.0", "1.0.0", "1"},
		{"lower", "ÀB", "àb"},
		{"upper", "àb", "ÀB"},
		{"collapse", "a \t\n b", "a b"},
		{"collapse=inline", "a \t b\n\n  c", "a b\n\n c"},
		{"newline", "a\r\nb\rc\u2028d\u0085e", "a\nb\nc\nd\ne"},
		{"newline", "a\n\rb", "a\n\nb"},
		{"capfirst", "élan vital", "Élan vital"},
		{"capfirst", "ǆungla", "ǅungla"},
		{"capfirst", "", ""},
		{"slug", "  Hello, World! 2024 ", "hello-world-2024"},
		{"slug", "Привет — мир", "привет-мир"},
		{"slug", "--a--", "a"},
		{"slug", "cafe\u0301 au lait", "cafe\u0301-au-lait"},
		{"email", "  Alice.Smith@Example.COM ", "Alice.Smith@example.com"},
		{"email", "no-at-sign ", "no-at-sign"},
		{"email", `"a@b"@Example.org`, `"a@b"@example.org`},
		{"digits", "+1 (555) 010-99", "155501099"},
		{"letters", "a1-b2 c", "abc"},
		{"alnum", "a1-b2 c!", "a1b2c"},
		{"nocontrol", "a\x00b\x1bc\td\ne", "abc\td\ne"},
		{"keep=digit", "tel: 8 800", "8800"},
		{"keep=letter|space", "R2-D2 and C-3PO", "RD and CPO"},
		{"keep=ascii", "naïve café", "nave caf"},
		{"remove=punct|symbol", "a.b,c$d+e", "abcde"},
		{"remove=invisible", "a\u200bb\ufeffc\u202ed", "abcd"},
		{"remove=control", "a\x7fb\nc", "ab\nc"},
		{"remove=mark", "cafe\u0301", "cafe"},
		{"truncate=3", "abcdef", "abc"},
		{"truncate=3", "привет", "при"},
		{"truncate=10", "short", "short"},
		{"case=snake", "prepTime", "prep_time"},
		{"case=snake", "HTTPServer v2Api", "http_server_v2_api"},
		{"case=kebab", "Prep Time", "prep-time"},
		{"case=dot", "prep_time", "prep.time"},
		{"case=constant", "in progress", "IN_PROGRESS"},
		{"case=camel", "prep_time", "prepTime"},
		{"case=camel", "HTTP server", "httpServer"},
		{"case=pascal", "prep-time", "PrepTime"},
		{"case=pascal", "user ID", "UserID"},
		{"case=pascal", "a b", "AB"},
		{"case=snake,case=pascal", "IN_PROGRESS", "InProgress"},
		{"case=pascal", "IN_PROGRESS", "INPROGRESS"},
		{"case=constant", "v2Api", "V2_API"},
		{"case=camel", "User ID", "userID"},
		{"case=train", "prep time", "Prep-Time"},
		{"case=snake", "Время Готовки", "время_готовки"},
		{"case=snake", "", ""},
		{"trim,default=n/a", "   ", "n/a"},
		{"default='a, b'", "", "a, b"},
		{"default='it''s'", "", "it's"},
		{"trim,lower,truncate=5", "  HELLO WORLD ", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.tag+"/"+tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := normalize.Apply(tt.in, tt.tag)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("String(%q, %q) = %q, want %q", tt.in, tt.tag, got, tt.want)
			}
			again, err := normalize.Apply(got, tt.tag)
			if err != nil {
				t.Fatal(err)
			}
			if again != got {
				t.Errorf("not idempotent: %q -> %q -> %q", tt.in, got, again)
			}
		})
	}
}

func TestInvalidUTF8IsKept(t *testing.T) {
	if got, _ := normalize.Apply("\xffa", "capfirst"); got != "\xffa" {
		t.Errorf("capfirst on a leading invalid byte: %q", got)
	}
	for _, tag := range []string{"collapse", "collapse=inline", "newline", "email", "trimleft", "trimright"} {
		got, err := normalize.Apply("a\xffb", tag)
		if err != nil {
			t.Fatal(err)
		}
		if got != "a\xffb" {
			t.Errorf("%s changed invalid UTF-8: %q", tag, got)
		}
	}
	for _, tag := range []string{"keep=letter", "remove=punct", "nocontrol"} {
		if got, _ := normalize.Apply("a\xffb", tag); got != "ab" {
			t.Errorf("%s must drop invalid bytes: %q", tag, got)
		}
	}
	if got, _ := normalize.Apply("\xdc!\x83", "remove=punct"); got != "" {
		t.Errorf("removal must not join invalid bytes into a rune: %q", got)
	}
	if got, _ := normalize.Apply("ab\xff\xfe", "truncate=3"); got != "ab\xff" {
		t.Errorf("truncate counts an invalid byte as one rune: %q", got)
	}
}

func TestApplyTagErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag  string
		want error
	}{
		{"nope", normalize.ErrUnknownOperation},
		{"trim,,lower", normalize.ErrMalformedTag},
		{"trim,", normalize.ErrMalformedTag},
		{"truncate", normalize.ErrMalformedTag},
		{"truncate=0", normalize.ErrMalformedTag},
		{"truncate=x", normalize.ErrMalformedTag},
		{"lower=x", normalize.ErrMalformedTag},
		{"keep=letters", normalize.ErrMalformedTag},
		{"keep=", normalize.ErrMalformedTag},
		{"case=title", normalize.ErrMalformedTag},
		{"collapse=all", normalize.ErrMalformedTag},
		{"trim=", normalize.ErrMalformedTag},
		{"cutprefix=", normalize.ErrMalformedTag},
		{"default", normalize.ErrMalformedTag},
		{"default=a,default=b", normalize.ErrMalformedTag},
		{"default='a", normalize.ErrMalformedTag},
		{"default=a'b", normalize.ErrMalformedTag},
		{"default='a'b", normalize.ErrMalformedTag},
		{"trim=)", normalize.ErrMalformedTag},
		{"trim(x)", normalize.ErrMalformedTag},
		{"keys(trim)", normalize.ErrUnsupportedField},
		{"keys", normalize.ErrMalformedTag},
		{"keys()", normalize.ErrMalformedTag},
		{"keys(trim", normalize.ErrMalformedTag},
		{"keys(keys(trim))", normalize.ErrMalformedTag},
		{"keys(dive)", normalize.ErrMalformedTag},
		{"dive,trim", normalize.ErrUnsupportedField},
		{"dive=1", normalize.ErrMalformedTag},
		{"nilempty", normalize.ErrUnsupportedField},
		{"nilempty=1", normalize.ErrMalformedTag},
		{"-", normalize.ErrMalformedTag},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			t.Parallel()
			got, err := normalize.Apply(" x ", tt.tag)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			var te *normalize.TagError
			if !errors.As(err, &te) || te.Type != reflect.TypeFor[string]() || te.Field != "" || te.Tag != tt.tag {
				t.Errorf("want *TagError with the value type and the tag, got %#v", err)
			}
			if got != " x " {
				t.Errorf("input changed on error: %q", got)
			}
		})
	}
}

type Matrix struct {
	Rows   [][]string           `normalize:"dive,dive,trim"`
	Labels map[string]string    `normalize:"keys(trim,lower),dive,trim"`
	Groups map[string][]string  `normalize:"keys(case=snake),dive,dive,upper"`
	ByID   map[int]string       `normalize:"dive,trim"`
	Nested map[string]Item      // values traversed without a tag
	Ptrs   map[string]*Item     // through pointers
	Opts   []Optional[string]   `normalize:"dive,trim"`
	Deep   map[string][]*string `normalize:"dive,dive,lower"`
}

func TestContainers(t *testing.T) {
	in := Matrix{
		Rows:   [][]string{{" a ", "b "}, {" c"}},
		Labels: map[string]string{" EN ": " Hello ", "de": " Hallo "}, //nolint:gocritic // untrimmed keys are the input under test
		Groups: map[string][]string{"Main Course": {"x", "y"}},
		ByID:   map[int]string{1: " one "},
		Nested: map[string]Item{"k": {Name: " nested ", Unit: ptr(" G ")}},
		Ptrs:   map[string]*Item{"p": {Name: " ptr "}, "nil": nil},
		Opts:   []Optional[string]{some(" o "), {}},
		Deep:   map[string][]*string{"d": {ptr("ABC"), nil}},
	}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	want := Matrix{
		Rows:   [][]string{{"a", "b"}, {"c"}},
		Labels: map[string]string{"en": "Hello", "de": "Hallo"},
		Groups: map[string][]string{"main_course": {"X", "Y"}},
		ByID:   map[int]string{1: "one"},
		Nested: map[string]Item{"k": {Name: "nested", Unit: ptr("g")}},
		Ptrs:   map[string]*Item{"p": {Name: "ptr"}, "nil": nil},
		Opts:   []Optional[string]{some("o"), {}},
		Deep:   map[string][]*string{"d": {ptr("abc"), nil}},
	}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("got  %+v\nwant %+v", in, want)
	}
}

func TestMapKeyCollision(t *testing.T) {
	type labels struct {
		M map[string]string `normalize:"keys(trim,lower),dive,trim"`
	}
	in := labels{M: map[string]string{"en": " a ", " EN": " b ", "de": " c "}} //nolint:gocritic // untrimmed keys are the input under test
	err := normalize.Struct(&in)
	if !errors.Is(err, normalize.ErrKeyCollision) {
		t.Fatalf("got %v, want ErrKeyCollision", err)
	}
	var fe *normalize.FieldError
	if !errors.As(err, &fe) || fe.Path != "M" || fe.Op != "keys" {
		t.Errorf("got %#v", fe)
	}
	if want := map[string]string{"en": "a", " EN": "b", "de": "c"}; !reflect.DeepEqual(in.M, want) { //nolint:gocritic // untrimmed keys are the input under test
		t.Errorf("keys must be left unchanged and values normalized: got %q, want %q", in.M, want)
	}

	swap := labels{M: map[string]string{"A": "1", "a ": "2"}} //nolint:gocritic // untrimmed keys are the input under test
	if err := normalize.Struct(&swap); !errors.Is(err, normalize.ErrKeyCollision) {
		t.Errorf("two keys normalizing to one: got %v", err)
	}
	type rename struct {
		M map[string]string `normalize:"keys(cutprefix=x)"`
	}
	moved := rename{M: map[string]string{"xa": "1", "a": "2"}}
	if err := normalize.Struct(&moved); !errors.Is(err, normalize.ErrKeyCollision) {
		t.Errorf("onto an existing key: got %v", err)
	}
	ok := rename{M: map[string]string{"xa": "1", "xb": "2", "c": "3"}}
	if err := normalize.Struct(&ok); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ok.M, map[string]string{"a": "1", "b": "2", "c": "3"}) {
		t.Errorf("got %v", ok.M)
	}
}

type Directives struct {
	Name     *string           `normalize:"trim,nilempty"`
	Tags     []string          `normalize:"nilempty"`
	Meta     map[string]string `normalize:"nilempty"`
	Notes    []*string         `normalize:"dive,trim,nilempty"`
	Title    string            `normalize:"trim,default=Untitled"`
	Lang     *string           `normalize:"default=en"`
	Servings int               `normalize:"default=2"`
	Rating   *float64          `normalize:"default=4.5"`
	Public   *bool             `normalize:"default=true"`
	Limit    *uint8            `normalize:"default=0x10"`
	Units    []*string         `normalize:"dive,default=g"`
}

func TestDirectives(t *testing.T) {
	in := Directives{
		Name:  ptr("   "),
		Tags:  []string{},
		Meta:  map[string]string{},
		Notes: []*string{ptr(" "), ptr(" keep ")},
		Title: "  ",
		Units: []*string{nil, ptr("kg")},
	}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.Name != nil || in.Tags != nil || in.Meta != nil {
		t.Errorf("nilempty: %v %v %v", in.Name, in.Tags, in.Meta)
	}
	if in.Notes[0] != nil || *in.Notes[1] != "keep" {
		t.Errorf("nilempty after dive: %v", in.Notes)
	}
	if in.Title != "Untitled" || *in.Lang != "en" || in.Servings != 2 || *in.Rating != 4.5 || !*in.Public || *in.Limit != 16 {
		t.Errorf("defaults: %+v", in)
	}
	if *in.Units[0] != "g" || *in.Units[1] != "kg" {
		t.Errorf("default after dive: %q %q", *in.Units[0], *in.Units[1])
	}

	set := Directives{Lang: ptr("ru"), Servings: 4, Public: ptr(false), Name: ptr(" x ")}
	if err := normalize.Struct(&set); err != nil {
		t.Fatal(err)
	}
	if *set.Lang != "ru" || set.Servings != 4 || *set.Public || *set.Name != "x" {
		t.Errorf("present values must be kept: %+v", set)
	}
}

func TestDirectiveTagErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		arg  any
		want error
	}{
		{"nilempty on string", &struct {
			S string `normalize:"nilempty"`
		}{}, normalize.ErrUnsupportedField},
		{"default on struct", &struct {
			S Item `normalize:"default=x"`
		}{}, normalize.ErrUnsupportedField},
		{"default not an int", &struct {
			N int `normalize:"default=two"`
		}{}, normalize.ErrMalformedTag},
		{"default overflows", &struct {
			N int8 `normalize:"default=300"`
		}{}, normalize.ErrMalformedTag},
		{"default on double pointer", &struct {
			S **string `normalize:"default=x"`
		}{}, normalize.ErrUnsupportedField},
		{"nilempty with default", &struct {
			S *string `normalize:"nilempty,default=x"`
		}{}, normalize.ErrMalformedTag},
		{"keys on slice", &struct {
			S []string `normalize:"keys(trim)"`
		}{}, normalize.ErrUnsupportedField},
		{"keys on int-keyed map", &struct {
			M map[int]string `normalize:"keys(trim)"`
		}{}, normalize.ErrUnsupportedField},
		{"keys twice", &struct {
			M map[string]string `normalize:"keys(trim),keys(lower)"`
		}{}, normalize.ErrMalformedTag},
		{"default on wrapper", &struct {
			O Optional[string] `normalize:"default=x"`
		}{}, normalize.ErrUnsupportedField},
		{"operation on map", &struct {
			M map[string]string `normalize:"trim"`
		}{}, normalize.ErrUnsupportedField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := normalize.Struct(tt.arg); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

var (
	errNotAllowed    = errors.New("not allowed")
	errTitleRequired = errors.New("title is required")
)

func newChecked(t *testing.T) *normalize.Normalizer {
	t.Helper()
	n := normalize.New(normalize.WithFieldName(func(sf reflect.StructField) string {
		name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		return name
	}))
	if err := n.RegisterCheck("noadmin", func(s string) (string, error) {
		if strings.EqualFold(s, "admin") {
			return s, errNotAllowed
		}
		return s, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := n.RegisterParam("pad", func(arg string) (normalize.CheckFunc, error) {
		if len(arg) != 1 {
			return nil, fmt.Errorf("want one character")
		}
		return func(s string) (string, error) {
			if len(s) < 3 {
				s = strings.Repeat(arg, 3-len(s)) + s
			}
			return s, nil
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

type Account struct {
	User    string            `json:"user" normalize:"trim,noadmin"`
	Code    string            `json:"code" normalize:"pad=0"`
	Members []Member          `json:"members"`
	Aliases map[string]string `json:"aliases" normalize:"dive,trim,noadmin"`
}

type Member struct {
	Name string `json:"name" normalize:"trim,noadmin"`
	Note string // no json tag: Go name in paths
}

func TestFieldErrors(t *testing.T) {
	n := newChecked(t)
	in := Account{
		User:    " Admin ",
		Code:    "7",
		Members: []Member{{Name: " ok "}, {Name: "admin"}},
		Aliases: map[string]string{"root": " ADMIN "},
	}
	err := n.Struct(&in)
	if err == nil {
		t.Fatal("want errors")
	}
	var paths []string
	for _, e := range err.(interface{ Unwrap() []error }).Unwrap() {
		var fe *normalize.FieldError
		if !errors.As(e, &fe) || !errors.Is(e, errNotAllowed) || fe.Op != "noadmin" {
			t.Errorf("unexpected error %v", e)
			continue
		}
		paths = append(paths, fe.Path)
	}
	if want := []string{"user", "members[1].name", `aliases["root"]`}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
	if in.User != " Admin " || in.Aliases["root"] != " ADMIN " {
		t.Errorf("rejected values must be left unchanged: %q %q", in.User, in.Aliases["root"])
	}
	if in.Code != "007" || in.Members[0].Name != "ok" {
		t.Errorf("other fields must still be normalized: %+v", in)
	}
	if !strings.Contains(err.Error(), `normalize: members[1].name: noadmin: not allowed`) {
		t.Errorf("message: %v", err)
	}

	if _, err := normalize.ApplyWith(n, "admin", "noadmin"); !errors.Is(err, errNotAllowed) {
		t.Errorf("Apply: got %v", err)
	}
	if _, err := normalize.ApplyWith(n, "1", "pad=00"); !errors.Is(err, normalize.ErrMalformedTag) {
		t.Errorf("invalid argument: got %v", err)
	}
	if got, err := normalize.ApplyWith(n, "1", "pad=0"); err != nil || got != "001" {
		t.Errorf("pad: %q %v", got, err)
	}
}

func TestRegisterPlainAndParamForms(t *testing.T) {
	n := normalize.New()
	if err := n.RegisterParam("trim", func(string) (normalize.CheckFunc, error) {
		return func(s string) (string, error) { return "param", nil }, nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := normalize.ApplyWith(n, " x ", "trim"); got != "x" {
		t.Errorf("plain form must be kept: %q", got)
	}
	if got, _ := normalize.ApplyWith(n, " x ", "trim=x"); got != "param" {
		t.Errorf("param form replaced: %q", got)
	}
	if err := n.RegisterParam("p", nil); err == nil {
		t.Error("nil ParamFunc: want error")
	}
	if err := n.RegisterCheck("p", nil); err == nil {
		t.Error("nil CheckFunc: want error")
	}
	if err := n.RegisterParam("nilop", func(string) (normalize.CheckFunc, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := normalize.ApplyWith(n, "x", "nilop=1"); !errors.Is(err, normalize.ErrMalformedTag) {
		t.Errorf("ParamFunc returning nil: got %v", err)
	}
	if _, err := normalize.ApplyWith(n, "x", "nilop"); !errors.Is(err, normalize.ErrMalformedTag) {
		t.Errorf("param-only operation without argument: got %v", err)
	}
}

type Recipe struct {
	Title string `normalize:"trim"`
	Slug  string
	Steps []Step
	calls *int
}

func (r *Recipe) AfterNormalize() error {
	if r.calls != nil {
		*r.calls++
	}
	if r.Slug == "" {
		r.Slug, _ = normalize.Apply(r.Title, "slug")
	}
	if r.Title == "" {
		return errTitleRequired
	}
	return nil
}

type Step struct {
	Text  string `normalize:"trim"`
	Order int
}

func (s *Step) AfterNormalize() error {
	if s.Text == "" {
		return errors.New("empty step")
	}
	return nil
}

type Wrapper struct {
	Recipe        // AfterNormalize is promoted: called once, as Wrapper's
	Extra  string `normalize:"trim"`
}

type Override struct {
	*Recipe
	Count int
}

func (o *Override) AfterNormalize() error {
	o.Count++
	return nil
}

func TestAfterNormalize(t *testing.T) {
	calls := 0
	in := Recipe{Title: "  Pasta Carbonara ", Steps: []Step{{Text: " boil "}, {Text: "  "}}, calls: &calls}
	err := normalize.Struct(&in)
	var fe *normalize.FieldError
	if !errors.As(err, &fe) || fe.Path != "Steps[1]" || fe.Op != "AfterNormalize" {
		t.Fatalf("got %v", err)
	}
	if in.Slug != "pasta-carbonara" || calls != 1 {
		t.Errorf("hook must run after fields: slug %q, calls %d", in.Slug, calls)
	}

	empty := Recipe{}
	err = normalize.Struct(&empty)
	if !errors.As(err, &fe) || fe.Path != "" || !errors.Is(err, errTitleRequired) {
		t.Errorf("root hook error: %v", err)
	}

	calls = 0
	w := Wrapper{Recipe: Recipe{Title: " T ", calls: &calls}, Extra: " e "}
	if err := normalize.Struct(&w); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || w.Title != "T" || w.Extra != "e" {
		t.Errorf("promoted hook: calls %d, %+v", calls, w)
	}

	calls = 0
	o := Override{Recipe: &Recipe{Title: " T ", calls: &calls}}
	if err := normalize.Struct(&o); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || o.Count != 1 || o.Title != "T" {
		t.Errorf("declared hook replaces the embedded one: calls %d, count %d", calls, o.Count)
	}
}

func TestEmbeddedFieldPaths(t *testing.T) {
	type Inner struct {
		V string `normalize:"noadmin"`
	}
	type Outer struct {
		Inner
		W string `normalize:"noadmin"`
	}
	err := newChecked(t).Struct(&Outer{Inner: Inner{V: "admin"}})
	var fe *normalize.FieldError
	if !errors.As(err, &fe) || fe.Path != "V" {
		t.Errorf("promoted field path: %v", err)
	}
}

func TestWrapperErrorsAtRunTime(t *testing.T) {
	type wrapped struct {
		O Optional[[]string]          `normalize:"trim"`
		M Optional[map[string]string] `normalize:"trim"`
		D Optional[string]            `normalize:"dive,trim"`
	}
	in := wrapped{O: some([]string{"x"}), M: some(map[string]string{"a": "b"}), D: some("x")}
	err := normalize.Struct(&in)
	if !errors.Is(err, normalize.ErrUnsupportedField) {
		t.Fatalf("got %v", err)
	}
	var paths []string
	for _, e := range err.(interface{ Unwrap() []error }).Unwrap() {
		var fe *normalize.FieldError
		if errors.As(e, &fe) {
			paths = append(paths, fe.Path)
		}
	}
	if want := []string{"O", "M", "D"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
}

func TestMapOfStructsIsWalkedOnceWhenShared(t *testing.T) {
	type node struct {
		Name string `normalize:"trim"`
		Kids map[string]*node
	}
	a := &node{Name: " a "}
	a.Kids = map[string]*node{"self": a}
	if err := normalize.Struct(a); err != nil {
		t.Fatal(err)
	}
	if a.Name != "a" {
		t.Errorf("got %q", a.Name)
	}
}

func TestDecodeThenNormalize(t *testing.T) {
	var in struct {
		Email string   `json:"email" normalize:"trim,email"`
		Tags  []string `json:"tags" normalize:"nilempty,dive,trim,lower"`
	}
	if err := json.Unmarshal([]byte(`{"email":" Bob@MAIL.Example ","tags":[" Go "]}`), &in); err != nil {
		t.Fatal(err)
	}
	if err := normalize.Struct(&in); err != nil {
		t.Fatal(err)
	}
	if in.Email != "Bob@mail.example" || in.Tags[0] != "go" {
		t.Errorf("got %+v", in)
	}
}

func TestDeepPath(t *testing.T) {
	type leaf struct {
		V string `normalize:"noadmin"`
	}
	in := struct {
		A [][][][][]map[string][]leaf
	}{A: [][][][][]map[string][]leaf{{{{{{"k": {{}, {V: "admin"}}}}}}}}}
	err := newChecked(t).Struct(&in)
	var fe *normalize.FieldError
	if !errors.As(err, &fe) || fe.Path != `A[0][0][0][0][0]["k"][1].V` {
		t.Errorf("got %v", err)
	}
}

func TestVar(t *testing.T) {
	email := Email("  Bob@Example.COM ")
	if err := normalize.Var(&email, "trim,lower"); err != nil || email != "bob@example.com" {
		t.Errorf("named string: %q %v", email, err)
	}
	tags := []string{" Go ", " "}
	if err := normalize.Var(&tags, "dive,trim,lower"); err != nil || tags[0] != "go" || tags[1] != "" {
		t.Errorf("slice: %q %v", tags, err)
	}
	empty := []string{}
	if err := normalize.Var(&empty, "nilempty"); err != nil || empty != nil {
		t.Errorf("nilempty: %v %v", empty, err)
	}
	var limit *int
	if err := normalize.Var(&limit, "default=20"); err != nil || limit == nil || *limit != 20 {
		t.Errorf("default: %v %v", limit, err)
	}
	labels := map[string]string{" EN ": " a "}
	if err := normalize.Var(&labels, "keys(trim,lower),dive,trim"); err != nil || labels["en"] != "a" {
		t.Errorf("map: %v %v", labels, err)
	}
	item := Item{Name: " x "}
	if err := normalize.Var(&item, ""); err != nil || item.Name != "x" {
		t.Errorf("struct without tag: %+v %v", item, err)
	}
	if err := normalize.Var(&item, "trim"); !errors.Is(err, normalize.ErrUnsupportedField) {
		t.Errorf("operation on struct: %v", err)
	}
	if err := normalize.Var(&struct{ U unknownOp }{}, ""); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Errorf("nested tag error must be reported before any change: %v", err)
	}
	var nilPtr *string
	for _, arg := range []any{nil, "x", nilPtr} {
		if err := normalize.Var(arg, "trim"); !errors.Is(err, normalize.ErrInvalidArgument) {
			t.Errorf("Var(%#v): %v", arg, err)
		}
	}
	err := normalize.Var(&tags, "upper")
	var te *normalize.TagError
	if !errors.As(err, &te) || te.Type != reflect.TypeFor[[]string]() || !strings.Contains(err.Error(), "[]string") {
		t.Errorf("tag error names the value type: %v", err)
	}
}

func TestApplyNamedType(t *testing.T) {
	got, err := normalize.Apply(Email(" A@B.C "), "trim,lower")
	if err != nil || got != Email("a@b.c") {
		t.Errorf("got %q %v", got, err)
	}
	n := newChecked(t)
	got2, err := normalize.ApplyWith(n, []string{" ok ", "admin"}, "dive,trim,noadmin")
	var fe *normalize.FieldError
	if !errors.As(err, &fe) || fe.Path != "[1]" || got2[0] != "ok" {
		t.Errorf("ApplyWith: %q %v", got2, err)
	}
}

var cleanEmail = normalize.MustCompile[Email]("trim,email")

func TestRule(t *testing.T) {
	got, err := cleanEmail.Apply("  Bob@MAIL.Example ")
	if err != nil || got != "Bob@mail.example" {
		t.Errorf("Apply: %q %v", got, err)
	}
	e := Email(" X@Y.Z ")
	if err := cleanEmail.Var(&e); err != nil || e != "X@y.z" {
		t.Errorf("Var: %q %v", e, err)
	}
	if err := cleanEmail.Var(nil); !errors.Is(err, normalize.ErrInvalidArgument) {
		t.Errorf("nil: %v", err)
	}

	if _, err := normalize.Compile[int]("trim"); !errors.Is(err, normalize.ErrUnsupportedField) {
		t.Errorf("Compile on int: %v", err)
	}
	func() {
		defer func() {
			err, _ := recover().(error)
			var te *normalize.TagError
			if !errors.Is(err, normalize.ErrUnknownOperation) || !errors.As(err, &te) {
				t.Errorf("MustCompile must panic with the tag error, got %v", err)
			}
		}()
		normalize.MustCompile[string]("nope")
	}()

	n := newChecked(t)
	rule := normalize.MustCompileWith[string](n, "trim,noadmin")
	if _, err := rule.Apply(" admin "); !errors.Is(err, errNotAllowed) {
		t.Errorf("custom operation: %v", err)
	}
	if err := n.Register("noadmin", strings.ToUpper); err != nil {
		t.Fatal(err)
	}
	if _, err := rule.Apply("admin"); !errors.Is(err, errNotAllowed) {
		t.Errorf("a Rule keeps the operations it was compiled with: %v", err)
	}
	if _, err := normalize.CompileWith[string](n, "nope"); !errors.Is(err, normalize.ErrUnknownOperation) {
		t.Errorf("CompileWith: %v", err)
	}
}
