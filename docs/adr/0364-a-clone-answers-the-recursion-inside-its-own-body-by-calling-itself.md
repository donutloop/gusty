# ADR 0364 — A clone answers the recursion inside its own body by calling itself, and the entry tuple is read by ordinal

Status: accepted
Date: 2026-10-18
Amends: [ADR 0345](0345-a-function-used-as-a-value-is-a-name-this-pass-mints-before-the-doors-that-decide-a-call-are-asked.md) (which specialised a higher-order body into one clone per tuple of functions its call sites hand its parameters, and silently assumed the body never called the enclosing function), [ADR 0340](0340-the-ordering-builtins-read-key-and-reverse-by-name-and-a-callable-key-is-called-by-the-ordinary-call-road.md) (the key door that calls a name this pass minted)
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md) (one backend; the record and CPython are the witnesses), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md) (a shape with no lowering is a named refusal, never a silent value, and never exit 2), [ADR 0273](0273-a-door-that-opens-must-not-speak-over-the-road-that-named-what-it-could-not-carry.md) (a pass that declines leaves the older honest refusal in place), [ADR 0322](0322-a-refusal-that-blames-a-container-the-program-never-mutated-is-a-pass-keyed-by-the-wrong-thing.md) (a refusal names the missing half, and one decline per shape), [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md) (one question, one asker: `pos` and `slot` are two questions), [ADR 0317](0317-a-guard-that-does-not-read-its-own-file-is-a-guard-that-never-ran.md) (the file-guards below depend on `-count=1`)
Roadmap rows: Gap R.267 (closed by this ADR), Gap R.313 (the entry-tuple ordinal, closed by this ADR), L11.7 (still 🟨 `PARTIAL` — Gaps R.265, R.266, R.268, R.269, R.255, R.281 stay owed), L11.1 (still owed — the tagged value word a *read* function needs)

## The gap, as measured

ADR 0345 clones a function whose body calls one of its own parameters: one clone per distinct tuple of
functions the call sites hand that parameter, each clone calling the entry **by name**. The account is
sound and the suite never asked the one question this row is about — that the higher-order function also
calls **itself**. Eight programs, the most ordinary shapes a Python reader writes, all at **exit 1** on
the commit before this one, against answers CPython prints without hesitation:

| program | compiled at HEAD | CPython |
|---|---|---|
| `def fact(n, f): if n <= 1: return 1` / `return f(n) * fact(n - 1, f)` / `print(fact(4, lambda x: x))` | refused: `"f" is called but is not a function this backend can build a call to` | `24` |
| the same body entered twice, once by a `def`'d `dbl`, once by a `lambda` | refused: the same | `24 6` |
| `def down(n, f): … return f(n) + down(n - 1, f)` / `print(down(3, lambda v: v))` | refused: the same | `6` |
| `def down(n, f): …` with `return f(n) + down(n - 1, f)` and `k = treble` / `print(down(3, k))` | refused: the same | `18` |
| `def count(n, f): … if f(n): return 1 + count(n - 1, f)` / `print(count(5, lambda v: v > 2))` | refused: the same | `3` |
| `def pick(a, f, g, b): … return f(a) + g(a) + pick(a - 1, f, g, b)` | refused: the same | `109` |
| the same recursion over texts (`return f(n) + down(n - 1, f)`, `f = lambda v: "ab"`) | refused: the same | `ababab` |
| `def run(v, fn): return fn(v)` / `print(run(21, inc))` | refused: `"fn" is called but is not a function …` | `22` |

The last row is not a recursion. It is a higher-order function whose **callable is not its first
parameter**, and it refuses for a different reason, which the recursion work found (below, §The second
cause). Every program in the first seven rows is one of the two shapes this language's own suite already
answers when the recursion is removed — `def twice(fn, v): return fn(v) + fn(v)` has answered `12` since
ADR 0345 landed. So the boundary was never "a higher-order body": it was "a higher-order body that
recurses", and no row said so.

Two refusals, one sentence, and the sentence blamed the parameter.

## The two causes

**1. The recursion was not a site, so the parameter was never filled.** `planDefunctionalization` reads
call sites and, for each called parameter, resolves the argument to the function it asks for; the calls it
reads are the program's calls, and a call written **inside a higher-order body** was excluded from that
walk (it is the body's own call, not a caller's). The consequence was total: with no site for the
recursion, the clone's parameter had no entry to be filled with, the plan declined, `applyDefunctionalization`
handed the program back untouched, and codegen met `f(n)` in an un-specialised body — ADR 0166's honest
refusal, printed as if the parameter were the problem.

**2. The entry tuple was indexed by the wrong number.** A call of a parameter records two distinct facts:
which **parameter** of the definition it evaluates (`pos`), and which **entry of the clone's tuple** that
parameter is asked to be (`slot`) — the entries are laid out in the order the called parameters were
collected, not in the order the source wrote them. The rewrite read `pos` as the tuple index. The two
numbers coincide exactly when every called parameter precedes the uncalled ones, which is how the suite
was written — `twice(fn, v)`, `apply1(fn, v)`, `sorted(…, key=…)`. Write the callable second —
`def run(v, fn): return fn(v)` — and the clone asked for tuple entry 1 of a one-entry tuple, bailed out of
its own rewrite, and the program fell back to the pre-pass refusal. A silent off-by-one in a coordinate
system, invisible to every program that had ever been asked.

## The decision

1. **The recursion is a site of the clone it lives inside.** `callableSelfCallsOf` reads the calls of a
   function's own name out of that function's own body, walking the body **exactly the way
   `callableBodyCalls` does** — the k-th call of the body is the k-th call of its clone, which is the
   coordinate every rewrite in `applyDefunctionalization` is already written against. Each higher-order
   function records them as `cf.self`; the clone rewrite points every one of them at the clone's own
   `define` and fills the callable's position with the same plain integer an outside call site gets, since
   a clone reads nothing there. A clone recursing into itself is the whole decision: the entry tuple the
   recursion carries is the one the clone was minted for.

2. **Handing the callable back to its own position is not a use as a value.** `fact(n - 1, f)` mentions
   `f` in a position that is *not* a callee, and ADR 0345's rule is that a called parameter which is also
   read is not substitutable. `callableSelfPassThrough` names the argument nodes the recursion hands back
   to their own positions and `onlyCalledInBody`/`callableCalledParams` credit them, so the parameter stays
   eligible. Nothing else about the parameter relaxes: `print(f)`, `xs = [f]`, a return of `f` still leave
   the function unrewritten, and ADR 0345's invariant `callableNoFunctionValuesLeft` is what says so after
   the fact on the tree that was actually produced.

3. **A recursion the clone cannot answer is declined by the pass, in a sentence per shape.** Once the
   recursion is the pass's, refusing it is the pass's too — leaving it to codegen would mean the one
   sentence (`"f" is called but is not a function …`) stands for four different programs.
   `callableSelfDeclines` asks, before any clone exists: does the recursion hand **each called parameter
   back to its own position** (`run(n - 1, other)` — the clone would need an entry for `other` too, chosen
   mid-recursion); does it **fill every parameter positionally**; is it written **inside a `lambda`**, whose
   hoisted body has a declaration and no frame to recurse through. Each answer is its own sentence naming
   the parameter and the line. A mutually recursive pair of higher-order bodies is declined where the plan
   learns nothing else was rewritable (`callableMutualHigherOrderDecline`), because a decline that fires on
   a program with work to do would take an answer away from a higher-order call that has nothing to do with
   the pair.

4. **A self-call whose name is also a parameter belongs to the frame.** `def go(n, go): return go(n)` calls
   the *parameter* — the frame's name wins over the module's here as in the reference — and that call is
   the inner-call road's, which has always answered it by name. `callableHasParamNamed` keeps it out of the
   self-call census.

5. **`pos` and `slot` are two fields, and the rewrite reads `slot`.** `callableInner` carries both, the plan
   fills `slot` from the enumeration of `cf.calledPos` (`for slot, pos := range cf.calledPos`), and the
   clone rewrite indexes the entry tuple with `in.slot`. The audit that the entries are declared for the
   calls the body makes (`callableInnerAgrees`) is unchanged — it was correct about `pos` and blind to
   `slot`, which is exactly the shape of the bug.

6. **The recursion is auditable, not folklore.** The pass's account grew `recursive_sites`: how many calls
   written inside a higher-order body now name that body's own clone. An agent planning a higher-order
   program reads one number to learn whether the recursion is in hands the compiler has, and a clone that
   recursed through the un-specialised entry — the bug this row is about — cannot leave that field at a
   plausible value by accident.

## Why a clone and not a value

Rejected: give the callable a word (a tag the value can carry, a function pointer, a symbol index) so the
recursion can pass the callable around like a value, which is what a general fix would need. That is
L11.1's row, it is what Gaps R.266/R.268/R.281 all wait on, and it is not what this row needs: the
recursion hands the parameter **the same function it already arrived with**, so the entry the recursion
must pass is the one the clone was minted for, and minting a value to carry a fact the plan already holds
would buy a `ptrtoint` this house bans. The recursive case is where ADR 0345's choice pays for itself: no
indirect `call`, no `fnptr`, no runtime addition — `call i32 @gy_fact$callable$…(i32 %t9, i32 0)`, a call to
a `define` whose name the pass wrote.

Also rejected: specialising the recursion as a *second* clone per (tuple-at-recursion) pair. It is the same
fixed point expressed as a bigger table, it multiplies definitions for a coordinate that never varies
(the recursion's tuple is the enclosing one by construction), and it would have made the arity audit of
`callableInnerAgrees` a two-level question.

Also rejected: following the recursion to a fix point (as `runProgram` iterates for a different reason).
Following it means deciding, for each clone, which function the recursion's callable *will be* — a
run-time question this backend answers by refusing rather than guessing, and the guess would be wrong for
`run(n - 1, other)`, which is precisely the program the decline now names.

## What the legs say

- **The compiled leg**: 20 answered shapes in `pkg/lang/recursive_callable_test.go`, each compiled,
  JIT-run, and printed-compared; the IR rows assert the clone calls itself
  (`call i32 @gy_fact$callable$…_0(i32 %t\d+, i32 0)`), that two call sites mint two clones each calling
  **its own** entry, that no un-specialised `define i32 @gy_fact(` survives, and — ADR 0345's ban re-asked
  of the new road — that no module reaches the recursion through `ptrtoint`/`inttoptr`.
- **The record leg**: 50 sources this row first asked were recorded from **the reference** by
  `tools/recmerge` (`pkg/lang/testdata/interpreter-golden.json`, 7636 → 7686 entries), never from this
  backend; `TestARecursiveHigherOrderBodyIsWhatTheRecordSaid` fails when the compiled answer and the record
  disagree, and the integration file fails when a row of the answer table is missing from the record.
- **The reference leg**: `integration/recursive_callable_test.go` runs the same 20 programs through the CLI
  beside a live `python3` with every expectation read off the reference at run time, plus the three
  refusals at **exit 1, never exit 2**.
- **The refusals**: 7 shapes stay refusals, and each row asserts *two* sentences — the one the user is
  shown (the ordinary road's, unchanged from before this ADR) and the one the pass's account says
  (`recursive_sites: 0`, `rewritten: false`, and a `refused[]` sentence naming the parameter and the line).
- **Determinism and naming**: every answered program compiles twice to the same bytes, and a recursive
  clone reports the name the programmer wrote (`fact`) to `--debug-info`, not the symbol the pass minted.
