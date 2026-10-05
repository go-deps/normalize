# normalize

[![Go Reference](https://pkg.go.dev/badge/github.com/go-deps/normalize.svg)](https://pkg.go.dev/github.com/go-deps/normalize)
[![CI](https://github.com/go-deps/normalize/actions/workflows/ci.yml/badge.svg)](https://github.com/go-deps/normalize/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/go-deps/normalize/graph/badge.svg)](https://codecov.io/gh/go-deps/normalize)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/go-deps/normalize/badge)](https://scorecard.dev/viewer/?uri=github.com/go-deps/normalize)
[![Release](https://img.shields.io/github/v/tag/go-deps/normalize?sort=semver&filter=v*)](https://github.com/go-deps/normalize/tags)
[![Go version](https://img.shields.io/github/go-mod/go-version/go-deps/normalize)](go.mod)
[![License](https://img.shields.io/github/license/go-deps/normalize)](LICENSE)

Struct-tag driven normalization for Go: trim, re-case, filter, slugify and
reshape decoded input in place, so that it reaches validation and storage in
one canonical form.

```go
type SignUp struct {
	Email    string   `normalize:"email"`
	Name     string   `normalize:"trim,collapse,truncate=64,trimright"`
	Username string   `normalize:"trim,lower,keep=ascii,alnum"`
	Password string   // no tag: left exactly as typed
	Tags     []string `normalize:"dive,trim,slug"`
}

// {"  Alice@Example.COM ", " Ada   Lovelace ", " Ada_Lovelace! ", ..., [" Go Lang ", "RUST "]}
err := normalize.Struct(&in)
// {"Alice@example.com", "Ada Lovelace", "adalovelace", ..., ["go-lang", "rust"]}
```

- **No dependencies.** The core uses the standard library only; operations
  that need more, such as Unicode normalization forms and PRECIS profiles,
  live in separate modules and are added as plugins.
- **Fast.** Tags are parsed once per type and cached; normalizing clean
  input does not allocate, apart from a few allocations per map visited.
- **Safe.** A malformed tag is an error, not a panic (only the `Must*`
  helpers, and `New` with conflicting plugins, panic), and is reported for
  the whole type on the first call,
  nested types behind nil pointers included. Cyclic values are walked once,
  depth and error counts are limited, and clean values are only read.
- **Thorough.** Nested structs, pointers, slices, arrays, maps and map keys,
  optional-value wrappers, defaults, nil-on-empty, cross-field hooks, and
  errors that carry the path of the value, as in `Items[1].Name`.
- **Not only structs.** `Var`, `Apply` and `Rule` normalize a single value.

normalize changes values; it does not validate them. Run a validator after
it.

**Status:** v0.x. The API may still change between minor versions; changes
are listed in the [changelog](CHANGELOG.md).

## Install

```sh
go get github.com/go-deps/normalize
```

Requires Go 1.26 or later.

## Contents

- [Tag syntax](#tag-syntax)
- [Operations](#operations)
- [Containers and map keys](#containers-and-map-keys)
- [Defaults and nil](#defaults-and-nil)
- [Single values](#single-values)
- [Errors](#errors)
- [Cross-field rules](#cross-field-rules)
- [Wrapper types](#wrapper-types)
- [Custom operations](#custom-operations)
- [Plugins](#plugins)
- [Tag names](#tag-names)
- [Using it with HTTP frameworks](#using-it-with-http-frameworks)
- [What is visited](#what-is-visited)
- [Limits](#limits)
- [Security notes](#security-notes)
- [Modules](#modules)
- [Contributing and security](#contributing-and-security)

## Tag syntax

A tag is a comma-separated list of elements applied from left to right:

```go
Name string `normalize:"trim,collapse,truncate=64,trimright"`
```

- An element is an operation name (`trim`) or a name with an argument after
  `=` (`truncate=64`).
- An argument containing a comma, a parenthesis or a single quote is
  wrapped in single quotes; a quote inside it is written twice:
  `trim=',;'`, `cutsuffix=' (copy)'`, `cutsuffix='''s'` (removes `'s`).
- Order matters: put `truncate` after the operations that change the text,
  and follow it with `trimright` if the cut may end in a space.
- Directives shape the value rather than the text:
  - `dive` applies what follows to the elements of a slice, array or map;
  - `keys(op,op)` applies operations to the keys of a map;
  - `nilempty` and `default=value` are described in
    [Defaults and nil](#defaults-and-nil);
  - `-` as the whole tag skips the field.

The tag key is `normalize`, or its short form `norm`; see [Tag names](#tag-names).

## Operations

Operations without an argument:

| Operation   | Effect                                                         | Example                                   |
|-------------|----------------------------------------------------------------|-------------------------------------------|
| `trim`      | remove leading and trailing white space                        | `"  a b  "` → `"a b"`                     |
| `trimleft`  | remove leading white space                                     | `"  a "` → `"a "`                         |
| `trimright` | remove trailing white space                                    | `" a  "` → `" a"`                         |
| `lower`     | lower-case                                                     | `"ÀB"` → `"àb"`                           |
| `upper`     | upper-case                                                     | `"àb"` → `"ÀB"`                           |
| `collapse`  | replace each run of white space with one space                 | `"a \t\n b"` → `"a b"`                    |
| `newline`   | convert CRLF, CR, NEL, U+2028 and U+2029 to LF                 | `"a\r\nb\rc"` → `"a\nb\nc"`               |
| `capfirst`  | title-case the first character, keep the rest                  | `"élan vital"` → `"Élan vital"`           |
| `slug`      | lower-case letters and digits joined with hyphens              | `" Crème Brûlée! "` → `"crème-brûlée"`    |
| `email`     | trim, lower-case the domain, keep the local part as typed      | `" Bob@Example.ORG"` → `"Bob@example.org"`|
| `digits`    | keep digits only                                               | `"+1 (555) 010"` → `"1555010"`            |
| `letters`   | keep letters only                                              | `"R2-D2 droid"` → `"RDdroid"`             |
| `alnum`     | keep letters and digits only                                   | `"R2-D2!"` → `"R2D2"`                     |
| `nocontrol` | remove control characters other than white space               | `"a\x00b\tc"` → `"ab\tc"`                 |

Operations with an argument:

| Operation                    | Effect                                                         | Example                                         |
|------------------------------|----------------------------------------------------------------|-------------------------------------------------|
| `trim=<chars>`               | remove the given characters from both ends                     | `trim=-_`: `"--a_b__"` → `"a_b"`                |
| `trimleft=<chars>`           | … from the start                                               | `trimleft=0`: `"007"` → `"7"`                   |
| `trimright=<chars>`          | … from the end                                                 | `trimright=.`: `"end..."` → `"end"`             |
| `cutprefix=<s>`              | remove a prefix, however often repeated                        | `cutprefix=www.`: `"www.go.dev"` → `"go.dev"`   |
| `cutsuffix=<s>`              | remove a suffix, however often repeated                        | `cutsuffix=/`: `"/a//"` → `"/a"`                |
| `keep=<classes>`             | keep only the characters of the classes                        | `keep=letter\|space`: `"a1 b2"` → `"a b"`       |
| `remove=<classes>`           | remove the characters of the classes                           | `remove=punct\|symbol`: `"a+b!"` → `"ab"`       |
| `collapse=inline`            | collapse white space within lines, keep line breaks            | `"a  b\n\nc"` → `"a b\n\nc"`                    |
| `case=<style>`               | convert identifier style                                       | `case=snake`: `"prepTime (min)"` → `"prep_time_min"` |
| `truncate=<n>`               | keep the first n characters (runes)                            | `truncate=3`: `"héllo"` → `"hél"`               |

Character classes for `keep` and `remove`, joined with `|`: `letter`,
`upper`, `lower`, `digit`, `number`, `space`, `punct`, `symbol`, `mark`,
`control` (control characters other than white space), `invisible` (format
characters such as zero-width spaces and joiners), `ascii` and `print`.

Styles for `case`:

| Style      | Result for `"HTTP server v2"` |
|------------|-------------------------------|
| `snake`    | `http_server_v2`              |
| `kebab`    | `http-server-v2`              |
| `dot`      | `http.server.v2`              |
| `constant` | `HTTP_SERVER_V2`              |
| `camel`    | `httpServerV2`                |
| `pascal`   | `HTTPServerV2`                |
| `train`    | `Http-Server-V2`              |

Words are split at any character other than a letter, digit or mark, and at
changes of case when the input has lower-case letters (`prepTime`,
`HTTPServer`). Words without lower-case letters, such as `HTTP` or `ID`, stay
upper-case in `pascal`, and in `camel` after the first word (`user ID` →
`userID`).

Every built-in operation is idempotent: applying it twice gives the same
result as once. Bytes that are not valid UTF-8 are dropped by `slug`, `case=`
and the operations that remove characters, may become U+FFFD in `lower`,
`upper` and `capfirst` (as with `strings.ToLower`), and are left as they are
by the others.

## Containers and map keys

Strings inside slices, arrays and maps are changed only after `dive`. Each
`dive` goes one level deeper, and operations before the first `dive` apply to
the container itself:

```go
type Recipe struct {
	Tags   []string            `normalize:"dive,trim,lower"`
	Labels map[string]string   `normalize:"keys(trim,lower),dive,trim"`
	Steps  map[string][]string `normalize:"dive,dive,trim,capfirst"`
}

// Labels: {" EN ": " Soup "}                   → {"en": "Soup"}
// Steps:  {"en": [" boil water", "add salt "]} → {"en": ["Boil water", "Add salt"]}
```

`keys(...)` applies to maps with string keys. If two keys would become
equal, or a key operation fails, the keys are left unchanged and a
`FieldError` wrapping `ErrKeyCollision` (or the operation's error) is
reported; the values are still normalized. Structs inside containers need
no `dive`: they are always walked and get their own fields' tags.

## Defaults and nil

```go
type Query struct {
	Sort   string   `normalize:"trim,lower,default=name"` // "  " → "name"
	Limit  *int     `normalize:"default=20"`              // nil → pointer to 20
	Search *string  `normalize:"trim,nilempty"`           // pointer to "   " → nil
	Tags   []string `normalize:"nilempty"`                // []string{} → nil
}
```

- `default=value` sets a string that is empty after the operations, a zero
  bool or number, or a nil pointer to one of them. A non-nil pointer is
  kept as it is, so an explicit `false` or `0` sent by a client survives.
  The value is parsed for the field type when the tag is first read; integers
  accept the `0x`, `0o` and `0b` prefixes.
- `nilempty` sets a pointer to nil when the value it points to is empty after
  normalization, and a slice or map to nil when it has no elements. Placed
  after a `dive`, it applies to the elements.
- `default` and `nilempty` exclude each other at one level.

## Single values

```go
// In place.
err := normalize.Var(&email, "email")

// By value; works for any type, including named string types.
key, err := normalize.Apply("  Prep Time (min) ", "case=snake") // "prep_time_min"

// Parsed and checked once, at start-up.
var cleanTags = normalize.MustCompile[[]string]("nilempty,dive,trim,lower")

tags, err := cleanTags.Apply([]string{" Soup", "VEGAN "}) // ["soup" "vegan"]
```

A `Rule` keeps the operations it was compiled with and is safe for concurrent
use. `Var` and `Apply` cache parsed tags by type and text, so pass them
constant tags; `ApplyWith`, `CompileWith` and `MustCompileWith` take a
`*Normalizer` with custom operations or plugins.

## Errors

There are two kinds of errors.

**Tag errors** are programming errors. A tag that names an unknown
operation, does not fit the field type or cannot be parsed is reported as a
`*TagError` wrapping `ErrUnknownOperation`, `ErrUnsupportedField` or
`ErrMalformedTag`, and nothing is changed:

```
normalize: main.Input.Name: tag "trim,lowr": unknown operation "lowr"
```

Every tag of a type is checked on the first call, even for a zero value, so
one test catches typos in all input types:

```go
func TestNormalizeTags(t *testing.T) {
	for _, in := range []any{&SignUp{}, &Recipe{}, &Query{}} {
		if err := normalize.Struct(in); err != nil {
			t.Error(err)
		}
	}
}
```

**Field errors** come from values. The value is left as it was, the walk
goes on, and each failure is reported as a `*FieldError` with the path of
the value; several are joined with `errors.Join`. The cause, in `Err`, is
one of:

- the error of an operation registered with `RegisterCheck` or
  `RegisterParam` that rejected its input;
- the error of an `AfterNormalize` method;
- `ErrKeyCollision`, when normalized map keys collide;
- `ErrInvalidTarget`, when an `Unwrapper` returns something other than nil
  or a pointer;
- `ErrUnsupportedField`, for a map key that cannot be stored back, such as
  a NaN float key;
- `ErrTooDeep`, when the value is nested beyond the [limit](#limits);
- a `*TagError` of a type reached only through an `Unwrapper`, which cannot
  be checked in advance.

```go
n := normalize.New(normalize.WithFieldName(func(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return name // "" keeps the Go field name
}))

// min3 is an operation registered with n.RegisterCheck.
var fe *normalize.FieldError
if errors.As(err, &fe) {
	fmt.Println(fe.Path, fe.Op) // items[1].name min3
}
```

`WithFieldName` names path segments after JSON (or any other) names, so
errors can be returned to clients as they are.

## Cross-field rules

A struct implements `AfterNormalizer` for rules that tags cannot express. The
method is called on a pointer to the struct after all of its fields,
including nested structs, are normalized:

```go
type Contact struct {
	Phone   string `normalize:"digits"`
	Country string `normalize:"trim,upper"`
}

func (c *Contact) AfterNormalize() error {
	if c.Country == "US" && len(c.Phone) == 10 {
		c.Phone = "1" + c.Phone
	}
	return nil
}
```

An error it returns is reported as a `FieldError` with `Op` set to
`AfterNormalize`. The method is not called when it would be promoted
through a nil embedded pointer, which would give it a nil receiver, or when
the struct is reached through an unexported embedded field. When the outer
struct has the method, its own or promoted, the embedded struct's method is
not called separately.

## Wrapper types

A type that wraps a value, such as an optional field, implements `Unwrapper`
and the field's operations reach the wrapped value. The wrapper does not
import this package:

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

## Custom operations

```go
n := normalize.New()

// A plain operation.
n.Register("nozw", func(s string) string {
	return strings.ReplaceAll(s, "\u200b", "")
})

// An operation that can reject its input.
n.RegisterCheck("ascii", func(s string) (string, error) {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return s, errors.New("not ASCII")
		}
	}
	return s, nil
})

// An operation with an argument, parsed once when the tag is first read.
n.RegisterParam("pad", func(arg string) (normalize.CheckFunc, error) {
	if len(arg) != 1 {
		return nil, fmt.Errorf("want one character, got %q", arg)
	}
	return func(s string) (string, error) { return strings.TrimLeft(s, arg), nil }, nil
})
```

A name can have both a plain form and a form with an argument; the tag picks
one by the presence of `=`. Operations should be idempotent, since a string
shared by two fields gets both fields' operations. Registering replaces an
existing operation of the same name and form.

## Plugins

Operations with dependencies of their own live in separate modules and are
added to a `Normalizer` as plugins, so the core stays free of dependencies:

```go
import (
	"github.com/go-deps/normalize"
	"github.com/go-deps/normalize/normtext"
)

func init() {
	// The package-level Struct, Var, Apply and Compile.
	if err := normalize.Use(normtext.Plugin()); err != nil {
		panic(err)
	}
}

// Or a Normalizer of your own; New panics if the plugins conflict.
var n = normalize.New(normalize.WithPlugins(normtext.Plugin()))

type Account struct {
	Username string `normalize:"trim,precis=username"`
	City     string `normalize:"trim,unaccent,fold"`
}
```

A plugin may replace a built-in operation, so a new operation in this module
never breaks a plugin that already has one of that name. An operation added
with `Register` or by another plugin is a conflict: `Use` then returns an
error wrapping `ErrConflict` and changes nothing. Using an equal plugin value
again does nothing; another value under a name already in use, such as the
same plugin with other settings, is a conflict. Call `Use` from `main`: a
library shares the package-level `Normalizer` with the whole program, so it
should create its own with `New`.

Writing a plugin takes two methods:

```go
type Plugin interface {
	Name() string // by convention, the module path
	Register(r *normalize.Registry) error
}
```

`Registry` has the same `Register`, `RegisterCheck` and `RegisterParam` methods
as `Normalizer`. The `Plugin` interface will not gain methods, so plugins keep
compiling across versions; new capabilities come as `Registry` methods.

## Tag names

A `Normalizer` reads the `normalize` and `norm` keys. `WithTagName` replaces
them, for example to add the shortest form:

```go
n := normalize.New(normalize.WithTagName("normalize", "norm", "n"))

type Input struct {
	Name string `n:"trim"`
}
```

A field that carries more than one of the keys is a `TagError`.

## Using it with HTTP frameworks

Normalize after decoding and before validation.

**net/http and chi** (chi handlers are `net/http` handlers):

```go
func createRecipe(w http.ResponseWriter, r *http.Request) {
	var in CreateRecipe
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := normalize.Struct(&in); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	// validate and use in
}
```

**echo** (v5): `Bind` decodes, `Validate` runs the registered validator:

```go
func createRecipe(c *echo.Context) error {
	var in CreateRecipe
	if err := c.Bind(&in); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if err := normalize.Struct(&in); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}
	if err := c.Validate(&in); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, in)
}
```

**gin** validates `binding` tags while it binds, so normalization has to run
inside the validator to come first. Wrapping the validator once at start-up
does that for every `ShouldBind*` call:

```go
type normalizeFirst struct{ binding.StructValidator }

func (v normalizeFirst) ValidateStruct(obj any) error {
	if err := normalize.Var(obj, ""); err != nil {
		return err
	}
	return v.StructValidator.ValidateStruct(obj)
}

func main() {
	binding.Validator = normalizeFirst{binding.Validator}
	// set up routes; c.ShouldBindJSON(&in) now normalizes, then validates
}
```

`Var` with an empty tag is used instead of `Struct` because gin also
validates bodies that are not structs, such as slices of structs.

## What is visited

- Exported fields, and the fields of embedded structs. In error paths, an
  embedded struct adds no segment of its own.
- Strings, including named string types, get the field's operations.
- Pointers are followed when non-nil.
- Nested structs, and structs inside slices, arrays and maps, are walked
  without a tag.
- Strings inside slices, arrays and maps are changed only after `dive`.
- Interfaces, channels and functions are not visited.
- A struct's own `AfterNormalize` is always called; one promoted from an
  embedded field is skipped when a pointer or interface on the way to it is
  nil.
- A struct, container or wrapped value reached twice, as in a cycle, is
  walked once.
- A value that is already normalized is only read, never written, so clean
  values shared between goroutines, such as default maps, are safe.

A `Normalizer` is safe for concurrent use. Register operations and plugins at
start-up: registering later is safe but discards the type cache.

## Limits

A call walks at most 10000 levels deep and reports at most 100 errors, so
hostile or broken input cannot exhaust the stack or memory. Beyond the depth
limit a `FieldError` wrapping `ErrTooDeep` is reported and deeper values are
left unchanged; beyond the error limit one last error wrapping
`ErrTooManyErrors` tells how many more there were. Both limits are options;
the depth limit cannot exceed 100000:

```go
n := normalize.New(normalize.WithMaxDepth(100), normalize.WithMaxErrors(20))
```

Error paths are shortened: a map key longer than 32 bytes is cut and marked
with `…`, and a path longer than 512 bytes keeps its start and end.

## Security notes

- Normalization is not validation and not sanitization for HTML, SQL or
  shells; escape output for its context.
- Reject the input on any error. A value that an operation rejects is left
  as it was, and the rest of the struct is still normalized, so a struct
  returned with an error is only partly normalized.
- `nocontrol` and `remove=invisible` remove control and format characters
  (categories Cc and Cf), but not characters that look alike across scripts
  (`а` and `a`), nor blank-looking characters of other categories, such as
  U+3164 HANGUL FILLER, U+2800 BRAILLE PATTERN BLANK, U+034F COMBINING
  GRAPHEME JOINER and the variation selectors U+FE00 to U+FE0F. For
  identifiers, restrict the alphabet (`trim,lower,keep=ascii,alnum`) or use
  the PRECIS profiles of [`normtext`](normtext), and check confusables
  (Unicode TS #39) separately where it matters.
- Case mapping and character classes follow the Unicode tables of the Go
  release that builds the program, so a new Go release may normalize some
  strings differently. Normalize again before comparing with values stored
  by an older build, or compare on a stricter alphabet.
- `email` does not convert internationalized domains to ASCII (IDNA) and
  does not check that the address is valid.
- Do not normalize passwords with these operations; leave them untagged or
  use `precis=password` from `normtext`, the same way when a password is set
  and when it is checked.
- `case=` and the filters scan the whole string; limit input sizes before
  normalizing, as with any per-character processing.

## Modules

| Module                                                          | Contents                                                      | Dependencies       |
|-----------------------------------------------------------------|---------------------------------------------------------------|--------------------|
| [`github.com/go-deps/normalize`](https://pkg.go.dev/github.com/go-deps/normalize)                   | the core: tags, built-in operations, single values, plugins   | none               |
| [`github.com/go-deps/normalize/normtext`](normtext)             | Unicode forms, case folding, language-aware case, accents, width, PRECIS | `golang.org/x/text` |

Each module is versioned on its own; `normtext` releases are tagged
`normtext/vX.Y.Z`.

## Contributing and security

Issues and pull requests are welcome. Report vulnerabilities privately, as
described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
