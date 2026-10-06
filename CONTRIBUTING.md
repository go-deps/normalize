# Contributing

Thank you for helping. Bug reports, new operations, documentation fixes and
benchmarks are all welcome.

## Before you start

- **Bugs.** Open an issue with a minimal reproduction: the struct, the tag,
  the input and the output you expected.
- **New operations or API changes.** Open an issue first and describe the
  use case and the tag syntax you have in mind, so that the design is agreed
  before you write code.
- **Vulnerabilities.** Do not open a public issue; follow
  [SECURITY.md](SECURITY.md).

## Setup

The repository holds two modules: the core (`.`) and `normtext`. Tie them
together with a workspace file; `go.work` is ignored by git.

```sh
git clone https://github.com/go-deps/normalize
cd normalize
go work init . ./normtext
```

Go 1.26 or later is required.

## Checks

A pull request has to pass the same checks as CI:

```sh
go vet ./... ./normtext/...
go test -race -count=1 ./... ./normtext/...
go test -count=1 -run Allocate .        # allocation guard, without -race
(golangci-lint run) && (cd normtext && golangci-lint run)
go run golang.org/x/vuln/cmd/govulncheck@latest ./... ./normtext/...
```

Fuzz the code you touched for a short while:

```sh
go test -run='^$' -fuzz='^FuzzOperations$' -fuzztime=30s .
go test -run='^$' -fuzz='^FuzzStruct$' -fuzztime=30s .
go test -run='^$' -fuzz='^FuzzTag$' -fuzztime=30s .
(cd normtext && go test -run='^$' -fuzz='^FuzzOperations$' -fuzztime=30s .)
```

## Guidelines

- **The core stays dependency-free.** It imports the standard library only.
  An operation that needs another module belongs in a plugin module, such
  as `normtext`.
- **Clean input must not allocate.** The allocation guard tests fail if an
  operation allocates on input it does not change; run the benchmarks before
  and after a change to a hot path:
  `go test -run='^$' -bench=. -benchmem -count=10 .`
- **Every behaviour is tested.** Add table cases for new operations and,
  where an operation has a simple property (idempotence, a fixed alphabet),
  an oracle in `fuzz_test.go`.
- **Document what users see.** A new operation or option is listed in
  `README.md` and `doc.go`, and the change is added to `CHANGELOG.md` under
  `Unreleased`.
- **One change per pull request**, with a commit message in the
  [Conventional Commits](https://www.conventionalcommits.org/) form, for
  example `feat: add the squeeze operation` or `fix(normtext): keep marks
  after nfc`.

## Code of conduct

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).
