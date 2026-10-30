# ADR 0468 — a base with NO slots answers the slice road too: the kind the source wrote, not a refusal about a list

**Status:** Accepted
**Date:** 2026-10-31
**Decides:** roadmap **Gap R.559** (the slice road refuses a base that has no slots at all, where the reference names the kind)
**Files:** **Gap R.560** (a name the program bound to an int has no binding record to read), **Gap R.561** (a name a container bound and a scalar rebound keeps the copy road's refusal), **Gap R.562** (a call's answer and a slot of a named list have no scalar kind record)
**Corrects:** an assumption carried into this row from the cycle that filed **Gap R.556** — that `rt_slice`
carried a constant `"not subscriptable"` sentence in its fourth argument. The helper takes no sentence at all and
has no raise arm (see *The assumption measurement corrected*); **Gap R.556**'s own row text never claimed it, and
nothing downstream repeated it.
**Keeps open:** **Gap R.235**, **Gap R.540**, **Gap R.554**, **Gap R.555**, **Gap R.556**, L11.1
**Related:** ADR 0466 (the base-first order this row extends, and whose two container arms this row must not disturb), ADR 0420 (the subscript road's `constantSubscriptKind`, the door this row asks again instead of writing a second one), Gap R.227 (the folded element read that door already answers), ADR 0336 (the bounds door, whose sentence a scalar base must never earn), ADR 0335 (the copy road's gate, which is where these programs used to refuse), ADR 0172 / ADR 0257 (the last-binding records), ADR 0274 (the double slot), ADR 0232 (one question asked once; a kind claim imported from a writer, never invented), ADR 0273 (a gate may not cost a program that compiles today), ADR 0166 (exit 2 is our bug), ADR 0211 (the exit-code contract), ADR 0302 (the two witness legs), ADR 0406 (`except … as err` binds the raise's arg)

## The rule

**Every base answers the question "what are you", and a base with nothing to slice into answers it with its own
name.** `1[0:1]`, `1.5[:]`, `True[0:1]`, `None[::]` are not missing features and they are not the copy road's
business: they are `TypeError: 'int' object is not subscriptable` and its three siblings, catchable by the
program that wrote them.

## Context

ADR 0466 gave the slice road its base question and answered it for the two containers that have no positions —
a set and a dict. Three families of base were left on the old road, and all three refused:

```
$ gustyc --eval 'print(1[0:1])'
gustyc: jit: codegen: cannot slice 1: the copy is a list whose slots are the base's payloads with their tags,
and the base is not a list the records describe — a literal, a variable a binding recorded as a list, a call
that hands back a list, or a slice of one (roadmap Gap R.235, ADR 0335)                       # exit 1
$ gustyc --eval 'print(None[0:1])'      # the same refusal                                     # exit 1
$ gustyc --eval 'b = True
               print(b[0:1])'           # the same refusal                                     # exit 1
```

against:

```
$ python3 -c 'print(1[0:1])'      # TypeError: 'int' object is not subscriptable
$ python3 -c 'print(None[0:1])'   # TypeError: 'NoneType' object is not subscriptable
$ python3 -c 'b = True
            print(b[0:1])'        # TypeError: 'bool' object is not subscriptable
```

Three things made this worth a row rather than a bullet inside ADR 0466.

1. **The refusal describes a program the author did not write.** "the copy is a list whose slots are the base's
   payloads with their tags" is a sentence about a list; the program contains no list. And for two of the shapes
   it was worse than wordy — `print((1)[0:1])` answered `cannot slice *lang.IntLit` and
   `print(xs[0][0:1])` answered `cannot slice *lang.Index`, the compiler's own AST in the place where a reader
   needs the source (**Gap R.232**'s disease, **Gap R.249**'s rule; the copy road's half is filed as
   **Gap R.563**).
2. **The subscript door beside it had answered this for two ADRs.** `print(1[0])` raises
   `'int' object is not subscriptable` through `constantSubscriptKind` (**ADR 0420**, Gap R.227), including the
   folded element read `print([1, 2][0][0])`. So the same object, read at the same subscript with a different
   kind of key, got an exception on one road and a compiler refusal on the other — two answers to one question
   (**ADR 0232**).
3. **A bool is the language's favourite test case.** A verdict is an int *where a position is asked*
   (`[1, 2][True:2]` is `[2]`, ADR 0336 says so) and a bool *where the object is asked* (`True[0:1]` is
   `'bool' object is not subscriptable`). A gate that read `constantSubscriptKind`'s int arm for the object
   question would print `'int'` and be wrong in a way a reader of the Python docs would catch immediately.

## The assumption measurement corrected

The note this cycle started from said the slice door's *"4th argument is the constant `not subscriptable` for
every base"*, and the plan was to thread a kind word into that argument. It is not. `rt_slice` is

```
define internal i32 @rt_slice(i32 %h, i32 %low, i32 %high, i32 %step, i32 %hasLow, i32 %hasHigh, i32 %hasStep)
```

(`pkg/lang/codegen_ir_slice.go`): seven i32s, **no message argument**, and no raise arm at all — it normalises
its bounds, copies slots with their tags and returns a handle. A base with no slots never reaches it, because
**ADR 0335**'s gate refuses the copy first. So the entire question is compile-time, and the fix is this gate,
not the helper. The lesson — *re-measure a remembered claim about code before planning against it* — is recorded
in `_001_session_learnings.md`; **Gap R.556** keeps its shape (`print({1, 2}[0:1][0])` still refuses, which is
what its row actually says and what its row is about) and is untouched by this one.

## The decision

`pkg/lang/slice_base_kind.go` grows one more arm, in the same single gate `sliceRaisesWhatTheBaseIs`, which is
called at the top of `valueSlice` (**ADR 0466**'s hook) before any bound is lowered:

```go
if kind, noSlots := g.sliceBaseWithNoSlots(n.Obj); noSlots {
    g.raiseTo(b, exnCode("TypeError"), "TypeError", notSubscriptableSentence(kind), n.Span())
    …
}
```

1. **The door asks the subscript road's classifier, not a new one.** `constantSubscriptKind` (**ADR 0420**) is
   what already answers `print(1[0])`; asking it again is the point — a second classifier would be a second
   answer, and the two would drift. It names `int`, `float`, `bool` and `NoneType` for what the SOURCE wrote,
   including the folded element read, so `print([1, 2][0][0:1])` and `print(1[0:1])` are one complaint.
2. **A name is read from the last-binding records** — `boolVars`, `noneVars`, `floatVars`, the same three
   **ADR 0336**'s `nonIntIndexTypeName` reads and **ADR 0172**/**ADR 0257** write. `f = 1.5`,
   `half = 1 / 2`, `flag = 1 == 1`, `void = None` are named objects; `n = 1` is not, because **nothing writes an
   int claim** — ints are the default word and no formatting needs one. That is **Gap R.560**, and it is a row
   rather than an exception in the gate: **ADR 0232** forbids inventing the kind.
3. **The container notebooks are asked FIRST, and that order is load-bearing.** Measured:
   `xs = 1.5` / `xs = [1, 2]` / `print(xs[0:1])` prints `[1]`, and `floatVars` still names `xs` — a container
   binding does not clear the scalar records. A gate that asked the scalar records first would raise
   `'float' object is not subscriptable` at a program that answers today, which **ADR 0273** forbids in words.
   The mirror-image residue (`xs = [1, 2]` / `xs = 1.5` / `print(xs[0:1])`, which the reference traps and this
   backend refuses because the container notebook still claims the name) is **Gap R.561**.
4. **A raise, emitted where the read was**, through `raiseTo` — the non-local road every trap in this backend
   takes — so `except TypeError:` takes it, an uncaught one exits **3** with the reference's two lines and a
   frame naming the line, and `except TypeError as err` binds the message without the caption (**ADR 0406**).
   No arm emits `rt_slice`, and the counter-row (`print([1, 2][0:1])` DOES call it) keeps that absence
   meaningful.

## What the gate buys, measured

| shape | the reference | parent | this commit |
|---|---|---|---|
| `print(1[0:1])` | `TypeError: 'int' object is not subscriptable` | exit 1, a refusal about a list | exit 3, catchable |
| `print((1)[0:1])` | the int's sentence | exit 1, `cannot slice *lang.IntLit` | exit 3, catchable |
| `print(1.5[:])` | `'float' object is not subscriptable` | exit 1 | exit 3, catchable |
| `print(True[0:1])` | `'bool' object is not subscriptable` | exit 1 | exit 3, catchable |
| `print(None[::])` | `'NoneType' object is not subscriptable` | exit 1 | exit 3, catchable |
| `b = True` / `print(b[0:1])` | the verdict's sentence | exit 1 | exit 3, catchable |
| `half = 1 / 2` / `print(half[0:1])` | the number's sentence | exit 1 | exit 3, catchable |
| `print([1, 2][0][0:1])` | the int's sentence | exit 1, `cannot slice *lang.Index` | exit 3, catchable |
| `print(1["a":"b"])` | the int's sentence, **not** ADR 0336's | exit 1 | exit 3 — the object is asked first |
| `print(1[0:1:0])` | the int's sentence, and no `ValueError` | exit 1 | exit 3 — the step is never strided |
| `def f(): return 1[0:1]` / `print(f())` | the int's sentence, framed at `f` | exit 1 | exit 3, two frames |
| `try: print(1[0:1]) except TypeError as e: print(e)` | `'int' object is not subscriptable` | refusal | exit 0, the raise's arg |
| `print([1, 2][True:2])` | `[2]` | `[2]` | `[2]` (a verdict is a position) |
| `xs = 1.5` / `xs = [1, 2]` / `print(xs[0:1])` | `[1]` | `[1]` | `[1]` (the safety order) |
| `xs = [1, 2]` / `xs = 1.5` / `print(xs[0:1])` | the number's sentence | refusal | refusal — **Gap R.561** |
| `n = 1` / `print(n[0:1])` | the int's sentence | refusal | refusal — **Gap R.560** |
| `def m(v): return v` / `print(m(1)[0:1])` | the int's sentence | refusal | refusal — **Gap R.562** |
| `xs = [1, 2]` / `print(xs[0][0:1])` | the int's sentence | refusal | refusal — **Gap R.562** |

The witnesses. `pkg/lang/slice_scalar_base_test.go`: **15 raise rows** each asked of the compiled leg
(`jitTrap`) AND of the record (`goldenRunError`, class and message compared, a missing entry failing the case —
**ADR 0302**), **4 catchable rows** asked of the compiler AND the record byte-for-byte, an **order table** of
three bound-shapes × three-and-four bases with a distinctness check (the list earns ADR 0336's bounds sentence,
the set earns the object's, the dict hashes and misses, a scalar earns its own kind — a collapse onto one
sentence fails), a **soundness table** that asks the door with no notebooks at all (`(*irGen)(nil)`) and of a
generator whose records were written by the binding road, a **counter-row** in the IR witness so "no `rt_slice`"
cannot be earned by deleting the helper, and a **filed-not-fixed table** with the reference's answer beside each
refusal. `integration/slice_scalar_base_test.go`: 28 raises at the CLI against a live CPython whose final
`Class: message` line is read out of the reference per row (exit 3, empty stdout, a traceback frame naming the
line, and `codegen:` on stderr failing the row), 9 handler rows compared byte for byte, 8 served bases still
answering at exit 0, 5 refusals pinned at exit 1 (never 0, never 2), and the emitted module asked for a global in
a value position — the shape that made `llc` reject these programs (**ADR 0166**). The corpus:
`integration/programs/slice_scalar_base.gy` joins `conformanceStandalone` as a parity row (corpus 288 → 289,
matrix 274 → 275, parity-asserted 238 → 239, oracle `match` 224 → 225, `fail` 0, `drift` 0).
`tools/recmerge` wrote **37 entries (10749 → 10786)**, none refused — every raise recorded from the reference
with the compiled leg cross-checked against the same sentence.

## Why not the alternatives

- **Give `rt_slice` a runtime kind check.** It cannot be right: the sentence has to name the kind the SOURCE
  wrote, and by the time the helper runs the base is a handle whose tag the copy road never agreed to write for
  these shapes. It would also turn a compile-time-known raise into a runtime abort, which is exactly how a row
  that should be catchable by `except TypeError:` ends up as an exit-2 story (**ADR 0166**).
- **Reuse ADR 0336's `nonIntIndexTypeName` as the base classifier.** It is the right SHAPE (its Name arm reads
  the same three records) and the wrong answer set: it deliberately returns *not an int*, so it is silent about
  the most common base of all, and it has no `bool` arm because a verdict IS a legal index. The base question
  needs the object's name, which is what **ADR 0420**'s door already returns.
- **Ask the scalar records first** (the last-binding rule, ADR 0172). Rejected by measurement, in both
  directions: `floatVars` survives a container binding, so the order would raise `'float'` at
  `xs = 1.5` / `xs = [1, 2]` / `print(xs[0:1])`, a program the reference answers `[1]` and this backend answers
  today (**ADR 0273**). The residue of the safe order is a row (**Gap R.561**), not an exception.
- **Let `n = 1` / `print(n[0:1])` raise too, by reading the int's word.** It is tempting — `foldConstInt`
  answers names from `constBindings` — but that notebook is a comprehension's constant walk, not a binding
  record, and it flattens `True` to `1`, which is the `'bool'` row turned into a lie. A row rather than a guess
  (**Gap R.560**).
- **Leave the four shapes as refusals.** **ADR 0335** won them that right against exit 2, and this ADR does not
  undo it — the four shapes the records cannot name still refuse at exit 1. What changed is that a refusal is
  reserved for the shapes nobody can name, instead of covering the ones the source spelled out.

## Consequences

- The slice road now asks ONE question of EVERY base — "what are you" — before it asks anything of a bound, and
  the three answers it can give are: a base with positions goes on to the bounds door, a base that hashes gets
  the hashing door's order (**ADR 0328**), and a base with nothing to slice into names itself. A future base
  (bytes, a class with `__getitem__`) enters through this gate, not beside it.
- Three refusals are owed with numbers: **Gap R.560** (the int binding record), **Gap R.561** (the
  container↔scalar rebinding residue), **Gap R.562** (a call's answer and a named list's slot carry no scalar
  kind). **Gap R.563** files the copy road's `%T` leak — `cannot slice *lang.Index` — which is **Gap R.232**'s
  disease in `exprSnippet`, whose default arm still answers with a Go type name.
- `pkg/lang/capability.go`'s `lower.unsupported.slice-base-not-described` is UNCHANGED by this row, and that is a
  measured claim rather than an omission: its code, its shape sentence and its sample still classify the refusals
  that remain (the guard over the registry's samples passes on this commit), and the shapes this row turned into
  raises were never in the registry. Naming **Gap R.560**–**Gap R.562** in that entry's `Rows` cell is a one-line
  follow-up in a file another cycle was editing while this row landed; it is owed, and it changes no behaviour.
- `docs/language.md`'s slice section now says which bases answer, which raise, and which are refused, with the
  four sentences the reference words.
- The record grows by 37 entries to **10786**, and the places that quote it (`roadmap.md`'s Snapshot,
  `docs/operations.md`'s artifact table, `AGENTS.md`, and the artifact guard's literal in
  `pkg/lang/leading_dot_float_test.go`) move together, because the file is the authority and not the memory.
