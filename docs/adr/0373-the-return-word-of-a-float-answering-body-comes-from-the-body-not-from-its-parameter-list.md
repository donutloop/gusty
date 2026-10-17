# 0373 — The word a float-returning body returns comes from the body, not from its parameter list

## Context

ADR 0254 fixed a wrong answer at exit 0 by refusing to read a function's return word off the shape of
its `return` expression. ADR 0196 had given a rebound parameter a slot; what was missing was asking the
*body* — not its last line — which of its names ended up holding a double. The gate it installed lives
in `pkg/lang/params.go` as `returnedFloatRebindings`, and it opened with two guards:

```go
if fd == nil || len(fd.Params) == 0 {
    return nil
}
```

The second one answered the question before asking it. The scan is about **the name the return reads
back**; where that name came from — a parameter, or a binding the body wrote itself — is not the
question. Three shapes, all answered by the reference, all printed a truncated word at the exit code of
success on this backend:

| source | CPython | this backend, before this ADR |
|---|---|---|
| `def f():` / `u = 9.5` / `return u` / `print(f())` | `9.5` | `0` |
| `def f():` / `v = 2.5 * 2` / `return v` / `print(f())` | `5.0` | `0` |
| `def f():` / `u = 2.5` / `v = u * 2` / `return v` / `print(f())` | `5.0` | `0` |

Each emitted `define i32 @gy_f()` over a body that had just written a `double` into `%_u` / `%_v`; the
read answered the low word of that double, and a `double` 9.5's low word is `0`. Nothing about the
programs is unusual — `def f(): u = 9.5; return u` is the first float function a beginner writes — and
the neighbouring shape **with** a parameter (`def f(x): u = 2.5; return u * x`) has answered correctly
since ADR 0254, which is what makes the parameterless form a hole in the gate rather than a boundary of
the language.

The third shape is a second, separate defect hiding behind the first. Even with the guard removed,
`v = u * 2` was not recognised as a double, because the scan asked `isFloat` of each binding **once**,
over a map, with no notion of which answers depended on which: `v` is a double *because* `u` is one,
and a single pass over an unordered map says no to `v` whenever it reaches `v` first. That shape
printed `0` for a body with parameters too — `def f(x):` / `u = 2.5` / `v = u * 2` / `return v` — so it
was never only the no-parameter case.

## Decision

**The return word is a fact about the body's names, and the body's float table is settled by
repetition. Neither half reads the parameter list.**

1. **`returnedFloatRebindings` keeps only the `fd == nil` guard.** A body with no parameters has no
   obstacle to a `double` return, so there is nothing for it to be refused for; the gate that decides
   whether a promotion can be *carried* is `doubleReturnCarriable`, which asks about parameters
   directly (a container parameter's word is a heap handle, an interned text's is a `@str_tab` index),
   and it stays exactly where it was. Removing the wrong guard cannot widen refusals, because refusal
   was never what this scan did for a parameterless body — it did nothing.
2. **The scan asks which name the body ENDS UP holding a double, by the LAST write.** It already
   collected every binding of every name (`scanRebinds`); it now keeps the last one per name, because
   the read answers the last write's word. A scan asking "was this name EVER bound to a float" would
   promote `u = 2.5` / `u = 3` / `return u` and print `3.0` where the reference prints `3` — a wrong
   answer traded for a wrong answer. This is the same `floatVars` rule the assignment road applies
   while emitting.
3. **Each round asks its questions inside `withFloatNames`, the table the emitted body acts on.**
   `floatReboundReturnAsksForTheDouble` was refactored into a general `(*irGen).withFloatNames(atStake,
   ask)`: it copies the float-variable table, adds the names at stake, runs the question, restores. The
   scan now passes that same helper as its own question-runner, so the answer `funcReturnsFloat` gives
   a body, the answer `isFloat` gives one of its expressions, and the answer the assignment road acts on
   while emitting are three readings of **one** table — ADR 0273's rule that a scan which decides must
   not keep a second opinion, read by the return road this time.
4. **The table is settled by repetition, and a round is snapshotted before it starts.** `v = u * 2` is
   a double only once `u` is one, and `u` is one only once its own binding is asked; the scan iterates
   to a fixed point (eight rounds is far past the depth of any real derivation chain) over a `known`
   list taken *before* the round, so no answer depends on the order Go's map iteration happens to visit
   `last` in (ADR 0273's determinism rule, which the pass's "compiles twice to the same bytes" test
   enforces from the other side). `emitClassMethod`'s call passes the same helper, so a method and a
   function agree about what a float is and differ only in what they can carry.

## Agentic rationale

- The three programs above are now **answer rows** with the reference's bytes written into them
  (`pkg/lang/float_rebound_return_test.go::TestWhatAFloatTheBodyBindsToItsOwnNameAndReturnsAnswersTheDouble`,
  `integration/float_rebound_return_test.go::TestAFunctionReturningAReboundFloatAnswersLikeCPython`), and
  fourteen of them are on the record leg (`pkg/lang/testdata/interpreter-golden.json`, recorded from the
  reference by `tools/recmerge`, which cross-checks the compiled leg before writing an entry). A future
  cycle that re-breaks the low-word read fails a table instead of shipping `0`.
- The IR-shape half is pinned beside it
  (`TestWhatTheReturnWordOfAParameterlessFloatBodySays`): a returned float local must emit
  `define double @gy_f()` ending in `ret double`, and an int-only body must keep `define i32` and never
  `ret double`. A gate that promoted every body containing an `alloca double` would answer the first
  table and be wrong in the other direction, so both directions are asserted.
- No new machine-readable surface: this cycle changes which word a `define` is given, and `--emit-llvm`
  already reports that faithfully. The `--callables` account is untouched — no callable is added,
  removed or re-shaped here.

## Codegen / IR implications

- A promoted body's `define` gains `double` in the return slot and every parameter keeps `i32` unless
  the convention already made it `double`; the `ret` writes the value `floatValue` produced. Nothing new
  is emitted — the instructions were already the right ones, and only the header and the terminator
  were lying about the word.
- The frame machinery is untouched: `rt_frame_open`/`rt_frame_close` still bracket the body and the
  `alloca double` sits above them, so a promoted body keeps its GC-scanned slot (Gap R.288's rule that
  a GC root lives in the root set, not in an alloca).
- A body that stores a double and then overwrites it with an int keeps a dead `alloca double`. That is
  an optimisation opportunity in the dead-allocation family (Gap R.290), not a wrong answer, and the IR
  test says so rather than asserting around it.
- `llc-20` verifies every promoted module; the shapes in the tables were checked through `llc` on both
  the linked and the in-process-JIT leg, and no row reaches exit 2.

## Alternatives rejected

- **Leave the guard and add a special case for "no parameters but a float local".** Two rules deciding
  one word is how the two backends of this repo keep disagreeing with themselves; the guard had no
  reason, only a hypothesis ("a rebind needs a parameter") that the code below it had already outgrown.
- **Keep a separate float table for the scan.** The scan would then hold an opinion the emission does
  not consult, which is exactly how `return -x` once reached a `define double` with an `ret i32` and got
  the module rejected (Gap R.3c's original report). One table, asked through `withFloatNames`.
- **Ask the binding expressions directly, once, without repetition.** Cheap, and wrong for the derived
  name — the third shape above. A fixed-point pass over a snapshot is four lines and order-independent.
- **Answer `v = u * 2; return v` by promoting every body that mentions a float.** Promoting `u = 2.5` /
  `u = 3` / `return u` prints `3.0` for `3`; promoting a comparison's answer puts an `i32` in a
  `double`'s word. Both are the same bug pointing the other way, and both are asserted against.
- **Widen the scan to names the return reads *through* a call.** The shape looks like a neighbour
  (`def twice(): return half() + half()` beside `def half(): u = 3.0` / `v = u / 2` / `return v`) and it
  already answers: `3.0`, the reference's own bytes, on both legs — it is a group of
  `programs/float_answer_word.gy` and the matrix asserts it. A callee's answer word is that callee's own
  question; the caller reads what the `define` says. Nothing here needed changing, and inventing a fix for
  a program that answers is how a cycle stops being one feature.

## Consequences

- **Closed:** roadmap Gap R.338 — the return word of a float the body binds to its own name, which the
  ADR 0254 gate declined to ask about. The `define`/`ret` pair and the derived-name derivation are both
  pinned in the two new tests above.
- **Filed, not fixed:**
  - **Gap R.339 — the float-variable table is one map for a whole module.** `def bound(): u = 9.5 /
    return u` written above `def lastWrite(): u = 2.5` / `u = 3` / `return u` prints `9.5` and then
    `3.0`, where the reference prints `3` — and `lastWrite` alone prints `3`, so the answer on screen
    depends on what another function in the file bound. The assignment road records "this name's last
    write produced a double" in `g.floatVars` and nothing clears it at the next `define`, so every
    predicate reading that table is answering a question about one body with evidence from all of them.
    The conformance program caught this, `TestConformanceMatrix` failed it, and the group came OUT of the
    asserted program rather than the ledger coming around to the wrong bytes; the pair is pinned with the
    reference's answer beside today's in
    `pkg/lang/float_rebound_return_test.go::TestWhatTheFloatAnswerWordStillOwesIsFiledNotFixed`.
  - a **method** whose body binds its own name to a double and returns it still refuses, because a
    method's return word is fixed at `i32` together with the receiver's — the same limit the parameter
    form pins, and the record answers these;
  - the dead `alloca double` a promoted body leaves behind when the float binding is overwritten before
    the return (the dead-allocation family, Gap R.290).
- **Measured and recorded:** fourteen sources went to `tools/recmerge` (the record grew to 7858 sources),
  every one of them cross-checked against the compiled leg before the entry was written.
