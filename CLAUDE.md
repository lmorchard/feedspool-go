# feedspool-go Development Notes

## Build System

**IMPORTANT**: Always use `make build` instead of `go build`. 

- `make build` - Builds the binary with proper build flags and versioning and with the name `feedspool`
- `go build` - Should be avoided as it doesn't include proper build metadata and produces an executable incorrectly named `feedspool-go`

The Makefile handles build flags, version injection, and other build-time configuration that `go build` alone does not provide.

Builds are cgo-free: the SQLite driver is `modernc.org/sqlite` (pure Go), and the Makefile sets `CGO_ENABLED=0`. Keep it that way — it is what makes the binaries static, lets every release target cross-compile without a C toolchain, and keeps the darwin builds ad-hoc signed by Go's internal linker. Do not reintroduce `github.com/mattn/go-sqlite3` or a `-linkmode external` build. Note the race detector does need cgo, so `go test -race` runs outside the Makefile (as CI does it).

## Linting and Testing

After every set of major changes, YOU MUST run `make format` and `make lint` for basic source code linting and then `make test` to ensure tests pass.

`make lint` installs and runs the golangci-lint version pinned as `GOLANGCI_LINT_VERSION` in the Makefile, into a gitignored `bin/`. CI reads that same pin via `make print-golangci-lint-version`, so local and CI runs agree on what counts as a finding. Do not `go install ...golangci-lint@latest` to work around a lint problem — a different version reports a different set of findings (2.12.2 flags 8 `goconst` issues that 2.13.1 does not). Change the pin in the Makefile instead, and expect CI to move with it.

**There are two pins, and they move together.** The Go toolchain is pinned by the `toolchain` directive in `go.mod`; the Makefile exports `GOTOOLCHAIN` from it, and `actions/setup-go` reads it in preference to the `go` directive. So `make build`, `make test`, `make lint`, CI and the release workflows all run the same Go, rather than whatever is on `PATH`.

That pin is not cosmetic. golangci-lint carries a type checker that only understands the Go releases it was built against, and its own `go.mod` sets a floor. For the current v2.13.1 the usable window is exactly Go 1.26.x: 1.25.x cannot build it (`requires go >= 1.26.0`), and against a 1.27 stdlib it dies inside `crypto/internal/randutil` with `method must have no type parameters` before reading a line of this repo. That is how `make lint` came to fail locally while CI passed on the same commit (#67).

So raising the Go version means bumping `toolchain` *and* `GOLANGCI_LINT_VERSION` to a release that handles it — and checking the new linter's floor, not just its ceiling. `make lint` prints both versions it used, and `make check-toolchain` fails with one readable line if the pin has gone missing.

You should also endeavor to keep the test suite up to date - our goal is not 100% coverage, but significant new logic and changes should be covered.

We don't really bother testing code in cmd/ but all internal/ modules should be tested.
