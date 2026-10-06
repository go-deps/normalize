// Package normalize rewrites strings in Go values in place, driven by struct
// tags, so that decoded input reaches validation and storage in one
// canonical form:
//
//	type SignUp struct {
//		Email    string   `normalize:"email"`
//		Name     string   `normalize:"trim,collapse,truncate=64,trimright"`
//		Password string   // no tag: left exactly as typed
//		Tags     []string `normalize:"dive,trim,slug"`
//	}
//
//	if err := normalize.Struct(&in); err != nil { ... }
//
// Single values are normalized with [Var], [Apply] or a compiled [Rule]:
//
//	email, err := normalize.Apply(raw, "email")
//
// The package changes values; it does not validate them. Run a validator
// after it.
//
// # Tags
//
// A tag is a comma-separated list of elements applied from left to right, so
// truncate comes after the operations that change the text, followed by
// trimright if the cut may end in a space. An element is an operation name,
// or a name and an argument after "=". An argument that contains a comma, a
// parenthesis or a single quote is wrapped in single quotes, and a quote
// inside it is written twice: trim=',;' or cutsuffix=”'s'. The tag keys are
// "normalize" and its short form "norm"; [WithTagName] changes them.
//
// Operations without an argument:
//
//	trim, trimleft, trimright   remove leading and/or trailing white space
//	lower, upper                change case
//	collapse                    replace each run of white space with one space
//	newline                     convert CRLF, CR, NEL, U+2028 and U+2029 to LF
//	capfirst                    title-case the first character
//	slug                        lower-case words joined with hyphens
//	email                       trim, and lower-case the domain
//	digits, letters, alnum      keep only those characters
//	nocontrol                   remove control characters other than white space
//
// Operations with an argument:
//
//	trim=<chars>, trimleft=<chars>, trimright=<chars>
//	                            remove the given characters instead of white space
//	cutprefix=<s>, cutsuffix=<s>
//	                            remove a prefix or suffix, however often repeated
//	keep=<classes>, remove=<classes>
//	                            keep or remove character classes, joined with |:
//	                            letter, upper, lower, digit, number, space, punct,
//	                            symbol, mark, control, invisible, ascii, print
//	collapse=inline             collapse white space but keep line breaks
//	case=<style>                snake, kebab, dot, constant, camel, pascal, train
//	truncate=<n>                keep the first n characters
//
// Every built-in operation is idempotent. Bytes that are not valid UTF-8 are
// dropped by slug, case and the operations that remove characters, may become
// U+FFFD in lower, upper and capfirst, as with strings.ToLower, and are left
// as they are by the others. More operations are added with
// [Normalizer.Register], [Normalizer.RegisterCheck] and
// [Normalizer.RegisterParam], or as a [Plugin].
//
// Directives shape the value rather than the text:
//
//	dive              apply what follows to each element of a slice, array
//	                  or map; a second dive reaches the elements of the
//	                  elements
//	keys(...)         apply operations to the keys of a map with string keys
//	nilempty          set a pointer, slice or map to nil when it is empty
//	default=<value>   set a nil pointer, or a zero string, bool or number,
//	                  to value
//	-                 skip the field; must be the whole tag
//
// # What is visited
//
// Struct visits exported fields and the fields of embedded structs. Nested
// structs, pointers, slices, arrays and maps are traversed without a tag, so
// structs inside them get their own fields' tags; strings inside containers
// are changed only after dive. Interfaces, channels and functions are not
// visited. A type that wraps a value, such as an optional field, takes part
// by implementing [Unwrapper]; a struct with rules that span several fields
// implements [AfterNormalizer]. A struct's own AfterNormalize is always
// called; one promoted from an embedded field is skipped when a pointer or
// interface on the way to it is nil, and a struct reached through an
// unexported embedded field is not called.
//
// Cyclic and shared values are safe: a struct, container or wrapped value
// reached again through a pointer is walked once. A string shared by two
// fields gets both fields' operations, which is harmless as long as
// operations are idempotent. A value that is already normalized is only
// read, never written, so clean values shared between goroutines are safe.
//
// # Errors
//
// Tags are parsed once per type, on the first call, and cached. A tag that
// cannot be applied anywhere in the type, including in nested types behind
// nil pointers, is reported as a *[TagError] and nothing is changed, so one
// test that passes the zero value of each input type catches tag typos. The
// only types not checked in advance are those reached through
// [Unwrapper.NormalizeTarget]; their tag errors are reported at run time as a
// *[FieldError] wrapping the *TagError.
//
// At run time an operation may reject a value, as an operation registered
// with [Normalizer.RegisterCheck] does. The value is left as it was, the walk
// goes on, and each failure is reported as a *[FieldError] whose Path
// locates the value, as in Items[1].Name or Labels["en"]. Several errors are
// joined with [errors.Join]. A call reports at most 100 errors and walks at
// most 10000 levels deep; [WithMaxErrors] and [WithMaxDepth] change the
// limits.
//
// # Concurrency
//
// A [Normalizer] is safe for concurrent use. Register operations and plugins
// at start-up; registering later is safe but discards the type cache. Once a
// type is cached, [Normalizer.Struct] on clean input does not allocate,
// except for a few allocations per map it visits; changed strings and
// errors allocate.
//
// # Panics
//
// [MustCompile] and [MustCompileWith] panic on a tag error, and [New] panics
// when the plugins given with [WithPlugins] conflict. No other function
// panics on its input; a panic in a registered operation, an
// AfterNormalize method or an Unwrapper is not recovered.
package normalize
