# normtext

[![Go Reference](https://pkg.go.dev/badge/github.com/go-deps/normalize/normtext.svg)](https://pkg.go.dev/github.com/go-deps/normalize/normtext)

Unicode text operations for [`github.com/go-deps/normalize`](https://github.com/go-deps/normalize):
normalization forms, case folding, language-aware casing, accent removal,
width mapping and the PRECIS profiles for usernames, nicknames and
passwords. It is a separate module because it depends on
[`golang.org/x/text`](https://pkg.go.dev/golang.org/x/text); the core module
has no dependencies.

## Install

```sh
go get github.com/go-deps/normalize/normtext
```

Requires Go 1.26 or later.

## Usage

Add the plugin to the package-level Normalizer, or to one of your own:

```go
import (
	"github.com/go-deps/normalize"
	"github.com/go-deps/normalize/normtext"
)

func init() {
	if err := normalize.Use(normtext.Plugin()); err != nil {
		panic(err)
	}
}

// or
var n = normalize.New(normalize.WithPlugins(normtext.Plugin()))
```

Then use the operations in tags, together with the built-in ones:

```go
type Account struct {
	Username string `normalize:"trim,precis=username"` // " Ａlice " → "alice"
	Name     string `normalize:"trim,nfc,collapse"`    // "Rene\u0301  Magritte" → "René Magritte"
	City     string `normalize:"trim,unaccent,fold"`   // " São Paulo " → "sao paulo"
}

city, err := normalize.ApplyWith(n, "İZMİR", "lower=tr,title=tr") // "İzmir"
```

## Operations

| Operation          | Effect                                                         | Example                                  |
|--------------------|----------------------------------------------------------------|------------------------------------------|
| `nfc`              | canonical composition                                          | `"e\u0301"` → `"é"`                      |
| `nfd`              | canonical decomposition                                        | `"é"` → `"e\u0301"`                      |
| `nfkc`             | compatibility composition                                      | `"ﬁ①"` → `"fi1"`                         |
| `nfkd`             | compatibility decomposition                                    | `"éﬁ"` → `"e\u0301fi"`                   |
| `fold`             | full Unicode case folding, for caseless matching               | `"Straße"` → `"strasse"`                 |
| `unaccent`         | remove non-spacing marks                                       | `"Crème brûlée"` → `"Creme brulee"`      |
| `width=narrow`     | map wide (full-width) characters to narrow                     | `"ＡＢＣ１２３"` → `"ABC123"`             |
| `width=wide`       | map narrow characters to wide                                  | `"ABC"` → `"ＡＢＣ"`                      |
| `width=fold`       | map to the canonical width for East Asian text                 | `"ＡＢＣｶ"` → `"ABCカ"`                  |
| `title`            | title case with language-neutral rules                         | `"hello wORLD"` → `"Hello World"`        |
| `title=<lang>`     | title case with the rules of a language                        | `title=nl`: `"ijsselmeer"` → `"IJsselmeer"` |
| `lower=<lang>`     | lower case with the rules of a language                        | `lower=tr`: `"İSTANBUL"` → `"istanbul"`  |
| `upper=<lang>`     | upper case with the rules of a language                        | `upper=el`: `"άδεια"` → `"ΑΔΕΙΑ"`        |
| `precis=<profile>` | enforce a PRECIS profile, rejecting what it does not allow     | `precis=username`: `"Ａlice"` → `"alice"` |

Languages are [BCP 47](https://www.rfc-editor.org/info/bcp47) tags such as
`tr`, `nl` or `pt-BR`. The plain `lower` and `upper` of the core module stay
available and are language-independent.

PRECIS profiles:

| Profile              | Standard                                                    | Use for                         |
|----------------------|-------------------------------------------------------------|---------------------------------|
| `username`           | [RFC 8265](https://www.rfc-editor.org/rfc/rfc8265) UsernameCaseMapped    | case-insensitive usernames      |
| `username-preserved` | RFC 8265 UsernameCasePreserved                              | case-sensitive usernames        |
| `nickname`           | [RFC 8266](https://www.rfc-editor.org/rfc/rfc8266)          | display names                   |
| `password`           | RFC 8265 OpaqueString                                       | passwords, before hashing       |

`precis` is the only operation that rejects input: a string the profile does
not allow, such as a username with a space, is reported in a
`*normalize.FieldError` and the field is left unchanged. An empty string is
left empty rather than rejected, so that optional fields stay optional;
requiring a value is the job of validation.

Every operation is idempotent and safe for concurrent use. A malformed
argument, such as an unknown width, language or profile, is a
`*normalize.TagError` reported when the tag is first read.

## Security notes

- `precis=password` maps non-ASCII spaces to U+0020 and applies NFC but
  keeps case. Apply it the same way when a password is set and when it is
  checked, and store only a hash of the result.
- The username profiles restrict and normalize characters but do not detect
  characters that look alike across scripts (Unicode TS #39); check
  confusables separately where impersonation matters.
- `precis=nickname` keeps case, so two nicknames that differ only in case
  are different values; fold them (`precis=nickname,fold`) before checking
  uniqueness.
- `nfkc`, `nfkd` and `precis=nickname` can make a string much longer: a
  single character may expand to 18 characters and 11 times its size in
  bytes, as U+FDFA does. Limit
  input sizes before normalizing and lengths after it.
- Results follow the Unicode version of `golang.org/x/text`, so upgrading it
  may normalize some strings differently. Normalize again before comparing
  with values stored by an older build.
- Language-aware casing (`title`, `lower=`, `upper=`) is slower than the
  core's `lower` and `upper`; limit input sizes and use it only where the
  language matters.

## Versioning

This module is versioned separately from the core and tagged
`normtext/vX.Y.Z`. Each release requires a released version of
`github.com/go-deps/normalize`.

## License

[MIT](LICENSE)
