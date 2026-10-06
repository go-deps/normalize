package normalize_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-deps/normalize"
)

func Example() {
	type SignUp struct {
		Email    string   `normalize:"email"`
		Name     string   `normalize:"trim,collapse,truncate=64,trimright"`
		Username string   `normalize:"trim,lower,keep=ascii,alnum"`
		Password string   // no tag: left exactly as typed
		Tags     []string `normalize:"dive,trim,slug"`
	}

	in := SignUp{
		Email:    "  Alice@Example.COM ",
		Name:     " Ada   Lovelace ",
		Username: " Ada_Lovelace! ",
		Password: " p@ss word ",
		Tags:     []string{" Go Lang ", "RUST "},
	}
	if err := normalize.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%q %q %q %q %q\n", in.Email, in.Name, in.Username, in.Password, in.Tags)
	// Output: "Alice@example.com" "Ada Lovelace" "adalovelace" " p@ss word " ["go-lang" "rust"]
}

func Example_maps() {
	type Recipe struct {
		Labels map[string]string   `normalize:"keys(trim,lower),dive,trim"`
		Steps  map[string][]string `normalize:"dive,dive,trim,capfirst"`
	}

	in := Recipe{
		Labels: map[string]string{" EN ": " Soup "},
		Steps:  map[string][]string{"en": {" boil water", "add salt "}},
	}
	if err := normalize.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%q %q\n", in.Labels, in.Steps)
	// Output: map["en":"Soup"] map["en":["Boil water" "Add salt"]]
}

func Example_directives() {
	type Query struct {
		Sort   string   `normalize:"trim,lower,default=name"`
		Limit  *int     `normalize:"default=20"`
		Search *string  `normalize:"trim,nilempty"`
		Tags   []string `normalize:"nilempty"`
	}

	blank := "   "
	in := Query{Sort: "  ", Search: &blank, Tags: []string{}}
	if err := normalize.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(in.Sort, *in.Limit, in.Search == nil, in.Tags == nil)
	// Output: name 20 true true
}

func ExampleVar() {
	email := "  Bob@Example.ORG "
	if err := normalize.Var(&email, "email"); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(email)
	// Output: Bob@example.org
}

func ExampleApply() {
	key, err := normalize.Apply("  Prep Time (min) ", "case=snake")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(key)
	// Output: prep_time_min
}

func ExampleMustCompile() {
	var cleanTags = normalize.MustCompile[[]string]("nilempty,dive,trim,lower")

	tags, err := cleanTags.Apply([]string{" Soup", "VEGAN "})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%q\n", tags)
	// Output: ["soup" "vegan"]
}

var errTooShort = errors.New("too short")

func ExampleFieldError() {
	n := normalize.New(normalize.WithFieldName(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		return name
	}))
	_ = n.RegisterCheck("min3", func(s string) (string, error) {
		if len([]rune(s)) < 3 {
			return s, errTooShort
		}
		return s, nil
	})

	type Item struct {
		Name string `json:"name" normalize:"trim,min3"`
	}
	type Order struct {
		Items []Item `json:"items"`
	}

	in := Order{Items: []Item{{Name: " Tea "}, {Name: " x "}}}
	err := n.Struct(&in)

	var fe *normalize.FieldError
	if errors.As(err, &fe) {
		fmt.Println(fe.Path, fe.Op, errors.Is(err, errTooShort))
	}
	fmt.Printf("%q %q\n", in.Items[0].Name, in.Items[1].Name)
	// Output:
	// items[1].name min3 true
	// "Tea" " x "
}

func ExampleNormalizer_RegisterParam() {
	n := normalize.New()
	err := n.RegisterParam("pad", func(arg string) (normalize.CheckFunc, error) {
		if len(arg) != 1 {
			return nil, fmt.Errorf("want one character, got %q", arg)
		}
		return func(s string) (string, error) {
			return strings.TrimLeft(s, arg), nil
		}, nil
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	code, _ := normalize.ApplyWith(n, "000042", "pad=0")
	_, err = normalize.CompileWith[string](n, "pad=ab")
	fmt.Println(code)
	fmt.Println(err)
	// Output:
	// 42
	// normalize: string: tag "pad=ab": malformed tag: pad=ab: want one character, got "ab"
}

type Contact struct {
	Phone   string `normalize:"digits"`
	Country string `normalize:"trim,upper"`
}

// AfterNormalize adds the country code once fields are normalized, a rule
// that depends on two fields.
func (c *Contact) AfterNormalize() error {
	if c.Country == "US" && len(c.Phone) == 10 {
		c.Phone = "1" + c.Phone
	}
	return nil
}

func ExampleAfterNormalizer() {
	in := Contact{Phone: "(555) 010-9999", Country: " us "}
	if err := normalize.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(in.Phone, in.Country)
	// Output: 15550109999 US
}

// Maybe is an optional value that takes part in normalization through
// Unwrapper.
type Maybe[T any] struct {
	Value T
	Valid bool
}

func (m *Maybe[T]) NormalizeTarget() any {
	if !m.Valid {
		return nil
	}
	return &m.Value
}

func ExampleUnwrapper() {
	type Patch struct {
		Title       Maybe[string] `normalize:"trim"`
		Description Maybe[string] `normalize:"trim"`
	}

	in := Patch{Title: Maybe[string]{Value: "  New title ", Valid: true}}
	if err := normalize.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%q %v %q %v\n", in.Title.Value, in.Title.Valid, in.Description.Value, in.Description.Valid)
	// Output: "New title" true "" false
}

func ExampleTagError() {
	type Input struct {
		Name string `normalize:"trim,lowr"`
	}
	err := normalize.Struct(&Input{})
	fmt.Println(err)
	fmt.Println(errors.Is(err, normalize.ErrUnknownOperation))
	// Output:
	// normalize: normalize_test.Input.Name: tag "trim,lowr": unknown operation "lowr"
	// true
}

// shout is a plugin with one operation. A plugin with settings would carry
// them in its fields.
type shout struct{}

func (shout) Name() string { return "example.com/shout" }

func (shout) Register(r *normalize.Registry) error {
	return r.Register("shout", strings.ToUpper)
}

// loud adds an operation of the same name.
type loud struct{}

func (loud) Name() string { return "example.com/loud" }

func (loud) Register(r *normalize.Registry) error {
	return r.Register("shout", func(s string) string { return strings.ToUpper(s) + "!" })
}

func ExamplePlugin() {
	n := normalize.New(normalize.WithPlugins(shout{}))

	s, err := normalize.ApplyWith(n, " hi ", "trim,shout")
	fmt.Println(s, err)
	// Output: HI <nil>
}

func ExampleNormalizer_Use() {
	n := normalize.New()
	fmt.Println(n.Use(shout{}))
	fmt.Println(n.Use(shout{})) // the same plugin again does nothing
	err := n.Use(loud{})
	fmt.Println(errors.Is(err, normalize.ErrConflict))
	fmt.Println(err)
	// Output:
	// <nil>
	// <nil>
	// true
	// normalize: Use: plugin conflict: shout (plugin example.com/loud, already added by example.com/shout)
}

func ExampleWithTagName() {
	n := normalize.New(normalize.WithTagName("normalize", "norm", "n"))

	in := struct {
		A string `normalize:"trim"`
		B string `norm:"trim"`
		C string `n:"trim"`
	}{A: " a ", B: " b ", C: " c "}
	if err := n.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%q %q %q\n", in.A, in.B, in.C)
	// Output: "a" "b" "c"
}
