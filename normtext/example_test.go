package normtext_test

import (
	"fmt"

	"github.com/go-deps/normalize"
	"github.com/go-deps/normalize/normtext"
)

func Example() {
	n := normalize.New(normalize.WithPlugins(normtext.Plugin()))

	type Account struct {
		Username string `normalize:"trim,precis=username"`
		Name     string `normalize:"trim,nfc,collapse"`
		City     string `normalize:"trim,unaccent,fold"`
	}
	in := Account{Username: " Ａlice ", Name: "Rene\u0301  Magritte", City: " São Paulo "}
	if err := n.Struct(&in); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%q %q %q\n", in.Username, in.Name, in.City)
	// Output: "alice" "René Magritte" "sao paulo"
}

func Example_language() {
	n := normalize.New(normalize.WithPlugins(normtext.Plugin()))

	city, err := normalize.ApplyWith(n, "İZMİR", "lower=tr,title=tr")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(city)
	// Output: İzmir
}
