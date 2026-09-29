# The Go floor is a declared dependency: 1.22, in go.mod and in CI, checked

## Context

CI failed to build at all:

```
go: downloading golang.org/x/text v0.3.0
# github.com/donutloop/gusty/pkg/lang
pkg/lang/oracle.go:94:44: undefined: strings.ContainsFunc
```

That call — `strings.ContainsFunc` — is a **Go 1.21** stdlib addition. It came from the oracle
version parser I wrote in the previous cycle (ADR 0193). It compiled, tested, and passed here on Go
1.22.2, and failed only in CI, which builds against `go-version: '1.20'` matching `go.mod`'s
`go 1.20`.

The mechanism is worth stating precisely, because it surprises people: **the `go` directive gates
language features, not stdlib API availability.** A 1.21-only function type-checks happily on a
1.22 toolchain no matter what `go.mod` says; the directive only changes parsing/semantics and some
vet behaviour. So the project's declared floor did not actually constrain the standard library I
could call, and CI — not the declaration, not a linter, not the local build — was the only thing
that noticed.

This is the same failure mode as ADR 0193 (a stale CPython presenting as oracle drift on a compiler
row), one toolchain over: an external toolchain's version was load-bearing, informally. There the
expectation came from CPython; here the build came from a Go older than the one everything was
developed on.

## Decision

**1. Raise the floor to Go 1.22, and keep the two places that state it equal**: `go.mod`'s `go`
directive and the CI workflow's `go-version` (both were 1.20; CI ran 1.20 while development ran
1.22.2). The floor now matches the toolchain the project is actually written against, so a
newer-than-floor stdlib call cannot be simultaneously "legal locally" and "illegal in CI".

**2. Checked, not recorded.** The CI toolchain step prints `go` / `llc-20` / `python3` versions and
now fails if Go is below the floor:

```
go version | grep -Eq 'go1\.(2[2-9]|[3-9][0-9])' || { echo "Go is older than the declared 1.22 floor in go.mod"; exit 1; }
```

ADR 0193's rule generalises: a version that is only printed is documentation; a version that gates
something is a contract. Both the oracle (≥ 3.12) and now the compiler floor are enforced in the
same step, with the same shape of failure.

**3. The loop-variable semantics change was verified, not waved through.** Raising the `go`
directive is not cosmetic: at `go 1.22` the per-iteration loop-variable semantics become active, and
every local green run up to now had been compiled under 1.20 language rules by a 1.22 compiler. So
the meaningful test was not `go build` — it was the whole suite with the new rules in effect: full
`go test -tags=llvm20 -count=1 ./...` across `pkg/`, `integration/`, `cmd/` (66-row conformance
matrix included), green. A grep for the pattern that actually changes behaviour — closures or
`defer` capturing a loop variable — found none in the compiler packages, which is consistent with
that, though the suite rather than the grep is the evidence.

**4. The offending call stays version-portable.** I left `strings.IndexFunc(f, unicode.IsDigit) >= 0`
rather than restoring `strings.ContainsFunc`. It predates every plausible floor and the comment at
the call site says why the difference exists, so the file no longer carries a version landmine in a
place that reads as ordinary string work.

**5. Reproduce the floor locally instead of trusting the report.** I installed the real 1.20 SDK
(`go install golang.org/dl/go1.20@latest && go1.20 download`) and ran `go1.20 build/vet ./...` over
every package, including the test files CI had not reached — because a single reported error is not
evidence of a single violation, and the alternative is discovering them one CI run at a time. The
floor has moved since, but the habit is the point: **when CI's toolchain differs from yours, take CI's
toolchain and run it locally before pushing.**

## Alternatives rejected

- **Keep the floor at 1.20 and hand-port every new-API call.** Defensible for a library with a wide
  consumer base; this is a compiler toolchain whose only supported build path is its own CI, and the
  floor was 1.20 by inheritance, not by decision. Paying a permanent tax of API-memory for an
  undeclared compatibility promise is the wrong trade.
- **Raise CI's `go-version` and leave `go.mod` at 1.20.** The worst of both: the mismatch would
  remain, just inverted — CI permissive, declaration stale, and anyone reading `go.mod` to learn the
  project's requirements told something false.
- **Add a lint rule banning newer stdlib APIs.** Real tools exist (`apiusage`-style checks,
  `golangci-lint` with a Go-version build tag), and they'd be reasonable if the project genuinely
  needed a low floor. Choosing instead to make floor == dev toolchain removes the class of problem
  rather than policing it, with no new configuration to maintain or bypass.
- **Pin to exactly `1.22.2`, the version on my laptop.** Over-precision that manufactures friction:
  patch releases are not what the code depends on, and the guard accepts any ≥ 1.22.

## Consequences

- `go.mod` and `.github/workflows/go.yml` must move together; the version step fails if the CI
  toolchain drifts below the floor, and ADR 0193's oracle pin sits beside it in the same step.
- The floor is a *declared* dependency like LLVM 20 and CPython ≥ 3.12: recorded in the toolchain
  step's output and enforced there. `docs/operations.md` states all three together.
- Process rule this cycle added, and it generalises beyond Go: **a discrepancy between CI's
  toolchain and the local one is not a thing to reason about, it is a thing to eliminate** — either
  by running CI's toolchain locally (free, with the `golang.org/dl/…` SDKs) or by removing the
  difference outright, as here.
