package normalize_test

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/go-deps/normalize"
)

func Example() {
	type SignUp struct {
		Email    string   `normalize:"trim,lower"`
		Name     string   `normalize:"trim,collapse"`
		Password string   // no tag: left exactly as typed
		Tags     []string `normalize:"dive,trim,lower"`
	}

	in := SignUp{
		Email:    "  Alice@Example.COM ",
		Name:     " Ada   Lovelace ",
		Password: " p@ss word ",
		Tags:     []string{" Go ", "RUST "},
	}
	if err := normalize.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%q %q %q %q\n", in.Email, in.Name, in.Password, in.Tags)
	// Output: "alice@example.com" "Ada Lovelace" " p@ss word " ["go" "rust"]
}

func ExampleNormalizer_Register() {
	n := normalize.New()
	err := n.Register("digits", func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsDigit(r) {
				return r
			}
			return -1
		}, s)
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	in := struct {
		Phone string `normalize:"digits"`
	}{Phone: "+1 (555) 010-99"}
	if err := n.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(in.Phone)
	// Output: 155501099
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
	// Output: normalize: normalize_test.Input.Name: tag "trim,lowr": unknown operation "lowr"
}
