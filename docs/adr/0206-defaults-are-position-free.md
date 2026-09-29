# 0206. A default marks a parameter as optional, not as last

## Status

Accepted. No compiler change — this ADR decides what the existing behaviour *is*, and the tests, the
document and the ledger are what make it a rule rather than an accident. Pinned by
`pkg/lang/param_order_test.go`, `integration/param_order_test.go` and
`integration/programs/param_default_order.gy`. Roadmap Gap R.11.

## Context

```gusty
def offset(base, step=10, bonus):
    return base + step + bonus
```

CPython refuses this at the `def`:

```
SyntaxError: parameter without a default follows parameter with a default
```

The roadmap recorded it as a gap on the assumption that the refusal is the correct answer and we were
missing it: "such a function can never be called correctly, so the definition site is the honest place
to say so." That reasoning is imported from Python, and it is wrong here — so the first thing this
cycle did was measure it instead of implementing the imported fix.

Measured: `--check` says `ok`; `offset(1, 2, 3)` prints `6` on the interpreter and on the compiled
binary; `offset(1, bonus=5)` prints `16`; `offset(base=1, step=2, bonus=3)` prints `6`. CPython, again,
cannot get as far as running it.

Why the difference is legitimate rather than a hole: Python's restriction exists because Python's
positional binding stops at the first defaulted parameter, so `f(1, 2, 3)` would leave `c` unreachable
and the author would get a run-time `TypeError` for a call that *looks* right. This language's
`bindParams` fills positionally left to right regardless of defaults, and a keyword argument names the
parameter it fills, so there is no unreachable parameter and no surprising failure. The shape Python
forbids is not the shape this language would mishandle: what this language mishandles — a call that
leaves a no-default parameter unfilled — is already refused at the call, by name (ADR 0201).

## Decision

**A default marks a parameter that may be omitted; it says nothing about the parameters around it.**

1. `def f(a, b=1, c)` is accepted, and so is every ordering of defaults: first, middle, last, all,
   none — including methods, whose `self` is not part of the caller's binding (`class C: def m(self, a,
   b=1, c)` is checked and runs).
2. **What is enforced is fillability, at the call**: positional binding fills left to right; a keyword
   argument names what it fills; and a call that leaves a parameter with no default unfilled is refused
   with either `function "f" expects 3 arguments, got 1` or `is missing argument "c"` (ADR 0201). Those
   two messages are the whole contract, and they are asserted against this shape rather than only
   against the ordered one.
3. **The divergence is declared, not hidden.** `programs/param_default_order.gy` joins the conformance
   ledger in the *gusty-only surface* section with `oracle: not-applicable` — the same treatment as
   positional set subscript, module-scope `await`, and the stdlib name space. `TestDefaultedParameterInAnyPositionAgreesOnEveryPath`
   asserts both engines produce `6 16 6 123 923 129 9`, and asserts that CPython still cannot run it:
   the row says the oracle is excluded, and the test keeps the row honest.
4. **The roadmap entry is closed by documentation, not by a refusal** — the honest outcome when the
   measured behaviour is right and the *specification* was the thing that was missing. Where a gap turns
   out to be a missing document rather than a missing check, the fix is allowed to be a document plus
   tests, provided the document is normative and the tests fail if the behaviour moves.

## Consequences

- `docs/language.md` gains the rule under "A call must fill every parameter without a default", with
  the CPython message quoted so nobody re-imports the restriction by reflex, and a pointer to the
  ledger row that records the divergence.
- `docs/operations.md` tells a code generator (the primary consumer here) not to sort parameters to
  satisfy a Python rule this language does not have: it must give every parameter a way to be filled,
  and the checker enforces exactly that.
- A generated-signature agent that *does* emit defaults last remains perfectly valid; this is a
  permission, not a preference.
- The ledger gains its 77th row with the oracle excluded, and the drift test remains the guarantee that
  "excluded" means what it says: if CPython ever runs the program, the harness complains.

## Alternatives considered

- **Refuse the definition, matching CPython's message.** Rejected on measurement: it would reject
  programs whose calls all bind correctly, and it would do so in the name of a failure mode
  (`TypeError` at run time for an unreachable parameter) that cannot occur under this language's binding
  rules. A refusal that fires on working programs is a defect with better marketing.
- **Refuse only when the parameter is genuinely unreachable** — i.e. implement the restriction
  conditionally on positional binding. Rejected as needless: with left-to-right positional filling plus
  keyword filling, there is no such case; the condition is `false` by construction, so the rule would be
  dead code with a test suite.
- **Leave it undocumented and untested.** Rejected: this is how a correct behaviour becomes a
  regression. The gap's title reads like a bug; the next agent through will reach for the CPython-shaped
  fix unless the language document says, in those words, why it is not a bug — and the tests are what
  make the document load-bearing.
