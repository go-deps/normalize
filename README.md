# normalize

[![Go Reference](https://pkg.go.dev/badge/github.com/go-deps/normalize.svg)](https://pkg.go.dev/github.com/go-deps/normalize)
[![CI](https://github.com/go-deps/normalize/actions/workflows/ci.yml/badge.svg)](https://github.com/go-deps/normalize/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/go-deps/normalize/graph/badge.svg)](https://codecov.io/gh/go-deps/normalize)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/go-deps/normalize/badge)](https://scorecard.dev/viewer/?uri=github.com/go-deps/normalize)
[![Release](https://img.shields.io/github/v/tag/go-deps/normalize?sort=semver)](https://github.com/go-deps/normalize/tags)
[![Go version](https://img.shields.io/github/go-mod/go-version/go-deps/normalize)](go.mod)
[![License](https://img.shields.io/github/license/go-deps/normalize)](LICENSE)

Struct-tag driven normalization of string fields for Go: trim, lower-case and
collapse white space in decoded input before it is validated and stored.

- Standard library only, no dependencies.
- Tags are parsed once per type and cached; steady-state normalization does not allocate.
- Malformed tags are returned as errors, never panics, and are reported for the
  whole type on the first call, including nested types behind nil pointers.
- Works with nested structs, pointers, slices, arrays and wrapper types such as
  optional values.

## Install

```sh
go get github.com/go-deps/normalize
```

Requires Go 1.26 or later.

## Usage

```go
type SignUp struct {
	Email    string   `normalize:"trim,lower"`
	Name     string   `normalize:"trim,collapse"`
	Password string   // no tag: left exactly as typed
	Tags     []string `normalize:"dive,trim,lower"`
}

in := SignUp{
	Email: "  Alice@Example.COM ",
	Name:  " Ada   Lovelace ",
	Tags:  []string{" Go ", "RUST "},
}
if err := normalize.Struct(&in); err != nil {
	return err // a malformed tag
}
// in.Email == "alice@example.com"
// in.Name  == "Ada Lovelace"
// in.Tags  == []string{"go", "rust"}
```

### Operations

| Name       | Effect                                                    |
|------------|-----------------------------------------------------------|
| `trim`     | `strings.TrimSpace`                                       |
| `lower`    | `strings.ToLower`                                         |
| `upper`    | `strings.ToUpper`                                         |
| `collapse` | replace each run of Unicode white space with one space    |
| `dive`     | apply the operations that follow to each slice/array item |
| `-`        | skip the field entirely                                   |

Operations run from left to right.

### Custom operations

```go
n := normalize.New()
_ = n.Register("digits", func(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
})

in := struct {
	Phone string `normalize:"digits"`
}{Phone: "+1 (555) 010-99"}
_ = n.Struct(&in) // in.Phone == "155501099"
```

A different tag key is set with `normalize.New(normalize.WithTagName("clean"))`.

### Wrapper types

A type that wraps a value implements `Unwrapper`, and the field's operations
reach the wrapped value:

```go
type Optional[T any] struct {
	Value T
	Set   bool
}

func (o *Optional[T]) NormalizeTarget() any {
	if !o.Set {
		return nil // nothing to normalize
	}
	return &o.Value
}

type Patch struct {
	Title Optional[string] `normalize:"trim"`
}
```

The wrapper does not import this package.

### Catching tag typos in tests

Every malformed tag in a type is reported on the first call, even for a zero
value. One test over all input types is enough:

```go
func TestNormalizeTags(t *testing.T) {
	for _, in := range []any{&SignUp{}, &Patch{}} {
		if err := normalize.Struct(in); err != nil {
			t.Error(err)
		}
	}
}
```

Errors wrap `ErrUnknownOperation`, `ErrUnsupportedField` or `ErrMalformedTag`
inside a `*TagError` that names the type, field and tag.

## What is visited

Exported fields, and exported fields promoted through embedded structs.
Strings get the field's operations; pointers are followed when non-nil; nested
structs and structs inside slices and arrays are traversed without a tag.
Maps, interfaces, channels and functions are not visited. Cyclic values are
safe: a struct reached again through a pointer is walked once. Custom
operations should be idempotent, since a string shared by two fields gets both
fields' operations.

## License

[MIT](LICENSE)
