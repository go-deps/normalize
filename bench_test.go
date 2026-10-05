package normalize_test

import (
	"testing"

	"github.com/go-deps/normalize"
)

type benchItem struct {
	Name string  `normalize:"trim"`
	Unit *string `normalize:"trim"`
}

type benchInput struct {
	Title       string           `normalize:"trim"`
	Description Optional[string] `normalize:"trim"`
	Email       string           `normalize:"trim,lower"`
	Items       []benchItem
	Tags        []string `normalize:"dive,trim,lower"`
}

func newBenchInput() benchInput {
	unit := " g "
	items := make([]benchItem, 20)
	for i := range items {
		items[i] = benchItem{Name: " flour ", Unit: &unit}
	}
	return benchInput{
		Title:       "  Pancakes ",
		Description: some(" Fluffy. "),
		Email:       " Cook@Example.com ",
		Items:       items,
		Tags:        []string{" Breakfast ", "Sweet"},
	}
}

func BenchmarkStruct(b *testing.B) {
	in := newBenchInput()
	b.ReportAllocs()
	for b.Loop() {
		if err := normalize.Struct(&in); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStructDirty restores untrimmed values before every call, so each
// operation has work to do; BenchmarkStruct measures already clean input.
func BenchmarkStructDirty(b *testing.B) {
	in := newBenchInput()
	unit := " g "
	b.ReportAllocs()
	for b.Loop() {
		in.Title, in.Description.Value, in.Email = "  Pancakes ", " Fluffy. ", " Cook@Example.com "
		for i := range in.Items {
			in.Items[i].Name, in.Items[i].Unit = " flour ", &unit
		}
		in.Tags[0], in.Tags[1] = " Breakfast ", "Sweet"
		if err := normalize.Struct(&in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStructParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		in := newBenchInput()
		for pb.Next() {
			if err := normalize.Struct(&in); err != nil {
				b.Fatal(err)
			}
		}
	})
}
