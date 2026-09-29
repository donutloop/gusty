# 0200. A method and a module function of one name are two definitions

## Status

Accepted. Implemented this cycle in `pkg/lang/semantic.go`; pinned by
`integration/programs/method_function_name_clash.gy`, `pkg/lang/method_function_key_test.go` and
`integration/method_function_name_test.go`. Roadmap Gap R.8 — the probe that pinned it was promoted
to the parity corpus once the debt was paid.

## Context

A program with classes uses the same words twice:

```gusty
def time(x):
    return x + 5


class Timer:
    def time(self, x):
        return x * 3

    def run(self, x):
        return self.time(x) + 1


print(time(1))          # 6 everywhere except the checker
print(Timer().run(2))   # 7
```

`--check` answered `error at 7:16: undefined name "x"` — pointing at the *method's own parameter*
in `return x * 3`. `--interp` printed `6` and `7`; CPython printed `6` and `7`; and the module the
codegen emitted for the same source linked and ran, printing the same. Only the front end thought
the program was broken, and because the build gate trusts the front end (ADR 0177), the program
could not be compiled.

The mechanism took a while to pin down, because the obvious hypotheses were wrong and each was
falsified by a smaller program:

- it is **not** a method-call checking problem — `self.time(x)` is a dynamic attribute call and is
  not argument-checked at all;
- it is **not** the name `time` being special — the same program with `tick`, `zork`, or `f` behaved;
- it is **not** the class alone, nor the module function alone: removing either half made the
  program check clean.

What remained after the bisect: the checker kept one function table keyed by the bare name. Methods
were registered in it (`an.funcs[fd.Name] = fd` while analyzing a class body), so the method
analyzed later **replaced** the module function. The call `time(1)` then resolved — through
`userFunc`, the same table — to the *method*, whose parameters are `(self, x)`. Binding one call
argument to two parameters left the second unbound, and re-walking the method body during
per-call-site return inference reported its `x` as an undefined name. The error pointed at the
callee, never at the call, which is why it looked like a scoping bug rather than a resolution bug.

## Decision

**A method is registered under its class, never under its bare name.**

1. `SemanticAnalyzer.methods`, keyed `Class.method`, alongside `funcs`, keyed by bare name.
   `analyzeFunc` consults `inClass`/`curClass` (set by the `ClassDef` branch of `analyzeStmt`,
   saved and restored around the class body) and writes to one table or the other. A call resolved
   by bare name can therefore only ever reach a module function or a nested def — never a method —
   so the arity it is checked against is the arity the call site actually has.

2. **Restoring the enclosing class on the way out is part of the fix, not hygiene.** A class nested
   inside a method body, or a def inside a class inside a def, must register in the table that
   matches *its own* enclosing scope; without the save/restore the class name would leak into the
   outer function's defs and the collision would return in a shape no test covers.

3. **Methods are still analyzed, and still reported on.** The fix moves a registration, not a walk:
   an error inside a method body (`return nope`) is verified to still be reported by
   `TestMethodBodyErrorsAreStillReported`.

4. **Which definition a call means is observable, and is asserted.** Because methods were never
   argument-checked through this table, "the collision is gone" could be satisfied by accidentally
   checking nothing. The tests therefore assert the *parameter named in the diagnostic*: with
   `def time(x: int)` and `Timer.time(self, label: str)`, a bad argument must be reported as
   `argument "x": expected int, got str`. If the method's definition were used, the name in the
   message would be `label`.

## Consequences

- `programs/method_function_name_clash.gy` — the program that was pinned as a probe — is now in the
  parity corpus with `oracle: "match"`: `6 7 6 12` on the interpreter, in the compiled binary, and
  in CPython. Its ledger row was deleted by the harness's own instruction:
  `TestOracleProbeRowsAreRecordedAsDebt` fails a probe whose debt has been paid, which is the
  mechanism that turns "known divergence" back into "regression coverage".
- Names a program may choose freely no longer include an invisible tax: `area`, `run`, `send`,
  `open`, `time` — the words that describe a class's behaviour are exactly the words that describe
  a helper's, so this collision was not a corner case.
- Measured while writing the arity assertion, and opened as **R.10**: calling a function with too
  few arguments is not reported at all, annotated or not (`def build(a, b)` called as `build(1)`
  passes the checker on the base compiler and after this change). It is the reason an arity test had
  to be phrased as "which parameter does the diagnostic name".
- Nothing in codegen is involved: the emitted module always resolved the two definitions correctly
  (methods by `Class_method`, functions by `gy_<name>`, ADR 0198). The defect lived entirely in the
  front end, which is the same place Gap R.5 lived.

## Alternatives considered

- **Qualify the key only when a collision occurs.** Rejected: collision-conditional keying makes
  the checker's behaviour depend on declaration order and file layout, which is how R.5 got in.
- **Skip registering methods in any table.** Rejected: methods *are* definitions — they need a
  definition site for their own bodies to be analyzed against, and a place for the (future)
  attribute-call checking to resolve `self.m(...)`; and skipping the registration would also erase
  the errors inside them, which is what the "still analyzed" test exists to prevent.
- **Fix it in the call path instead (special-case `userFunc` when the found def has a `self`).**
  Rejected as a patch over the symptom: the wrong thing had been put in the table in the first
  place, and the same table feeds every other name-based decision in the checker.
