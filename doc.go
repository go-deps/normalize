// Package normalize rewrites string fields of Go structs in place, driven by
// struct tags.
//
// It is meant to run on decoded user input before validation, so that values
// such as "  Alice@Example.COM " reach validation and storage in one canonical
// form:
//
//	type SignUp struct {
//		Email    string   `normalize:"trim,lower"`
//		Name     string   `normalize:"trim,collapse"`
//		Password string   // no tag: left exactly as typed
//		Tags     []string `normalize:"dive,trim,lower"`
//	}
//
//	in := SignUp{Email: "  Alice@Example.COM ", Name: " Ada   Lovelace "}
//	if err := normalize.Struct(&in); err != nil {
//		// a malformed tag; see TagError
//	}
//	// in.Email == "alice@example.com", in.Name == "Ada Lovelace"
//
// # Tags
//
// The tag value is a comma-separated list of operation names, applied from
// left to right. The built-in operations are:
//
//	trim      strings.TrimSpace
//	lower     strings.ToLower
//	upper     strings.ToUpper
//	collapse  replace every run of Unicode white space with a single space
//
// Further operations are added with [Normalizer.Register]. Two names are
// reserved:
//
//   - skip the field entirely, including any nested structs
//     dive  apply the operations that follow to each element of a slice or array
//
// # What is visited
//
// Struct normalizes exported fields, and exported fields promoted through
// embedded structs. For each field:
//
//   - strings (including named string types) get the field's operations;
//   - pointers are followed when non-nil, and the operations apply to the
//     pointed-to value;
//   - nested structs, and structs inside slices and arrays, are traversed
//     without any tag;
//   - string elements of slices and arrays are changed only after dive;
//   - maps, interfaces, channels and functions are left untouched, and a tag
//     with operations on them is an error.
//
// A type that wraps a value, such as an optional field, takes part by
// implementing [Unwrapper].
//
// # Errors
//
// Tags are parsed once per type, the first time the type is seen, and the
// result is cached. An unknown operation, an operation on a field that cannot
// hold it, or a misplaced dive is reported as a *[TagError] from that first
// call onward; Struct never panics on a malformed tag. A test that calls
// Struct on the zero value of every input type therefore catches tag typos
// before they reach production.
//
// Cyclic and shared values are safe: a struct reached again through a pointer,
// or a slice of structs reached again, is walked once. Strings carry the
// operations of the field that reaches them, so a string shared by two fields
// gets both fields' operations; operations should therefore be idempotent, as
// every built-in operation is.
//
// # Concurrency
//
// A [Normalizer] is safe for concurrent use. Register operations before the
// first call to [Normalizer.Struct]; registering later is safe but discards
// the type cache.
package normalize
