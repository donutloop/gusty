# ADR 0366 — a method is a callable, so its parameter and its answer carry their kind

**Family:** `codegen` · **Date:** 2026-10-16 · **Status:** Accepted · **Roadmap:** Gap R.315 (closed), Gap R.316 (closed), Gap R.317 (closed), Gap R.318 (closed), Gap R.319 (opened), Gap R.320 (opened) · **Implements:** L11.1 (what a callable hands back is a fact about the callable), L15.4 (a verdict is rendered as a verdict) · **Co-:** ADR 0365 (the sequence door whose repeat this row needed registered), ADR 0360 (the formatting door whose answer this row needed registered), ADR 0362 (the case table `s.upper()` belongs to), ADR 0257 + ADR 0254 (a verdict a function hands back prints as a verdict; a callee's body is read, not guessed), ADR 0111/Gap R.111 (the verdict census has read methods since that row), ADR 0229 (a text is an `@str_tab` index and every road asks one table), ADR 0299 (a census is only honest when every road asks it), ADR 0186 (a probe becomes a parity row when the compiled leg agrees), ADR 0273 (a door that decides one shape stays silent about the rest — declined here) · **Counter:** ADR 0290 (pin a wrong answer beside the reference's) — kept for the two shapes this row leaves owed · **Extends:** ADR 0166 (an unsupported lowering is a diagnostic, never an invalid module, never a crash), ADR 0203's module-state rule (a function body cannot read module state it is not allowed to see) · **Supersedes:** — · **Tier:** ladder, core (a method is the most ordinary way a program is shaped) · **Schema:** `--emit-llvm`, `--verify`, `--json` unchanged — no field; the census is internal to codegen.

## Context

Four programs, measured while widening ADR 0365's one text predicate, and none of them was a refusal:

```gusty
class C:
    def fmt(self, s):   return "x%s" % s      # compiled printed  x0     (reference: xhi)
    def rep(self, s):   return s * 2           # compiled printed  0      (reference: hihi)
    def yes(self):      return 1 == 1          # compiled printed  1      (reference: True)
    def upper(self, s): return s.upper()       # compiled printed nothing, and died by SIGSEGV
```

The first two are **Gap R.315**, the third **Gap R.316**, the fourth **Gap R.317**. All four print a value at
**exit 0** (or in the fourth's case, print nothing and take the process down), and in every one of them the
function twin — the same body written as a `def` — was already right. That pairing is the diagnosis: this
backend decides what a callable's slot holds, and what its answer is, from censuses keyed by a callable's
*name*, and those censuses read `def`s. A method never entered them.

The consequence is mechanical. A text value is an index into `@str_tab` (ADR 0224/0229); the roads that print,
measure, compare and format one ask `internedVars` of a name. A parameter the method road never registered is
an `i32` to those roads, so the body prints the **position** under the text. The same shape one door down: a
verdict is the 0/1 a comparison answers with, and `print` picks its renderer by asking whether the callee's
body returns only verdicts — a question `callReturnsBool` asked of a `*Name` callee and never of an `*Attr`
one. And the dispatch road resolved `s.upper()` by the *method name* alone: the enclosing class declares
`upper`, so the body's call became a call of the body, with the text as its receiver, forever.

While measuring, a fourth defect surfaced on the same road. `b = Bag(True)` / `def outer(): print(b.get_flag())`
— a module instance read from inside a `def` — emitted

```llvm
  %t7 = call %obj @rt_mkobj(i32 10, i32 )
```

because the dynamic-dispatch road wrote `h, _ := g.value(b, attr.Obj)` and continued with an empty operand
(**Gap R.318**, exit 2), while the neighbouring road two lines away already had the truthful sentence that
program deserves (exit 1, Gap R.35's module state).

## Decision

**A method is a callable. Wherever the compiler asks a question of a `def`, it asks the same question of a
method, and one predicate — not a second one — decides each answer.**

1. **The parameter.** `strArgKinds` (`pkg/lang/strargs.go`) now walks the arguments a program hands
   `obj.m(...)` as well as the arguments it hands `f(...)`, through the same `claimArgs`, with the only
   difference the census honours: a method's first parameter belongs to the receiver. A constructor gets its
   own key (`class ⟂ __init__`), because `__init__` is the one method name every class declares and a
   name-keyed table would let one class's call sites classify another's. `emitClassMethod` then writes the
   classified parameter into `internedVars` — the authoritative write, not an addition — exactly as `funcDef`
   has since Gap J.5, and the method-side return census (`methodReturnsStr`, `returnsStringExpr`) reads the
   same table so `return s * 2` is known, by the caller, to be a text.
2. **The answer.** `pkg/lang/boolmethod.go` is the method-side reading of the program, written beside the text
   one: which method **one callable** owns, which class a receiver hands back (`C(...)`, and a module name
   bound exactly once to such a call), and what **every write** of a field hands the program. `BoolEnv` grew
   `MethodLookup`, `SelfLookup`, `SelfClass` and `FieldVerdict`, and the attribute branch of `callReturnsBool`
   now opens the callee's body the way the name branch does. `self.helper()` resolves against the class the
   *body* belongs to — which only the generator knows, hence `irGen.curClassSrc`, set for the duration of
   `emitClassMethod`.
3. **The dispatch.** `callMethod` asks whether the receiver is provably a text **before** the class-method and
   dynamic-dispatch roads, and skips both for a text. A text is never an instance, so neither road can be
   right for it; skipping them can only turn nonsense into the text's own answer or a refusal in words.
4. **Errors are propagated, not dropped.** The dispatch road returns the error its receiver or argument
   produced. An unbuildable operand is a refusal at exit 1 that names the missing half; it is never a module
   `llc` rejects.
5. **Three bounds keep the censuses honest.** A name more than one callable owns is classified as **neither**
   (one key, two callables, and one call site would dress the other's number in the string table). A tail
   position a call leaves to its **default** is evidence *against* being a text — the call road passes only the
   arguments the source wrote, so an interned default nothing interned was a SIGSEGV. And a text handed to a
   parameter the call sites never agreed is a **refusal** (`refuseUnmarkedMethodText`), which is the method road
   catching up with the `strArgCheck` the function road has had since Gap J.5.

## Consequences

- `integration/programs/probe_method_text_answer.gy` and `probe_method_verdict_answer.gy` and
  `probe_method_shadows_text_method.gy` went from pinned-debt rows to byte-identical `match` rows (ADR 0186's
  promotion), and the record leg grew 7739 → 7741 for the sources no second engine ever answered before.
- The ladder's ordering is what made this one cycle rather than four: a wrong value at exit 0 is the outcome
  the roadmap ranks worst, so the verdict row could not be closed without the text row's predicate, and the
  dispatch row could not be closed without the crash.
- Two shapes are **filed, not smoothed over**. A text held in a *field* (`return self.v`) still prints the
  intern position — the field census ADR 0366 writes for verdicts has no text twin (**Gap R.319**, pinned in
  `integration/programs/probe_method_text_field.gy` with today's `0`/`1` beside the reference's `hi`/`yo`).
  A defaulted method parameter and a `self.other()` call still refuse (**Gap R.320**, pinned as refusals that
  must not reach the verifier).
- `TestWhatTheMethodAnswerOnceOwedNowAnswers` holds the paid rows so they cannot regress;
  `TestWhatTheMethodAnswerStillOwesIsFiledNotFixed` holds the owed ones so a future cycle finds a row, not a
  silence.

## Alternatives rejected

- **A second text predicate for the method road.** Two predicates is how the formatting door and the repeat
  door drifted apart under ADR 0365; the same split here would let a method's parameter be a text to the print
  road and a number to the repeat road. One predicate, asked by every road (ADR 0299).
- **Classifying a method from the module function that shares its name.** The table is keyed by name; marking
  one callable from the other's call site is exactly how a number gets dressed in the string table. The
  conservative answer is neither, and the neighbouring refusals (function road, method road) stay honest.
- **Answering the field-text family with a heuristic** (a field is a text if the class ever assigns a literal to
  it) — declined: `self.v = 3` in one method would then print a text's position for another method's reader.
  The rule has to be *every write of the name, or no claim*, which is what the verdict half implements and
  Gap R.319 owes.
- **Letting the dispatch fall through to the recursive call and fixing only the crash** — declined: the
  artifact-fault sentence (Gap R.203) is a report of our bug, not an answer, and ADR 0166 reserves exit 2 for
  it.
