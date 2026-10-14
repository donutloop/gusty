# A declined container answer is read by the doors that ask the object, and refused by the doors that ask the notebooks

Status: accepted
Date: 2026-10-14
Related: ADR 0333 (the container-answer scan, whose second half this reads), ADR 0334 (the subscript door over
an answer), ADR 0335 (the slice door), ADR 0339 (a loop asks its iterable what it is), ADR 0273 (the pair door's
rule that a scan which opens a door must not speak over the road that named what it could not carry),
ADR 0166 (a refusal names what is missing, at exit 1), ADR 0232 (one question asked once), ADR 0181 (the rooted
slot an answer travels in), roadmap Gap R.67 (the scan's first half), roadmap Gap R.261 (this row).

## Context

`containerAnswerScan` answers two questions and returns them as two maps. Before this row it returned one, and
the toolchain read that one as the whole answer:

```go
// before
func containerReturningFuncs(prog *Program) map[string]containerAnswer
// after
func containerAnswerScan(prog *Program) (map[string]containerAnswer, map[string]containerAnswer)
```

A function reaches the first map only when *both* halves hold: every `return` arm names one container kind
(`containerKindOfIn`), **and** every value that fills one of that container's slots is a value the table can
write down (`containerAnswerDescribesItsSlots`). The second half is not decoration. A slot filled by a call, a
comprehension or a format may hold the (payload, tag) pair — the pair the print road carries and the ordinary
road reads as its payload alone — which is exactly how Gap R.67 got `[2, 1]` out of `pack(2.5)` where the
reference has `[2.5, 1]`. Declining is right.

What was wrong is what happened next. A declined function is absent from the map, absence is read by every
consumer as "not a container", and the ordinary call road then lowers the call to the single `i32` it keeps for
a value: the heap handle. `print` wrote that word with the number printer. Measured off `python3`, at exit 0:

| program | reference | before |
|---|---|---|
| `def f(): out = []` / `out.append(g())` / `return out` / `print(f())` | `[5]` | `0` |
| `def f(): out = set()` / `out.add(g())` / `return out` / `print(f())` | `{5}` | `0` |
| `def f(): out = {}` / `out["k"] = g()` / `return out` / `print(f())` | `{'k': 5}` | `0` |
| the dict above with `def g(): return 2.5` | `{'k': 2.5}` | `0` |
| the list above with `for v in f(): print(v)` | `5` | *(nothing)* |

Every one of those is the priority-1 class: a value the program never wrote, at the exit code that says the
program was answered. `str(f())` and `repr(f())` already answered correctly, because the render road hands the
handle to `rt_str_of_container`, which walks the object — so the object was never the problem. The problem was
that only one position had ever been handed it.

## Decision

**The scan returns the decline as a fact, and each position is asked who answers its question.**

1. `containerAnswerScan` returns `(described, undescribed)`. A function the walk *proved* a container and then
   declined for its slots goes into the second map with its `kind` and **without** its `arms`: a table that could
   not read the slots does not get to describe them, and `recordContainerAnswer`'s notebook writes are all
   derived from arms. A body the walk cannot prove at all (it can fall off its end, it yields, it is decorated,
   an arm names a kind the other arms do not share) goes into **neither** map, and keeps the road it has always
   had — refusal included. `ContainerAnswerIsUndescribed` exports the question for the front end and the tests.

2. Which positions may take such an answer is a method on the record, not a choice each door makes:

   ```go
   type containerAnswerRead int
   const (
       containerReadSlots   containerAnswerRead = iota // index, slice, comparison, builtin argument
       containerReadWhole                               // print, str, repr, f-string field, binding
       containerReadShape                               // a condition, `not`
       containerReadMembers                             // `x in f()`
       containerReadWalk                                // a loop's iterable
   )
   func (a containerAnswer) takesRead(read containerAnswerRead) bool
   ```

   The division is not "can this position hold a pointer" — it is **who answers the question the position is
   about to ask**. `print`, `str`, `repr`, an f-string field and a binding hand the handle to a runtime entry
   point that walks the object; a condition asks the object its count, a slot every builder writes; a membership
   test asks the object whether it holds the needle (`rt_contains`). None of them needs the table to have read a
   slot — only to know which container it is, which is the half the second map keeps.

   An index read, a slice, a comparison, a builtin argument and a loop's iterable are the other kind: they read
   a slot back out of the object **using the notebooks this table writes** (`listElemStr`, `dictKeyStr`,
   `mixedLists`, …). A declined answer has no notebooks, because the walk could not have written honest ones.
   Those positions refuse.

3. **A door that declines must not fall through.** `containerAnswerReadIsRefused` is the guard: where the callee
   is a proven container and this position's read is the half the record cannot answer, the loop road and the
   membership road return the refusal instead of continuing to the road that would print the handle. Without it
   the row would trade one wrong answer for another — the first attempt at this fix, which let every
   handle-carrying position take the answer, printed `0` for `for k in f()` where the reference prints `k`.

4. **The default is the strictest read.** `containerAnswerOperand(b, e)` is
   `containerAnswerOperandRead(b, e, containerReadSlots)`. Every call site that wants the wider rule says so by
   name, so the change is monotone: a door can only widen from what it was, never shut, and an unconverted site
   keeps the behaviour it has always had.

5. **One slot question, asked once.** The predicate that decides whether an expression may fill a slot lived in a
   closure inside `containerAnswerDescribesItsSlots`. It is now `containerAnswerSlotReader.slot`, a method on a
   type holding the three records the question reads (`binds`, `out`, `defines`). The full door and — for the
   membership and loop judgements — the narrower reading of the same data go through it. ADR 0232's rule: a
   question asked twice, in two places, becomes two answers.

6. **The refusal names the missing half and the way around it** (ADR 0166), at exit 1, and never the compiler's
   own machinery:

   ```text
   codegen: "f" hands back a container whose slots this backend will not describe: the object is built, and its
   `return` says list, dict or set, but a slot is filled by a value the answer table cannot follow (a call, a
   comprehension or a format), and reading the heap handle as a number would print what the allocator handed out
   where the reference prints the contents — bind it to a name and print that, or fill the slots with values the
   table can read (roadmap Gap R.261, Gap R.67, ADRs 0333 and 0344)
   ```

   "Bind it to a name" is an instruction that works: `ys = f()` reaches the binding road, which records the name
   as a container variable, and `print(ys)` has printed `[5]` for as long as the scan existed.

## Agentic rationale

An agent consuming this toolchain needs three things from a refusal, and the sentence above carries them: the
function **the program wrote** (`"f"`, not a mangled symbol), the half that is missing (the slots, not the
object), and a rewrite that is known to answer (`bind it to a name`). The class of defect this row closes is
worse than a refusal for an agent: an exit-0 `0` is a value the agent will carry onward, and no amount of
downstream checking recovers it. So the interface rule is the same one ADR 0211 states for every other door —
the toolchain would rather say "no" in words than say "yes" with a number it invented — and the machine path is
unchanged: exit 1, one sentence, no Go type names, no `0x`, no `__cans` temps (asserted, not hoped for, in
`container_declined_answer_test.go`).

## Codegen / IR implications

- The answer of a declined call is materialised exactly like the answer of a described one: an `alloca i32`,
  rooted by `gcReg` (ADR 0181), the handle stored into it, and `settleContainerKind` recording which of the
  three kinds it is. The main body then asks `rt_print_list` / `rt_print_list_mixed` / `rt_dict_print` /
  `rt_set_print` / `rt_str_of_container` for that name — the printers a container *variable* is printed by. The
  IR row in the unit test asserts the rooted slot (`__cans…`) and the container printer both appear; the
  integration table runs `llc -filetype=null` over each of them.
- No new runtime entry point, and no change to any object layout: what changed is which doors are handed the
  handle, not what the handle points at.
- The `f-string` field goes through the same door as `str`/`repr` (`renderPairContainer`), which is why it is
  `containerReadWhole`; the format-spec engine below it is untouched (ADR 0299).
- The pair door's permission (`containerAsked`) is taken only around the lowering of the call itself and
  released on the same page, so a nested call in an argument keeps its own honest refusal (ADR 0273).

## Alternatives rejected

- **Let every position that can hold a handle take the answer.** Tried first; it is what the row looks like if
  you read "can hold" instead of "asks the object". It printed `0` for `for k in f(): print(k)` where the
  reference prints `k` — the loop road read element kinds out of notebooks the scan had never written. A fix
  that turns one silent-wrong into a different silent-wrong is not a fix.
- **Refuse every position that cannot describe its slots.** Also tried, and it is *safe* — but it turned
  `print(str(f()))`, `print(repr(f()))` and `print(5 in f())`, all of which answered correctly before the row,
  into refusals. A fix that costs more than the bug is the other way to fail this row.
- **Describe the declined slots by following the call.** A slot filled by a call can be a (payload, tag) pair,
  and ADR 0273 exists precisely so this table does not print a pair as its payload; `def pack(v): return [v, 1]`
  / `pack(2.5)` printed `[2, 1]` for `[2.5, 1]` once, and that is the bug `containerAnswerDescribesItsSlots`
  was written to keep out. Reopening the pair question is its own row, not this one.
- **A per-door boolean** (`g.containerAnswerAllowedForPrint`, and so on). Rejected because the judgement would
  then live in seven places, each free to drift, and the row's own test could not ask the record what it
  permits. `takesRead` is one table with one test.
- **Widening `containerAnswerCallIsCarriable`** so declined functions never reach the door. That predicate is
  about *arguments* (a float argument carries a pair into a slot); folding a body-side judgement into a
  call-site predicate is how the two questions start answering each other.

## Consequences

- Three silent-wrongs and one silent-nothing are gone, replaced by the reference's answer or by an exit-1
  sentence that says what to do next.
- Two capability rows stay open, recorded with the reference's answer beside them so they are owed work and not
  folklore: an **index read** of a declined answer (`print(f()[0])`, `print(f()["k"])`) and a **loop over** one
  (`for v in f()`, `for k in f()`). Both are notebook doors; opening them means recording what a declined
  answer's slots hold, which is the claim this row deliberately refuses to make.
- The scan now has a second answer, so every consumer that iterated the first must be audited: only
  `containerAnswerOf` and the doors read it, and `ContainerAnswerIsUndescribed` is exported so the front end can
  ask the same question the tests do.
- A pattern for the next scan-shaped row: a table that declines is allowed to *keep* what it proved, and the
  doors are told who they ask, not what they may carry.
