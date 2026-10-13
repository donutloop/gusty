# ADR 0336: a slice bound is asked what it is — a position, the absence of one, or the reference's TypeError

Status: accepted (2026-10-14)
Amends: [ADR 0329](0329-an-index-that-is-not-a-position-names-its-type.md) (the same question at the subscript door, for an `Index`'s position — this row is that gate with the bounds in it), [ADR 0335](0335-a-slice-hands-back-a-copy-of-the-bases-slots-tags-included-and-the-copy-is-a-list-the-records-know.md) (the door this row stands in front of; its filed table carried this shape as Gap R.233)
Extends: [ADR 0229](0229-a-string-slice-with-values-for-bounds-asks-the-table.md) (a string slice whose bounds are values still has an answer, and the same bounds are positions there), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md) (the exit-2 half: a container bound reached `rt_slice`'s i32 argument as a global address and `llc` rejected the module), [ADR 0214](0214-a-trap-the-program-cannot-name-is-not-handlable.md) (the raise is the program's, and its `except` takes it)
Supersedes: nothing
Related: roadmap L11.4 (the position/key rule), Gap R.233 (closes), Gap R.222 (paid: the index half of the same question), Gap R.236 (paid by [ADR 0337](0337-a-slice-step-is-asked-what-it-holds-zero-is-the-programs-valueerror-and-a-run-that-has-to-be-killed-is-the-compilers-bug.md)) / R.237 / R.238 / R.239 / R.240 / R.241 (newly measured here, filed here)
Owner: `pkg/lang/slice_bounds_kind.go`, `pkg/lang/slice_bounds_kind_test.go`, `pkg/lang/codegen.go`

## Context

ADR 0335 opened the slice door and left one question outside it, filed as Gap R.233: the road asks the **base**
what it is, and had never asked the **bounds**. `rt_slice` takes seven i32s — handle, three bounds, three
presence flags — and each bound went to the generic expression door, whatever it held:

```
xs = [1, 2]
print(xs["a":"b"])   # TypeError: slice indices must be integers or None or have an __index__ method
                     # this toolchain: [1]   at exit 0   — "a" is @str_tab index 0, "b" index 1
print(xs[1.5:2])     # the reference raises;             this toolchain: [2]  at exit 0
print(xs[[0]:2])     # the reference raises;             this toolchain: exit 2 — llc rejected the module
s  = "abcd"
print(s["a":"b"])    # the reference raises;             this toolchain: b    at exit 0
```

A text bound is an index into `@str_tab`; read as a position it selects slots, and the copy the program asked
for arrives with elements nobody asked for. A list bound is worse than wrong: the literal lowers to a `@.lstN`
global address, that address lands in an i32 argument, and `llc` rejects the compiler's module — exit 2, the
code ADR 0166 reserves for a compiler bug.

And the one bound the reference excuses, `None`, was read as a position too, because `None` lowers to the word
0 and the presence flag was set beside it as though the program had written a number:

```
print(xs[None:None]) # [1, 2] in the reference;  this toolchain: []      at exit 0
print(s[None:None])  # abcd   in the reference;  this toolchain: (empty) at exit 0
print(xs[::None])    # [1, 2] in the reference;  this toolchain: exit 2  (the runtime's own step loop)
```

That last family is the same missing question seen from the other side: a bound the program did not write,
spelled as a value, was taken for a bound the program wrote.

## Decision

**A slice bound has two legal shapes and one illegal one.** An integer — including a `bool`, which is an int —
is a position. `None` is the **absence** of a position, the same thing an omitted bound means. Anything else the
compiler can name the kind of is the reference's

```
TypeError: slice indices must be integers or None or have an __index__ method
```

raised through the same non-local road every other exception takes, so `except TypeError:` catches it and an
uncaught one exits with the program's report, not the compiler's.

Three pieces, all in `pkg/lang/slice_bounds_kind.go`, which is the door-form file beside `list_index_kind.go`
rather than a second copy of it:

1. `sliceBoundIsAbsent` — a bound is absent when it is missing, when it is a `None` literal, and when it is a
   name the binding recorded as the `None` singleton. Every road that lowers a bound (the list road's three
   presence flags, the string fold's `hasLow`/`hasHigh`, the `rt_str_slice` path's step test) now asks this
   instead of `!= nil`, so `xs[None:None]` and `xs[:]` are one slice, as the reference reads them.
2. `nonIntBoundTypeName` — `nonIntIndexTypeName` (ADR 0329's classifier) with the two rulings a *bound* needs
   that an *index* does not: `None` is legal, and an absent bound is not a claim about a kind at all. A text, a
   float, a list, a dict, a set and a tuple are refused, by literal or by the notebooks the print door asks:
   `i = "a"` with `xs[i:2]` is a text bound, because the last thing the program put in `i` was a text.
3. `badSliceBoundGate` — asks low, high, step **in that order, before any bound is lowered**, raises the one
   sentence, ends its block and reopens the continuation. Not lowering a refused bound is the point: it is what
   keeps a container-valued bound out of an i32 argument slot.

**Where the gate stands.** Every road that reads a copy asks it, because all of them lower the slice through
one door — the print door, `len`, a `for` iterable, a condition, `==`, `in` and a binding all raise the same
sentence (asserted as separate rows). The gate sits **after** ADR 0335's base question, which is the reference's
order too: for `print({1, 2}["a":"b"])` the reference complains about the set, not the bounds, and a reader who
is told about the base is being told the thing that would have to be fixed first.

**Where the gate stays silent.** A bound whose kind is a run-time fact — a slot holding a text, a call handing
back one — keeps the road it always went through, exactly as ADR 0329's gate does for an `Index`. That silence
is not innocence: `print(xs[ys[0]:2])` over `ys = ["a"]` answers `[1, 2]` where the reference raises, and it is
filed as **Gap R.237** rather than being papered over with a guess. Accusing a name whose kind nobody wrote is
the other way to be wrong, and the tracker already has the price on it (ADR 0232's rule about claiming a kind).

## What the row leaves standing, filed rather than bundled

| shape | CPython | this backend today | row |
| --- | --- | --- | --- |
| `print(xs[::0])`, `print(xs[0:2:0])` | `ValueError: slice step cannot be zero` | the run is killed with no output — **exit 2** | **Gap R.236** (paid by [ADR 0337](0337-a-slice-step-is-asked-what-it-holds-zero-is-the-programs-valueerror-and-a-run-that-has-to-be-killed-is-the-compilers-bug.md), which asks the step what it holds beside the call that would have walked forever) |
| `ys = ["a"]` / `print(xs[ys[0]:2])` | the `TypeError` above | answers `[1, 2]` at exit 0 | **Gap R.237** — the bound is a pair the compiler holds one word of |
| `def g(): return "a"` / `print(xs[g():2])` | the `TypeError` above | answers `[1, 2]` at exit 0 | **Gap R.237** |
| `def n(): return xs[1:3]` / `print(n())` | `[2, 3]` | answers `1` (and `3` over a mixed base) at exit 0 | **Gap R.238** — the scan that decides whether a function hands back a container has no arm for a slice |
| `print({1, 2}[0:1])`, `d = {1: 2}` / `print(d[0:1])` | `TypeError: 'set' object is not subscriptable`, `KeyError: slice(0, 1, None)` | refuses, naming the missing record | Gap R.234 (the base question, which the gate asks first, in the reference's order) |
| `print(xs[0:2][0])`, `print(m()["k"][0:1])` | `1.5`, `[1.5]` | refuses | Gap R.235 |
| `def at(k): return xs[0:k]` / `print(at(1.5))` | the `TypeError` above | answers `1` at exit 0 | **Gap R.237** — a float parameter belongs to the pair road, so the body holds no static kind to ask; the same body with a **text** argument raises, which is the difference between a missing record and a missing syntax |
| `print([x * 2 for x in xs[0:2]])` | `[2, 4]` | refuses (`a comprehension over xs[0:2] is refused: …`) | **Gap R.239** — the iterable door never asks what the binding door records; `zs = xs[0:2]` then `[x * 2 for x in zs]` answers |
| `print(len(s[1:3]))` over `s = "abcd"` | `2` | refuses (`len requires an inline list/dict/set literal`) | **Gap R.240** — `len(s)` answers `4`, `len(xs[0:2])` answers `2`, the fold owns the characters, and only this arm is missing |
| `except IndexError:` / `h()` in the arm | the arm runs, the program continues, exit 0 | the arm runs, then the handled exception comes back: traceback, exit 3, the statement after the `try` skipped | **Gap R.241** — the arm's statements run with `@exn_flag` set and a call's post-check takes it; reproduced with a plain out-of-range subscript, so it is not the slice road's |

## IR / codegen consequences

- A bad bound emits no call at all: the module carries the raise (`exn_msg`, `rt_die` on the uncaught path) and
  no `@rt_slice(` — asserted mechanically, because the alternative is the exit-2 shape this row retires
  (`call i32 @rt_slice(i32 @.lst1, …)`).
- An absent bound emits `hasLow`/`hasHigh`/`hasStep` clear, which is what the helper reads as "use the default";
  previously a `None` bound emitted the flag **set** with a 0, and the default was never reached. `rt_slice`'s
  signature is unchanged, and no runtime helper was added.
- The fold keeps everything it already answered: `print("abcdef"[1:3])`, `print("abcdef"[::-1])` and
  `print([1, 2, 3][1:])` still emit no object, no slice helper and no allocation — the gate stands in front of
  the object road, and a row that starts allocating for those has asked the wrong question (ADR 0249's gate and
  door).
- The raise is emitted before any bound is lowered, so a bound with a side effect does not run before the trap.
  The reference evaluates the base and the bounds first; the divergence is a print inside an argument of a
  program that is about to raise, and ADR 0329's gate has the same property — it is recorded here rather than
  fixed quietly, because "the raise comes from the subscript, not from the call" is what makes the bounds check
  safe to run first.

## Alternatives rejected

- **Fold the bounds into the existing `Index` gate.** One gate with three handles reads worse and behaves worse:
  an `Index` has no notion of an absent position, so `None` — legal here, illegal there — would have to be a
  parameter of the classifier, and the two sentences the reference words differ (`list indices must be integers
  or slices, not X` names the type; the slice sentence does not).
- **Treat a `None` bound as 0 and keep the presence flag set**, the behaviour before this row, which is what made
  `xs[None:None]` print `[]`. It is not a rounding error but a category error: the flag says *the program wrote
  a bound here*, and it did not.
- **Raise on a bound whose kind the compiler cannot prove.** It would turn `xs[ys[0]:2]` from a wrong answer into
  a refusal, and `xs[k:2]` — the ordinary case, an int in a variable — into a coin flip unless the notebooks are
  complete, which they are not. That is Gap R.237's road, with the tag in it, not a guess here.
- **Fix the zero step in the same commit.** It is a *value* question (`step == 0`) on the same three i32s, it has
  its own sentence and its own class (`ValueError`), and today it is an exit 2 — which is why it is filed as
  R.236 at the head of the family rather than folded into a row about kinds.
- **Fix the returned slice here too.** `def n(): return xs[1:3]` answers `1` because the scan that classifies a
  function's return has no arm for a slice — the ADR 0333 shape one construct further out. Bundling it would hide
  a second wrong-answer class inside a closed row.

## Verification

- `pkg/lang/slice_bounds_kind_test.go` — 27 answered shapes (the `None` spellings, the bool bounds, negative and
  out-of-range positions, a text base, a call's answer, a copy of a copy), each asserted against the reference's
  value on both legs; 28 raise rows by class **and** message with exit 2 forbidden; four catchable rows; the
  fold's negative control; the gate-stands-in-front-of-the-object assertion; and a 14-row filed table (the rows
  above, each pinned beside the reference's answer, so a door that grows the answer fails the row rather than
  letting it disappear).
- `integration/slice_bounds_test.go` (27 answered rows against `pythonOutput`, 22 raise rows, four catchable rows,
  a five-row filed table, four rows for Gap R.241, and the IR assertion that a raising module carries the raise
  and **no** `@rt_slice(` call) + `integration/programs/slice_bounds.gy` — the same doors through the CLI,
  expectations derived from CPython, the raise rows pinned by exit class, stdout silence and the absence of any
  `codegen:` in the stream, and the whole program line-for-line against
  `integration/testdata/slice_bounds.expected`. The program asserts only the doors this row answers: no
  comprehension over a copy (Gap R.239), no `len` of a text slice (Gap R.240), no user call in an `except` arm
  (Gap R.241), and no slice handed back by a function (Gap R.238) — each of those is filed with its own row and
  its own pins.
- `pkg/lang/slice_answer_test.go` keeps ADR 0335's rows green across the gate: an untagged copy still emits
  `@rt_slice(` beside `rt_print_list` and no tagged printer, and the base gate still fires before the bounds gate.
