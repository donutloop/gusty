# 0201. A call must supply every parameter that has no default

## Status

Accepted. Implemented this cycle in `pkg/lang/semantic.go` (`bindParams`, `inferUserCall`); pinned
by `pkg/lang/arity_test.go`, `integration/arity_test.go` and
`integration/programs/arity_defaults.gy`. Roadmap Gap R.10.

## Context

The checker enforced half of the calling contract. Too **many** arguments was refused; too **few**
was not:

```gusty
def build(a, b):
    return a


print(build(1))     # --check: ok
```

Nothing complained, and the damage surfaced somewhere else entirely. With `b` never bound, the
per-call-site walk of the callee's body reported `undefined name "b"` at the *callee's* line —
before this change, one dropped argument arrived as three diagnostics, two of them pointing at
source the programmer had written correctly:

```
warning at 2:14: arithmetic on non-numeric operands (int, any)
error   at 2:16: undefined name "b"
error   at 2:16: undefined name "b"
```

…and not even consistently, because that walk only runs when the callee has annotations. The same
program on the interpreter failed at run time with a message the checker had never offered:
`missing argument b`. So the language had a correct answer sitting in one backend while the front
end, which everything else consults first, said `ok`.

This is the failure mode of an enormous class of ordinary mistakes: a renamed parameter, a dropped
argument, a call written against yesterday's signature. It is also the class an agent driving the
compiler cannot recover from on its own, because the diagnostic it receives blames the wrong file
position.

## Decision

**Parameter binding now reports what it left unfilled, and a malformed call is not analysed
further.**

1. `bindParams` already knew the answer: it fills `provided`/`exprs` by index, so any index with no
   entry and no `Default` is a parameter that received nothing. Two messages, because two different
   mistakes were made:
   - positional shortfall — `function "build" expects 2 arguments, got 1` — the caller lost count;
   - a keyword call that skipped a name — `function "build" is missing argument "b"` — the caller
     named parameters and left one out, so naming the parameter is the useful fact, not the count.
   A default *is* a way of being supplied: a call that passes fewer arguments than the definition
   lists is precisely what a default exists for, and the whole legitimate shape space (trailing
   defaults, all defaults, keyword-only, keyword-plus-default, zero-parameter) is pinned as the
   over-refusal guard.

2. **The arity message names the callee.** `too many arguments` said what was wrong and not of
   which of several calls in the same file; it is now `function "build" accepts 2 arguments, got
   more`, and attribute calls say `method "m" ...`. Position plus callee name is what makes a
   diagnostic actionable.

3. **Once a call does not fit its definition, the callee's body is not walked.** `inferUserCall`
   returns dynamic on an arity failure instead of continuing into `inferReturn`. This is not
   cosmetics: re-inferring a return type from a body whose parameters are half-bound manufactures
   follow-on errors that are *false* — the callee is fine, the call is not. One mistake, one
   diagnostic, at the call.

4. **Methods are measured without `self`.** Method calls resolve through attribute access and never
   reached this path, which the tests pin rather than assume (`one-arg method`, `no-arg method`,
   `method with a default`, `sibling call through self`), because counting `self` as a caller's
   argument is exactly the confusion that produced Gap R.8.

5. **Built-ins keep their own arity rules.** `print` with separators, `range` with one to three
   arguments, `len` — verified to stay clean, since the new rule lives in the user-definition path
   and must not become a second opinion on built-ins (Gap R.6 is the cautionary tale there).

## Consequences

- The R.10 repro now produces exactly one error, at the call, naming the function:
  `error at 5:12: function "build" expects 2 arguments, got 1`.
- The interpreter's runtime message (`missing argument b`) and the checker's static one agree, and
  the CLI test pins both — the interpreter's verdict is not treated as a substitute for the
  compiler's, and the compiler's is now consistent with it.
- Measured with the whole corpus: no existing program was newly refused. Every legitimate short call
  in the corpus is a defaulted parameter, which is the evidence that the rule is aimed at mistakes
  rather than at style.
- `programs/arity_defaults.gy` joined the ledger as a three-engine parity program (`11 6 8 13 6 33
  7 31 11 14 42`, `oracle: "match"`) — the positive side of a new refusal rule is a program that
  uses all the legitimate shapes and must keep running.
- Measured while writing the tests, and recorded as **R.11**: `def f(a, b=1, c)` — a non-default
  parameter after a defaulted one — parses and is only caught at the call, whereas CPython rejects
  the definition. Such a function cannot be called correctly at all, so the definition site is the
  honest place to say so.
- Related but distinct: R.7 (one line, the same diagnostic two or three times). This cycle removes
  one *source* of that duplication (walking a callee a call doesn't fit); the remaining source is
  `inferReturn` re-walking a callee once per call site, which is R.7's own work.

## Alternatives considered

- **Report missing arguments at the definition site** (a function whose parameters cannot all be
  filled). Rejected: parameters are individually fine; it is the call that is wrong, and the
  definition site is where the *uncallable-by-construction* case (R.11) belongs, not this one.
- **Leave the derived `undefined name` errors as they were.** Rejected: they are not merely noisy —
  they are *wrong*, they point at correct source, and they were the only signal a dropped argument
  ever produced for unannotated functions, which is how a missing-argument bug could masquerade as a
  scoping bug for a whole debugging session (see ADR 0200, which began exactly that way).
- **Fill unbound parameters with the dynamic type instead of reporting.** Rejected: it would make
  the program compile, which is the opposite of the goal — the program is broken, and a backend
  difference (interpreter runs, codegen refuses) is a symptom to fix, not a feature to preserve.
- **Warn rather than error.** Rejected: the interpreter already refuses at run time, and ADR 0164's
  rule is that errors mean "some path cannot execute this program at all" — which is exactly what
  happens here.
