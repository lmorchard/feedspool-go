# feedspool-go Development Notes

## Build System

**IMPORTANT**: Always use `make build` instead of `go build`. 

- `make build` - Builds the binary with proper build flags and versioning and with the name `feedspool`
- `go build` - Should be avoided as it doesn't include proper build metadata and produces an executable incorrectly named `feedspool-go`

The Makefile handles build flags, version injection, and other build-time configuration that `go build` alone does not provide.

## Linting and Testing

After every set of major changes, YOU MUST run `make format` and `make lint` for basic source code linting and then `make test` to ensure tests pass.

`make lint` installs and runs the golangci-lint version pinned as `GOLANGCI_LINT_VERSION` in the Makefile, into a gitignored `bin/`. CI reads that same pin via `make print-golangci-lint-version`, so local and CI runs agree on what counts as a finding. Do not `go install ...golangci-lint@latest` to work around a lint problem — a different version reports a different set of findings (2.12.2 flags 8 `goconst` issues that 2.13.1 does not). Change the pin in the Makefile instead, and expect CI to move with it.

You should also endeavor to keep the test suite up to date - our goal is not 100% coverage, but significant new logic and changes should be covered.

We don't really bother testing code in cmd/ but all internal/ modules should be tested.
