# ADR 0447 — a value the program's own class renders answers `print`, `str()` and `repr()` before the value-form table

- **Status**: Accepted
- **Date**: 2026-10-29
- **Cycle**: the one after **ADR 0446** (which closed **Gap R.56** + **Gap R.66**; row **L12.3** was standing in the Open queue)
- **Roadmap**: row **L12.3** done, paying the row it owns — **Gap R.55** (`print` ignores `__str__` / `__repr__`).
  **Gaps R.508**, **R.509** and **R.510** are filed here as the residue the sweep found; **Gap R.431** and **Gap R.19**
  stay open and are named by the doors this ADR leaves standing.
- **Builds measured**: parent `c0972a4` (**ADR 0446**) and this commit, every byte from a build made with the gate's
  `-tags=llvm20`.

## Context

`Gap R.55` was filed by the surface survey as "an instance prints `<instance>` interpreted and `0` compiled". The
compiled half is the worse one, and the number is the tell: `0` is an **interned-string index rendered with `%d`** —
**Gap R.22**'s signature arriving through the printer instead of a return. Measured on the parent build:

```
$ gustyc --eval 'class C:
    def __str__(self):
        return "S"
    def __repr__(self):
        return "R"
print(C())'
0                                                reference: S      ← exit 0, the answer class this repo ranks worst

$ gustyc --eval 'class C:
    def __str__(self):
        return "S"
print(str(C()))'
gustyc: codegen: str() of this value is refused: no form …        reference: S

$ gustyc --eval 'class C:
    def __repr__(self):
        return "R"
print([C()])'
[<__main__.C object at 0x0>]                     reference: [R]

$ gustyc --eval 'class C:
    def __str__(self):
        return "S"
print(f"v={C()}")'
v=0                                              reference: v=S
```

Four spellings of ONE question — `print(v)`, `str(v)`, `print([v])`, `print(f"{v}")` — and each had its own wrong
answer, because each had its own renderer. The reference has one rule and two dunders: `str()` asks `__str__`, and
`object.__str__` hands the question to `__repr__` when the class binds no `__str__` of its own; `repr()`, and every
container element, asks `__repr__` and nothing else. Getting that split wrong is not cosmetic: `print(v)` for a class
binding only `__repr__` prints `R` in the reference, and `print([v])` for a class binding only `__str__` prints the
**identity** form — a door that consulted `__str__` for container elements would print `R` there, quoting an answer
the reference never asks the class for.

## Decision

**One door per question, keyed by the object, emitted only for the program that asks.**

1. **Two doors, not one flag.** `classRenderStr` answers the *str* question (`print`, `str()`, an f-string field,
   `!s`): `__str__`, then `__repr__`, then the identity form. `classRenderRepr` answers the *repr* question (`repr()`,
   every container element, `!r`): `__repr__`, then the identity form — **never** `__str__`. Both find the member
   through **ADR 0446**'s C3 linearization, so a dunder a base owns is found on the base that owns it and an override
   wins.
2. **The door is keyed by the object, not the expression.** A class may bind `__str__` and still be printed through a
   name the program rebound, or as one element of a container that mixes it with numbers and texts. So the runtime
   helper reads the class id out of the instance's **own first slot** (`@rt_inst_get(h, 0)`, the slot **ADR 0325**'s
   constructor writes) and indexes a per-class row with it. `print(C())` is the easy case; `[1, c, "t"]` where `c` was
   bound two statements ago is the case the key has to survive. The positions that arrive with NO kind word at all — a
   parameter that received an instance, the loop variable of `[C(), C()]` — are waiting on the binding's tag, not on
   this table: the door reads the class off the object precisely so it will not care which position a handle came from,
   and it is filed as **Gap R.509** rather than described as answered.
3. **The value-form table is consulted first, and this backend's ONE printer is the thing called.** A render row
   calls `rt_print_mixed_value` — the same tag-reading renderer `str()` and the container walk already share (**ADR
   0258**, **ADR 0303**) — rather than a renderer the render road wrote for itself. `ADR 0258`'s "one printer" is
   guarded: a module defines it exactly once, with the render road in place or without it.
4. **A door is a permission, so the row is installed only on a proof.** `renderAnswerKind` reads a dunder body and
   returns a tag only when the body's answer is provable: a literal, an interpolation, a concatenation or repetition
   of texts, a verdict, a number, a container, or a plain call whose returns are all provable. Provable-and-text
   installs a live row. Provable-and-not-text installs a row that RAISES with the reference's own sentence
   (`__str__ returned non-string (type int)`, `__repr__ returned non-string (type NoneType)`, …) at the reference's
   exit class. Unprovable keeps the row **dead** and the identity form answers — which is ADR 0446's rule "a gate is
   permission, not a hint" asked of a renderer: a body the compiler cannot read does not get to invent an answer, and
   it does not get to print a number either.
5. **Rendering is a CALL, so it carries a frame and it raises.** A raise out of a dunder is the reference's raise:
   `rt_frame_push` names the method (`__str__`) with the line its `def` sits on, `rt_frames_restore` puts the module's
   frames back after the call, and the standing `@exn_flag` check reports the raise with the traceback the reference
   would have printed. A container walk that sees a raise STOPS — the entries after it and the brace that would close
   them are not written — because printing a container the program never finished building is a second wrong answer
   stacked on the first.
6. **File then pay.** `programAsksToBeRendered` gates the whole emission: no class in the program binds a
   `__str__`/`__repr__`, and the module carries no table, no indirect call and no frame push. The 266-program corpus
   is the witness — byte-identical parent to child, `0 moved` — and a module-shape test asserts the ABSENCE of the
   table for a program whose classes say nothing, beside its presence for one that asks.

## Consequences

- The identity form **ADR 0325** built stays what answers a class that binds nothing. The render road is a door in
  front of it, not a replacement: `class C: pass` still prints `<__main__.C object at 0x…>`, and the oracle's own
  address rule (`oracle.go`'s `instanceIdentityRe`) is what lets that row be judged at all — the address is an
  allocation accident, and a test that pinned it would be pinning the allocator.
- Every value road that can reach an instance now asks the same walk. **ADR 0446** established that a value road and
  a kind road are two roads; this cycle applied it to `print`'s mixed arm, `str()`, `repr()`, the f-string renderer
  (`fstringNeedsRenderer` now says yes to an instance field, which is how `v=0` became `v=S`) and the container walk.
- A container's elements are rendered with the quote flag ON, exactly as `str()` renders a top-level value with it
  OFF, so the same text answers `print(x)` and `print([x])` with the quoting the reference applies.
- The emitted IR is verified: `llc-20` and `llvm-as-20` accept every module the corpus and the new rows emit
  (269 modules, 0 verifier failures), and two traps the first draft sprang — an inline `getelementptr` whose index is
  a function-local register (a use that does not post-dominate its definition), and a `phi` naming a block that does
  not branch to it — are recorded here because both are the shape of the runtime, not of the language.
- `render.go`'s pair path now lowers an instance-bearing pair to `rt_str_of_value(…, TagInstance, quote)` instead of
  refusing when the module has no `rt_str_of_value`, which is what `str()` already used: the render road and the
  builtin share the renderer, and the instance pair's tag can reach the one printer that reads it.

## Alternatives rejected

- **One door that always asks `__str__`, both questions alike.** Rejected: `print([C()])` for a class binding only
  `__str__` prints the identity form in the reference, and this door would print the `__str__` answer. The two
  questions are two, and the fallthrough from `__str__` to `__repr__` belongs to the str side only.
- **Keying the table by the expression's static class** (`g.receiverClassOf(expr)`). Rejected: it is right for
  `print(C())` and wrong for every position the expression cannot name — a name bound to an instance, a container
  element, a parameter. The class id lives in the object; the table is indexed by it. Reading it from the AST would
  have re-created **Gap R.37**'s family, where an instance reached through a binding prints the identity form because
  the *expression* forgot what the *value* knows.
- **Rendering `str(dunderResult)` through `printf` beside the surrounding text.** Rejected for the same reason
  **ADR 0258** gives for the float door: a render CALLS the program, so it can raise, so the render must happen before
  the value reaches any printf — and a render written into a format string cannot be re-checked when the render is
  nested inside a container walk. The render writes through the module's one printer instead.
- **Installing a live row for an unreadable dunder body and letting the answer be whatever comes back.** Rejected:
  it is how a program ends up printing `0` for an object — the exact failure this row closes — and a wrong answer that
  replaced the honest identity form would be worse than the row it came to fix (**Gap R.503**'s lesson, asked of a
  renderer).
- **Printing the identity form when the body provably returns a number.** Rejected: the reference does not print
  anything there, it raises `TypeError: __str__ returned non-string (type int)`, and the row's promise is the
  reference's answer rather than a quieter one.
- **Rolling back the bytes a container walk already wrote when a member raises.** Rejected as not available, not as
  unwise: `rt_out_snapshot` does not exist, and **ADR 0258** already states plainly that the float door cannot recall
  what it printed. What this ADR does instead is stop the walk at the raise, and file the residue as **Gap R.508**.

## Where the answer lives

- `pkg/lang/codegen_render.go` — `renderDoor`, `classRenderStr` / `classRenderRepr` (the two questions),
  `programAsksToBeRendered` (the gate), `renderAnswerKind` / `renderLiteralKind` (the proof), `renderNonStringSentence`
  (the reference's own message), `renderTableIR` / `renderDoorBlock` / `instanceRenderIR` (the tables and the door),
  `renderDoorPositions` (the frame a raise is reported from).
- `pkg/lang/codegen_instance.go` — `instanceRuntimeIR` emits the render runtime beside ADR 0325's instance runtime.
- `pkg/lang/codegen_ir_mixed_print.go` — `rt_print_mixed_value`'s instance arm asks the door; the list walk stops at a
  raise.
- `pkg/lang/codegen_ir_tagged_slots.go` — the dict and set walks stop at a raise.
- `pkg/lang/render.go`, `pkg/lang/fstring_value.go`, `pkg/lang/codegen_builtin_print.go` — the three value roads that
  now route an instance to the same door.
- Answers: `pkg/lang/class_render_test.go` (compiled leg + record leg, traps, caught raises, module-shape absence,
  the two doors asked directly, and a filed table with this backend's answer beside the reference's) and
  `integration/programs/class_render.gy` + its `conformanceProbes()` row (the reference leg, byte-identical).
