# Changelog

All notable changes to this project are documented in this file. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `Struct` and `Normalizer.Struct`: in-place normalization of string fields
  driven by the `normalize` struct tag.
- Built-in operations `trim`, `lower`, `upper`, `collapse`; `dive` for slice
  and array elements; `-` to skip a field.
- `Normalizer.Register` for custom operations and `WithTagName` for a
  different tag key.
- `Unwrapper` for wrapper types such as optional values.
- `TagError` with `ErrUnknownOperation`, `ErrUnsupportedField` and
  `ErrMalformedTag`; malformed tags are reported for the whole type on the
  first call.
