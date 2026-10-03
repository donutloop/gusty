# 0266. An arithmetic answer the run time describes is bound with its tag, not stripped of it

## Status

Accepted. Ships with the tagged binding on the assignment road (`pkg/lang/codegen.go`, the `Name`
target of `AssignStmt`, calling the pair door `taggedArithPair` and the one binder `bindTaggedVar`), and
with `bindTaggedVar`'s bool forget in `pkg/lang/heapargs.go`. Pays roadmap **Gap R.138**; leaves
**Gap R.139** (the same pair at a call boundary) and files **Gap R.140** (the bound name used as a
number) and the double-slot limit beside it (**Gap R.98**'s word). Interpreter: unchanged, because the
evaluator carries a boxed value and was never the leg that lost the kind.

## Context

ADR 0265 opened the pair road — the payload and the tag travel to the target, the target does the
arithmetic in the one word that holds both families, and the answer comes back with its own tag — and
opened it **where the print dispatch asks for a value**. One statement earlier, the same expression met a
different door:

```
xs = []
xs.append([7, 8])
n = xs[0][0] * 2      # CPython 14 · --interp 14 · --aot exit 1, "index cannot reach into xs's slots"
print(n)
```

That is the record Gap R.138 was filed from, and it is the shape of a half-lift: the arithmetic worked in
`print(...)` and refused in a binding, so the language answered a program only when the program was
written to please the printer. The reason was structural, not accidental. The print dispatch wants a
(payload, tag) pair because the mixed printer dispatches on the tag; the assignment wants one `i32`,
because after a hundred statements of this backend an ordinary variable is an `alloca i32` and its kind
lives in the compiler's notebooks (`floatVars`, `listVars`, `boolVars`, `noneVars`, `internedVars`). The
pair road was never tried at the binding, so `g.value` walked into the ordinary numeric road, which still
insists on a static kind before it emits an `add`, and refused.

The refusal was honest and still wrong. A program that has to be written two ways to work depending on
whether its answer is printed or named is not a language; and ADR 0166's ladder says an exit 1 on a
program the reference runs is the compiler's failure, not the program's.

## Decision

**Bind the pair. Where an assignment's value is arithmetic the ordinary numeric road would have refused,
the name stores the (payload, tag) pair the answer arrived with, in the tagged-variable shape the print
door, `arithOperandPair` and every comparison already read.**

1. **One gate, two callers.** The binding asks the same question the print dispatch asks —
   `g.arithWouldRefuse(value)` — which is the cheap, side-effect-free "would the ordinary numeric road
   have refused this?", and then calls the same `taggedArithPair`. A program that was answered before is
   not rerouted through the new road: the road is only ever taken where the old one exited 1. The
   gate — `computeNumericSlotChains`'s program-wide proof that the slots the read reaches hold numeric
   literals (ADR 0265) — is untouched, so nothing about *which* programs may take the pair road changed
   with this ADR; only *where* the answer is allowed to land.

2. **One binder.** The store is not written at the call site. `bindTaggedVar` is already the single door
   for the shapes that bind a pair — a dict value, a list element, a slot of a container reached through
   a slot — and it is the one place that frees the old heap binding, clears the GC root, forgets the
   compile-time kind notes (`strVals`, `internedVars`, `noneVars`, `floatVars`, and now `boolVars`),
   allocates both slots once, and marks both bound for ADR 0228's definite-assignment question. An
   arithmetic answer is the fourth shape; writing its store inline would have made the fifth thing four
   other places already agree on.

3. **The name is tagged by its own expression, not by a signature.** `n = xs[0][0] * 2` where the slot
   holds `7` gives `n` the int tag; the same source with `7.5` in the slot gives it the float tag, and
   `print(n)` renders `14` and `15.0`. The kind is a fact about the value the expression computed — L11.1's
   sentence, applied to the fourth boundary in a row (the read, the print, the comparison, now the binding).

4. **The double slot stays on its own road.** If the name's slot was already allocated as a `double` (the
   body settled it as a float earlier, ADR 0254's return-word rule reads the same notebooks), the road is
   declined and the program keeps the refusal it always had. Storing the pair's `i32` payload through a
   `double*` is the module `llc` rejects, and ADR 0166 counts exit 2 on a program the reference runs as the
   compiler's bug. A double that has to travel is **Gap R.98**'s tagged value word — that is the missing
   instruction, not this binding.

5. **The refusal tells the truth about where the pair came from.** Once a binding could put a name in the
   tagged state, the numeric road's message about such a name — "`x` comes from a loop over a mixed list"
   — began to describe a program that had no loop in it. `mixedTaggedVarErr` now names the *state*
   ("travels with a tag … bound as a (value, tag) pair") and lists the origins without claiming one;
   the pinned substring "using it as a number needs a tagged value" is unchanged, so the diagnostic
   contract `mixed_list_test.go` asserts keeps holding. This is roadmap **Gap R.38**'s rule applied
   forward: a refusal may be honest about the other leg and still be a lie about this one.

## Consequences

- `programs/probe_arith_result_bound_to_a_name.gy` prints `14` on all three engines and left the oracle
  ledger for `conformanceStandalone()`; `programs/numeric_slot_arith.gy` grew its two bound-name lines and
  its golden was re-derived in this commit.
- **Gap R.140** is what this commit measured next door: `print(n + 1)` on the name just bound still refuses,
  because the numeric road reads the name and asks it for a static kind — the value slot means nothing
  without its tag and this context has no word to carry a pair in. Pinned as
  `programs/probe_tagged_answer_used_as_a_number` with the interpreted leg's `15` and the compiled leg's
  exit-1 refusal each recorded per leg, so the day the door opens the ledger fails until the row goes.
- **Gap R.139** stays open and its ledger wording moved: the caller now *has* the pair (this ADR paid the
  binding half), and the parameter is the half that does not take it.
- Binding is now a tagged binding in four shapes. That is why the bool forget moved into `bindTaggedVar`
  rather than being written at the new call site: ADR 0172's "the latest binding decides how a name
  renders", carried to bools by ADR 0257, has to be true for the fourth shape too, and a rule written at
  one of four callers is a rule that drifts.
- The pair road is still only reached from two places — the print dispatch and a name binding. The
  positions it does not reach (a call argument, an arithmetic operand, a container slot written by key,
  `+=` onto an int variable) stay refusals named by their owners: Gaps R.139, R.140, R.98.
- The gate keeps its coarseness, and this commit inherited its cost: adding a string to *any* container
  in a program closes `+` and `*` on run-time-described slots everywhere in it, bindings included. It
  refuses more, never answers wrongly (ADR 0265).

## Alternatives rejected

- **Answer the binding by printing it.** Folding the bound value into the print site that consumes it
  would have made the flagship program work and left every other binding refused — the half-lift this ADR
  exists to remove, in a different place.
- **Settle a static kind for the arithmetic and store one word.** `xs[0][0] * 2` where the slot holds `7.5`
  then answers `15` (or `15.0` where CPython answers `15`). A kind guessed from the operator is the wrong
  answer with exit 0, which ADR 0166 ranks below the refusal it replaced.
- **Reuse the tagged-variable record without the binder** (`g.taggedVars[n] = true` and two stores). That
  is the fifth copy of `bindTaggedVar`'s free/clear/allocate/mark sequence, and the GC-root clear and the
  ADR 0228 binding mark are exactly the two things a fast path is most likely to skip.
- **Store the pair into a `double` slot with a bitcast.** One instruction, and the tag becomes a
  reinterpreted double, so the printer reads a tag that is a bit pattern. It verifies and it is wrong —
  the same family as the empty-operand `fdiv` ADR 0253 caught.
- **Leave the loop-specific message and add a second one for bindings.** Two messages for one state is how
  the "wrong message" half of Gap R.38 starts; one message that names the state and lists the ways it
  arises cannot drift.
- **Widen the commit to the call boundary (Gap R.139) as well.** The parameter needs its own convention —
  what a callee does when its argument arrived as a pair and its own body settled a static kind (Gap
  R.110 measured what an invented convention does to that answer) — and AGENTS.md's one-feature rule keeps
  the two rows honest about which half was paid.
