# A compiled function body reads the module's float out of the module's own global

**Roadmap:** Gap R.35 (the float half; the container half stays open) · ADR 0227 (the module-global
mechanism this extends) · ADR 0375 (the per-body float table this must not merge with) · ADR 0373 (which
write a float read answers) · ADR 0196 (the return word a body is emitted with) · ADR 0166 (why nothing
malformed is ever emitted) · ADR 0351 (one name, one slot) · ADR 0220 (a body that assigns a name owns it)

## Status

Accepted. Landed 2026-10-19.

## Context

ADR 0227 settled how a compiled body touches state that outlives its frame: the name lives in a module
**global** that `@main` writes and any body loads, because a frame's allocas die with the frame and module
state does not. `moduleSlotNames` chooses the names that get one, and `@gy_mod_<NAME>` has been the answer
for `i32`-worded names ever since.

A float was left out on purpose — the table's word is an `i32`, and a double does not fit it. The result
was a refusal at exit 1 for a program CPython runs:

```
RATE = 2.5
def scale(v):
    return v * RATE
print(scale(3))        # CPython 7.5 · this toolchain, before this ADR: exit 1, "cannot read module-level state"
```

That refusal was the right *shape* for a construct the backend cannot lower and the wrong *sentence* for
this one. By ADR 0375 the double already had everything an `i32` had and more: a word (`double`), a slot
type (`alloca double`), a printer (`rt_fmt_double`), a return convention (`define double`), and a
per-body table that says which of a body's names hold one. What was missing was the module's half of the
pair — a place for the number to live that is not a frame.

## Decision

**A module-level float that a body reads gets a module global of its own: `@gy_mod_<NAME> = global
double`, written by `@main`, loaded by the body.** It is the float sibling of ADR 0227's slot, in a new
file (`pkg/lang/module_float.go`) and a table of its own (`g.moduleFloatSlots`), and the two tables never
share an entry.

The gate is the whole safety of the feature, because a global has one word and the write that fills it is
the literal the gate can see. A name enters the table only when all of:

1. some function, method or closure body **reads** it without **binding** it — the scope question is
   ADR 0227's `moduleBodyReads`, now shared by both doors rather than duplicated for the float case;
2. it is not a folded module constant (a name bound once to a literal is a value, and ADR 0227 folds it —
   paying for a global would be paying for nothing);
3. **every** module-level binding of it is an answer in the double the gate can write itself: a float
   literal, a negated one, or arithmetic over literal numbers (`RATE = 7 / 2`, `AREA = 2.0 * 3`) —
   including the module doubling its own previous value (`RATE = RATE * 2`), which `@main` replays in
   source order;
4. nothing else ever binds it: no `for` target, `with ... as`, `match` capture, unpacking or `import`
   touches the name;
5. no body that reads it also writes it, and no parameter carries the name.

Anything failing the gate keeps ADR 0227's existing refusal, unchanged, which is why this ADR can close a
half of a row without widening what the compiler ever claims about a name it cannot read.

**Reads and writes are separate roads, and the float road is the only one that loads.** `isFloat`'s name
arm consults the body's own table first (ADR 0375's rule — *whose* writes the body reads is answered by
that body and nothing else) and only then the module's table. `floatValue` loads the global. The **i32**
road (`valueName`) refuses the name outright: a slot, `select` or `ret` typed `i32` cannot carry a double,
and emitting the load anyway hands `llc` a `mul i32 %p1, <double>` — exit 2, the code the exit-code
contract reserves for a compiler bug. So the same name is answered by one road and refused by the other,
by the word each road has rather than by a second opinion about the program.

A method is refused before its header is written (`emitClassMethod`), for the reason ADR 0196 gives a
method that returns a rebound parameter: a method is emitted `define i32` whatever its body computes, so
the double has no word to travel in. A nested `def` is refused by the i32 road's sentence. Both name the
body that lacks the word, because the identical expression in a top-level `def` — which this ADR does
promote to `define double` — compiles.

## Alternatives rejected

- **Seed the body's float table from the module's.** One line, and it breaks ADR 0375 immediately: the
  body's table answers *what did this body write*, and a module entry in it would make every leaked-local
  defect come back with a global attached. `TestTheModuleFloatDoorDoesNotAnswerWhatTheFrameAnswers` is the
  case that would go quietly wrong — the same name in the same program answers two numbers.
- **Box the module's floats as heap objects (a tagged handle in an `i32` global).** Honest about the word,
  but it puts a per-read allocation on a value whose bits the compiler has in hand, roots a handle in every
  frame that reads it, and re-solves a representation question ADR 0373 already settled for floats that
  live in slots. The `double` word exists; using it is what the rest of the float family already expects.
- **Widen `moduleSlots` to a tagged pair (kind + word).** A global whose type is only known at the load
  site is how two roads end up with different opinions about one name, which is the defect class ADR 0273
  exists to stop. Two tables with disjoint domains say the same thing in a way a reader can check.
- **Answer the i32 positions by truncating** (`mul i32 %p1, %mfl1` after a `fptosi`). Silent wrong numbers
  at exit 0 — ADR 0166's definition of this compiler's bug, and what the pre-this-ADR build did for a
  neighbouring shape.
- **Include containers in the same commit.** A container's word is a heap handle that has to be rooted
  against the collector, and its element operations come from the per-scope notebooks ADR 0341 settles at
  a *binding*; a global handle supplies none of that. Refusing it, in the same file and the same table as
  the answers, is what keeps "Gap R.35 is half done" from being read as "done".

## Consequences

- `pkg/lang/module_float.go` owns the gate, the declarations, the load and the store;
  `codegen_module.go`'s `moduleBodyReads` is now asked once and shared by both doors (ADR 0273's one
  opinion, measured at two calls and one implementation).
- `codegen_value.go`'s `valueName` gained a refusal it did not have. That is the deliberate shape of this
  ADR: more programs are refused, none is answered wrongly, and every refusal names its construct.
- `TestTheFrameRuleIsNotAnAnswerToTheModuleStateQuestion` loses the two rows that pinned the module float
  as a refusal — they moved into the *answer* table, which is how the guard knew.
- Two defects surfaced while writing the tables and are filed rather than absorbed, both reproducing
  unchanged on the pre-ADR build: **Gap R.343** (a module-level *text* read by a body answers the intern
  position — ADR 0227's `i32` was never built for an interned index) and **Gap R.344** (printing a
  float-returning call whose argument arrives as a double emits `store i32` of a `double` and `llc`
  rejects it at exit 2). The gate in this ADR does not touch either; the tests pin today's answer with the
  reference's beside it.
- The record is **7929** sources and the conformance matrix **216** rows / **184** asserted / 0 failures /
  0 drift, with `integration/programs/module_float_read.gy` and `module_float_state.gy` as the two new
  reference-leg programs.
