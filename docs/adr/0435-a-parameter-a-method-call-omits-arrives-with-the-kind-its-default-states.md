# ADR 0435 — a parameter a method call omits arrives with the kind its default states: the text census reads the word the vector wrote

## Status

Accepted, 2026-10-27. Closes the text half of roadmap **Gap R.479**, which **ADR 0434** filed, and hands
what this row measured but does not fix to fresh rows — **Gap R.480** (a method body has no
heap-container parameter door at all), **Gap R.481** (one slot, two calls, one claim), **Gap R.482** (a
method name two classes share drops the claim for both) — and to **Gap R.35** / **L11.6** (the method's
missing double return word), which a defaulted `float` still cannot cross.

Depends on **ADR 0434**: this ADR is only sound because the call now WRITES the defaulted slot. Read it
after `0434-a-method-call-fills-the-parameter-vector-the-method-declared.md`.

## Context

**ADR 0434** made a method call lay its arguments out over the callee's declared parameters and fill every
slot the source left empty from `fd.Params[i].Default`, through the same `g.value` road a written argument
takes. The word arrived. What still arrived wrong was its KIND:

```python
class C():
    def m(self, s="hi"):
        return s

print(C().m())
```

printed **`0`** at **exit 0** where the reference — and the SAME body written as a module `def`, on the same
build — print `hi`. Along the same reading, `def greet(self, who="world")` with `print(f"hi {who}")` printed
**`hi 0`**, and `return "hi " + name` refused as if its operand were a number. `0` and `hi 0` are not
"wrong numbers" of the arithmetic kind: they are the `@str_tab` POSITION of the text, which is the exact
artifact **ADR 0366** was written to remove from the WRITTEN-argument spelling (`def fmt(self, s): return
"x%s" % s` printing `x0`). It had simply survived at the one door ADR 0434 opened a cycle later.

The kind of a parameter is a census, not a local question. `g.strParamOf` (`pkg/lang/strargs.go`,
**ADR 0366**) is the table `emitClassMethod` asks to decide whether a method parameter behaves inside the
body like a name the body assigned a text to — the print, `str`/`repr`, `len`, `in`, `==`, `%`, the repeat,
the f-string field and the char-method roads all ask `internedVars` of the name, and
`methodReturnsStr` builds its `extraTextParams` census from the same table, so the claim decides the
method's RETURN road as well. Its evidence is the arguments the program hands the callable. A slot the call
never wrote had no argument to read — and the census had two deliberate suppressions for exactly that
shape, both written when the slot was genuinely never written:

* `seed()` skipped a method's defaulted parameter (`byDefault := methods[fn] != nil`), because "a body told
  the slot holds a text reads a word nothing wrote": `print(C().greet())` for `def greet(self, name="world")`
  died by **SIGSEGV** — that is **Gap R.315**'s artifact, and the withholding was the fix;
* `w.attrCall`'s tail rule recorded the defaulted tail as evidence AGAINST the text
  (`negOf(method, i)`), which is the same reasoning seen from the call site.

Both suppressions were correct under the pre-ADR-0434 model and wrong under the new one. The default's word
is now in the slot — the binder emitted it through `g.value`, which for a text literal is the same
`rt_intern` call any written `"hi"` argument takes — so the parameter has evidence, and refusing to read
evidence is not conservatism, it is the bug: the body reads the slot as an integer and prints the intern
position at the exit code of success (**ADR 0166**'s worst rung).

Measured beside a `git worktree` build of the parent (`88d7da6`, **ADR 0434**) over the family's **63**
deduplicated sources: **7 moved, 56 held, and all 7 moved sources arrived at the reference's bytes exactly**
(`hi`, `hi world`, `hi world`, `HI`, `2`, `True`, and `YO`/`HI` from one program whose two calls both argue
for the text). Nothing moved that should not have: the controls — a `1` default, an undefaulted parameter,
the module-`def` twin, and every ADR 0434 vector row — are byte-identical across the two builds.

## Decision

**The text census reads the default that fills the slot. The claim is earned, never assumed.**

* `pkg/lang/strargs.go::strArgKinds`'s `seed()` no longer withholds the claim from a method's defaulted
  parameter. A defaulted method parameter is now seeded exactly as a module `def`'s is: an `str`
  annotation, or a default whose shape is a text (`isStringExpr(p.Default)`), marks the slot.
* `w.attrCall`'s tail rule no longer records evidence against a defaulted tail slot whose default IS a text.
  The rule keeps negating a defaulted tail whose default is not a text — that is still evidence, and it is
  the same evidence the seed declines to claim.
* Nothing else about the census was loosened. The negative-evidence roads are untouched, which the new
  reading rows pin: a number default earns no text claim, an undefaulted parameter no call names earns
  nothing, a method name two classes share is dropped as ambiguous rather than marked from either, and a
  call that writes `5` to a defaulted text slot still argues the slot is not a text.
* The claim stays ONE claim per parameter (`map[string]map[int]bool`, keyed by the method's own name under
  **ADR 0366**'s ambiguity rule). This is what leaves the "one call writes a number, the other omits and
  needs the text" program wrong (`5` then `0` for `5` then `hi`) and its sibling refusing: filed as
  **Gap R.481** rather than answered with a per-call claim this census cannot represent. A refusal and a
  pinned wrong number are both in the suite either way; what is not allowed is a silent number.
* The container and float halves of the filed row are NOT touched, because measuring them showed they are
  not defaults questions. `def m(self, xs)` called as `C().m([1, 2, 3])` refuses exactly as the defaulted
  spelling does — `emitClassMethod` has no `declareHeapParams` door at all (that call lives only on the
  module `def` road, `codegen_func.go`), so a method parameter holding a container is never rooted: that is
  **Gap R.480**. And `def m(self, x)` called as `C().m(2.5)` answers `4` for the reference's `5.0` with or
  without the default — the method's missing double return word, **Gap R.35** / **L11.6**, **ADR 0196**.

## Consequences

* One sentence describes every defaulted parameter in the language. The method road and the `def` road read
  ONE table, seeded by ONE function; the difference left between them is only that a method's slot 0 belongs
  to the receiver (**ADR 0366**), and the ADR-0344 fill is what made the sentence true rather than hopeful.
* The intern position can no longer be printed for a defaulted text parameter. The four roads that print it
  (the f-string field, `%`-formatting, the repeat, and a bare `return` of the parameter through
  `methodReturnsStr`) are reached by the claim, so the fix had to be in the census and not at the printing
  door: patching the printer would have left `len`, `==`, `in` and the char methods asking their
  non-text roads about a text.
* Two programs that answered before and now refuse: none, and the sweep says the held set is byte-identical.
  What DOES refuse, and did before this ADR too, is one slot written a number by one call and a text by
  another (`n.both(5)` / `n.both("hi")`), which refuses with **ADR 0366**'s sentence
  `a text argument to method "both" has no compiled word: … pass texts at every call site`. That refusal is
  honest and is pinned as **Gap R.481**; the conformance program keeps only the shapes both legs answer.
* `--capabilities` grows no entry. The claims here are not capability-shaped refusals; the refusals that
  remain are **ADR 0366**'s existing sentence, unchanged.
* The record leg grew: `pkg/lang/testdata/interpreter-golden.json` took **+27** sources
  (**9851 → 9878**), every one written by `tools/recmerge` from the reference with the compiled leg
  cross-checked, none hand-authored and none refused. `tools/recmerge` REFUSED two sources on the way —
  the ambiguous-method-name shape (`0` where the reference prints `hi`) and the constructor program whose
  required parameter the program never passes — which is the tool's cross-check doing its job, and each
  refusal is a row: the first is **Gap R.482**, the second is **ADR 0434**'s refused-shape row.

## Alternatives rejected

* **Keep the withholding and mark the slot only when some call writes a text.** Rejected: that is today's
  behaviour, and it is a wrong answer at exit 0 for the shape the row was filed with. The withholding was
  written for a slot that was never written; **ADR 0434** changed the fact, and a guard written against an
  old fact is a bug with a paper trail.
* **Re-mark the seed but decode the slot lazily, at each door.** Rejected: the doors ask `internedVars` of a
  NAME, and a name's kind has to be settled before the body is emitted — the same reason **ADR 0366** made
  the arm authoritative rather than additive (`delete` in the else branch). A door-by-door guess would
  reproduce the split this cycle exists to close.
* **Add per-call text claims (a slot is a text for the call that omits and not for the call that writes a
  number).** Rejected for this commit: `strParamOf` is consulted while emitting a BODY, which one call site
  does not own, and monomorphising the method per call shape is the method-round's decision, not a census
  patch. Filed as **Gap R.481** with today's bytes pinned in both suites.
* **Give `emitClassMethod` a `declareHeapParams` door in the same commit.** Rejected as a second feature:
  it roots a container in the GC inside a method body, which is the heap-argument machinery
  (**ADR 0098**/**ADR 0187**) arriving at a road that never had it, and the written-argument control shows
  it is not a defaults bug. Filed as **Gap R.480**.
* **Take the float default's `4` as a census miss too.** Rejected on measurement: the written-argument
  spelling answers `4` as well, so what is missing is the method's double return word — **Gap R.35**, whose
  row already owns it. A row may not close a defect it did not measure.
* **Edit the ADR-0366 comment rather than the two suppressions.** Rejected: the comment is the reason the
  suppressions exist, and deleting the code without telling the story would leave the next reader restoring
  it. Both now say which fact changed.

## Machine path

* The claim is readable without a compile: `strArgKinds(prog)` is the table, and
  `pkg/lang/method_args_test.go::TestTheTextCensusReadsTheDefaultTheVectorFills` asks it DIRECTLY over
  **7** programs — the claimed text default, the module-`def` twin, a `1` default, an undefaulted parameter,
  a call that writes a number, a call that writes a text, and the two-classes-share-a-name ambiguity — with
  the expected verdict named per row rather than inferred from a printed string.
* `integration/programs/method_default_kinds.gy` NEW is the matrix program (row
  `programs/method_default_kinds`, `asserted`, oracle `match`): **14** lines byte-identical to CPython,
  covering the returned default, the f-string field, the concatenation, the char method, `len`, an `==`
  against the default's own text, one slot claimed by two calls that agree, and the number-default controls.
* Tests: `pkg/lang/method_args_test.go` (**19** answered programs asserted on the compiled leg AND against a
  live `python3` in the same case, **7** census reading rows, **5** filed-not-fixed rows with today's bytes
  beside the reference's, **5** refusal rows), `pkg/lang/method_text_test.go` (the `class C:` spelling of the
  concatenation moves from the refusal table to the answered one, where the guard is that it ANSWERS),
  `integration/method_args_test.go` (**22** CLI programs each re-derived from a live `python3` inside the
  case, **6** exit-code rows with exit **2** forbidden and the reference's exit code beside each, **4** CLI
  owed rows, **1** assembler-rejection pin for **Gap R.474**, **1** corpus/matrix check),
  `pkg/lang/testdata/interpreter-golden.json` **+27** sources (**9851 → 9878**) recorded by
  `tools/recmerge` with the compiled leg cross-checked.
* Nothing new is machine-visible that was not already: the refusals stay ordinary codegen diagnostics at
  exit **1** (`gustyc --json` `phase: "codegen"`, `ok: false`), the assembler rejection a container default
  reaches stays **exit 2** and is pinned as **Gap R.474**'s, and no capability entry changes.
