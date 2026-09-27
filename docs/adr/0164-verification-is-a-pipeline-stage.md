# 0164. Verification is a pipeline stage

Status: Accepted
Date: 2026-09-27
Audience: compiler maintainers, agents driving the CLI

## Context

The AOT backend emits **textual** LLVM IR (no LLVM bindings). Until now the only thing
that checked that text was LLVM's verifier running *inside* `llc`, during a link step:

- `--emit-llvm` produced IR with **no verdict at all**; a caller had to pipe it into
  `llc` and scrape stderr.
- `Build` reported a bad module as `build: llc: exit status 1 <verbatim tool output>`,
  which mentions a temp-file path and gives no hint that the *compiler* — not the user's
  program — is at fault.
- `OptimizeIR` runs the real `opt` pipeline and, on any failure, silently returns the
  unoptimised module. "The optimiser rejected my IR" was invisible.
- The documented `--verify` flag runs the front end (lex/parse/analyse); the name
  suggested IR verification and did not deliver it.

Meanwhile the toolchain already pins one LLVM version (20), so a verifier invocation is
available and its output format is predictable.

## Decision

Add `pkg/lang/verify_llvm.go` and make verification an explicit stage:

```go
type IRVerification struct {
    OK       bool     // the verifier ran AND accepted the module
    Tool     string   // which binary decided
    Skipped  bool     // no toolchain: unverified, never "ok"
    Pipeline []string // ["verify"] or ["verify","-O2"], following --opt-level
    Errors   []string // verifier diagnostics, normalised
    Note     string   // matchable guidance
    Toolchain string  // "LLVM 20"
}
func VerifyModuleIR(ir string, optLevel int) (*IRVerification, error)
```

- Primary tool `opt-20 -passes=verify -disable-output` (+ `-O<n>` when `optLevel > 0`),
  fallback `llc-20 -filetype=null` when `opt` is unusable. Both are external tools
  already required by the pipeline; no new dependency.
- `BuildWithOptions` verifies the **optimised module it is about to link**, before `llc`,
  and stores the verdict in `BuildResult.Verification` (JSON: `verification`).
  `BuildOptions.NoVerify` (`--no-verify`) opts out.
- `gustyc --verify-llvm <src>` / `--verify-llvm-file <path>` expose the stage on its
  own: human line (`module verified by /usr/bin/opt-20 (verify)`) or `--json` emitting
  the `irVerification` record. Exit `1` on rejection, `0` with `skipped: true` when the
  toolchain is missing.
- Verifier diagnostics are normalised: the tool's own prefix and the per-run temp path are
  stripped to `prog.ll:line:col: error: …`, the echoed source line and caret are dropped,
  and the list is capped at 8 entries so a JSON consumer sees stable, small text.

## Rationale (agentic)

An agent consuming a compiler needs one question answered separately from all others:
*"is the IR you handed me valid?"* Before, answering it meant running `llc` and matching
free-form stderr containing a `/tmp/gusty-build-…` path that changes every run — not
something to branch on. `irVerification` is a stable, schema-documented record (`gustyc
--schema` → `irVerification`), and `skipped` distinguishes "checked and clean" from
"not checked", so a machine cannot mistake the absence of a toolchain for a pass. This is
the same duality as the rest of the CLI: a human line for the terminal, a JSON record for
scripts.

## Codegen / IR implications

- No IR is changed by this feature; it only *reads* the module. Verification runs after
  `OptimizeIR`, so the artifact that ships is the artifact that gets checked: IR produced
  by the real `opt` pipeline and IR from the textual fallback are both verified.
- Cost is one extra process per build (~tens of ms) — acceptable next to the `llc` + `cc`
  invocations already run. `--no-verify` exists for the fast path.
- Because verification reports *where* the module is broken, codegen fixes can be pinned
  with a unit test that asserts the rejection (`TestVerifyModuleIRRejectsBrokenModule`
  reproduces the exact `i32 @.str1` shape from Gap I.1).

## Alternatives rejected

- **Link-only verification (status quo)** — a codegen bug surfaces at link time with a
  temp path in the message; `--emit-llvm` users get nothing.
- **Verify inside `Compile` by default** — `Compile` is called by the REPL, `--eval`, the
  benchmark harness and hundreds of tests; doubling process spawns there for a check the
  caller may not want is the wrong default. `Compile` stays pure; `Build` (which links)
  verifies.
- **A Go-side IR validator** — would drift from LLVM's actual rules and pass modules
  `llc` rejects (we already saw `global variable reference must have pointer type`, an
  opaque-pointer rule only LLVM implements faithfully).
- **libLLVM via cgo (`LLVMVerifyModule`)** — links against a versioned libLLVM, breaks the
  "textual IR + external tools" toolchain rule and the pinned-version story.

## Consequences

- Every build now fails loudly on an invalid module, attributed to codegen
  (`LLVM rejected the module; this is a compiler bug, not a source error`).
- Enabling it inside `Build` immediately surfaced three container codegen bugs, fixed in
  Gap I.3 / ADR 0163 — evidence that unverified emission was hiding real defects.
- `--verify` (front end) and `--verify-llvm` (module) are now distinct, documented flags;
  `docs/operations.md` has the schema, the field table, and the exit-code behaviour.
- A machine without the pinned toolchain still builds, and the record says so.
