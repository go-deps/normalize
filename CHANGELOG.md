# Changelog

All notable changes to this project are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The repository holds two modules, versioned separately: the core
`github.com/go-deps/normalize`, tagged `vX.Y.Z`, and
`github.com/go-deps/normalize/normtext`, tagged `normtext/vX.Y.Z`. A
`normtext` release always requires a released core version, so the core is
tagged first.

## [Unreleased]

### Added

**`github.com/go-deps/normalize/normtext` v0.1.0**

- `Plugin` adding `nfc`, `nfd`, `nfkc`, `nfkd`, `fold`, `unaccent`,
  `width=narrow|wide|fold`, `title`, `title=<lang>`, `lower=<lang>`,
  `upper=<lang>` and `precis=username|username-preserved|nickname|password`,
  built on `golang.org/x/text`.

## [0.1.0] - 2026-10-06

### Added

**`github.com/go-deps/normalize` v0.1.0**

- `Struct` and `Normalizer.Struct`: in-place normalization of strings in
  structs, driven by the `normalize` tag or its short form `norm`
  (`DefaultTagName`); `WithTagName` sets other keys, such as `n`.
- Tag syntax: operations with arguments (`truncate=64`), single-quoted
  arguments (`trim=',;'`), and the directives `dive` for slices, arrays and
  maps at any depth, `keys(...)` for map keys, and `-` to skip a field.
- Built-in operations: `trim`, `trimleft`, `trimright`, `lower`, `upper`,
  `collapse`, `newline`, `capfirst`, `slug`, `email`, `digits`, `letters`,
  `alnum`, `nocontrol`; with an argument: `trim=`, `trimleft=`, `trimright=`,
  `cutprefix=`, `cutsuffix=`, `keep=` and `remove=` with character classes,
  `collapse=inline`, `case=` with seven identifier styles, `truncate=`.
- Directives `default=` for strings, bools, numbers and nil pointers, and
  `nilempty` for pointers, slices and maps.
- `Var`, `Apply`, `ApplyWith` and the compiled `Rule` (`Compile`,
  `MustCompile`, `CompileWith`, `MustCompileWith`) for single values.
- `Register`, `RegisterCheck` and `RegisterParam` for custom operations,
  including operations that reject input and operations with arguments.
- `Plugin`, `Registry`, `Use` and `WithPlugins` for operation sets kept in
  separate modules. A plugin may replace a built-in operation; `ErrConflict`
  when it adds an operation registered or added by another plugin.
- `Unwrapper` for wrapper types such as optional values, and
  `AfterNormalizer` for rules that span several fields.
- `TagError` wrapping `ErrUnknownOperation`, `ErrUnsupportedField` or
  `ErrMalformedTag`, reported for the whole type on the first call;
  `FieldError` with the path of the value, joined with `errors.Join`;
  `ErrKeyCollision`, `ErrInvalidTarget`, `ErrInvalidArgument`;
  `WithFieldName` to name path segments after JSON names.
- Limits on nesting depth and on the number of reported errors, set with
  `WithMaxDepth` and `WithMaxErrors` and reported with `ErrTooDeep` and
  `ErrTooManyErrors`.

[Unreleased]: https://github.com/go-deps/normalize/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/go-deps/normalize/releases/tag/v0.1.0
