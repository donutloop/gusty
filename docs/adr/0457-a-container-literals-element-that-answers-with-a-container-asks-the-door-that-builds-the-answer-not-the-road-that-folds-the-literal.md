# A container literal's ELEMENT that answers with a container asks the door that builds the answer, not the road that folds the literal

## Context

`print([1, 2] in [f()])` with `def f(): return [1, 2]` refused, while `print(len([f()]))` — the same literal, one
door away — had always compiled. The refusal read:

```
codegen: list literal elements must be integers, not list[int]
```

a sentence about an element kind the program had already proven, on the one construct (`[e]` where `e` is a call that
hands back a container) that **ADR 0333** proved as a whole-program fact and never handed to the literal's builder.
**ADR 0456** filed it as **Gap R.530**, alongside **Gap R.531** (a refusal that names no kind at all) and the
membership door it shipped.

The reason two answers were possible for one literal is that a container literal is decided by **five** doors and
built by a sixth:

| door | question | `*Call` arm before this ADR |
|---|---|---|
| `literalNeedsHeap` | may the folded compile-time global hold this literal? | none |
| `literalNeedsTags` | does an element need the tagged build path? | none |
| `nestedContainerElem` | is this element a container? | none |
| `elemKindTag` | what tag goes beside this payload? | none (empty-container calls only) |
| `taggableNestedElem` | can this element's slots carry tags? | none (empty-container calls only) |
| `emitList`'s tagged path | build the object | **yes — since ADR 0333** |

Asked narrowly, the first five said "no container here", the literal was routed at the static int-array global, and
the global emitter (`emitGlobalList`) met an element the only road that could have helped — the tagged builder — had
been waiting to be handed. Every one of those five had been given the arm for a *comprehension* (**ADR 0192**), a
*pair* (**ADR 0226**), a *bound container* (**ADR 0438** — the same lesson as a name) and an *empty container call*
(**ADR 0333**), and never for a call whose answer the scan proves. **ADR 0232**'s rule — the slots the builder wrote
are the truth about the object, so one question must be asked once — was the rule broken; **ADR 0249**'s name for the
shape is a gate and a door that disagree.

## Decision

One predicate, asked by all five doors:

```go
func (g *irGen) containerAnswerSlotIsContainer(e Expr) (int32, bool)
func (g *irGen) containerAnswerIsContainerSlotExpr(e Expr) bool   // the gates' reading of the same answer
```

It answers `TagList`/`TagSet`/`TagDict` for a `*Call` whose callee is a name and whose answer the whole-program scan
proves — the callee's builders made the object, the word crossing the `ret` is its handle, and the callee's ONE answer
kind is the tag. It answers nothing, before that, unless:

1. **the callee is a definition the source wrote** (`lookupFuncDef(name).CloneOf == ""`), because an applied
   `lambda`'s handle belongs to the hoisted-lambda machinery that answers its call at the site (**Gap R.536**);
2. **the arguments are carriable** by the container roads — the same question `containerOperand`'s `*Call` arm asks
   before lowering (**ADR 0333**), asked by the deciding door too so a decision cannot outrun the build; and
3. **the returned contents are describable** — each arm the verdict was read from is a `*ListLit`/`*SetLit`/`*DictLit`
   whose elements the door can read, or an empty-container call whose contents tag the runtime itself knows.

The tag is then the CALLEE's kind, never a literal's and never a default: `elemKindTag`'s `*Call` arm used to answer
`TagInt` (falling back to the default) for any call in a slot, and it still does for a call this door declines —
which is why the decline has to be a refusal at the door that builds, and why the three declines below are refusals
with a named owner rather than silent fallbacks.

## The three shapes that were tried, measured wrong, and declined by name

This is the part of the cycle with content in it. Each was implemented, run against CPython, and pulled back:

- **A tuple answer.** Taking `tupleAnswer` as `TagTuple` made `xs = [t()]` / `print(xs)` print `[1]` at exit 0 where
  the reference prints `[(1, 2)]`. The tuple bit is **ADR 0361**'s flag in the object's own `@estr[h]` word; a slot
  that copies the handle and claims `tuple` wears a tag the outer list's printer has no road that reads through.
  Declined → **Gap R.533**.
- **A forwarded answer.** `def f(): return inner()` proved as a container answer with `inner`'s kind, so the first
  draft asked `taggableNestedElem` of the forwarded call and let the element through. `print([f(), 7])` built a list
  whose slot 0 pointed at the list itself and printed `[` until the process died; `print([1, 2] in [f()])` answered
  `False`. A hand-off spells no contents where the tag is read from (**ADR 0333**'s forwarding road owns that
  handle). Declined → **Gap R.533**.
- **An applied `lambda`'s answer.** `print((lambda: [(lambda: [3, 4])(), 1])())` printed `[` × thousands and died on
  signal 11: the hoisted-lambda pass is already answering that call at the site, and a second road storing the same
  word builds an object that contains itself. Declined behind the `CloneOf` test → **Gap R.536**.

**ADR 0166** ranks a wrong answer at exit 0 above any refusal, and **ADR 0232** makes a slot tag a build-time claim
that may only be made about what the door can read. A decline that names its owner is the answer; a half-open door is
the bug.

Two shapes surfaced beside the row and are NOT this row's, both re-measured against the parent commit's binary so
they are filed rather than confessed: a `def` nested in a `def` still emits `ret i32 @.lst1` and `llc` refuses the
module (**Gap R.535**, exit 2 — the compiler's own class), and the count fold does not perform a literal element's
effects, so `print(len([probe()]))` swallows the callee's `print("built")` (**Gap R.534**).

## Consequences

- `print([1, 2] in [f()])` is `True` and `print(1 in [f()])` is `False`, where both were refusals; `print([f()])` is
  `[[1, 2]]`, `[f(), 3]` is `[[1, 2], 3]`, `[f()] == [[1, 2]]` is `True`; a set answer, a dict answer, a method's
  answer (`print([m.f()])` is `[[1, 2]]`), a comprehension's element, a binding, a condition, a `while` test, a `for`
  iterable and a module name read from a function body all answer the reference's bytes.
- Eight rows in four files — `pkg/lang/empty_container_tag_test.go`, `pkg/lang/container_return_test.go`,
  `pkg/lang/refusal_names_the_missing_rule_test.go`, `pkg/lang/lambda_answer_test.go` — that had pinned these shapes
  as **Gap R.67**'s refusals moved into the answered family rather than being deleted; the lambda half of the parity
  row stayed a refusal, so the parity row was deleted with its claim written out instead of left standing.
- `x in [1, 2, 3]` still costs **nothing**: no `@rt_alloc(`, no `@rt_set_elem(`, no `@rt_tag_elem(`, no
  `@rt_contains`, no scan. The fold's fast path is guarded by a row that fails if `@main` grows a builder, because the
  way to widen a gate is to quietly make the cheap case expensive (**ADR 0343**).
- The object is built once. `[f()]` printed and `[f()]` asked for membership emit the same `@rt_alloc(`/
  `@rt_set_elem(`/`@rt_tag_elem(` counts and the same single `@rt_contains_tagged` — a count in the module, not a
  reading of the code (**ADR 0456**'s lesson, applied here on the first day).
- **23 record entries** of this row's own went into `pkg/lang/testdata/interpreter-golden.json` from the reference
  with the compiled leg cross-checked (the artifact 10482 → **10505**), and the file's counts in `roadmap.md`,
  `docs/operations.md` and `AGENTS.md` moved with it, because **Gap R.204**'s guards recompute them.

## Machine path

Nothing new is *discoverable* — this is a widening of an existing door — so the machine-facing requirement is that
the answer stay measurable where an agent looks:

- the record leg covers every answered shape (`evalGolden`/`runGoldenStdout`), and a missing entry fails the case;
- the reference leg runs `python3` inside the CLI cases, so an expectation is read out of the reference rather than
  transcribed (`integration/membership_literal_test.go`);
- the IR claims are counts (`@rt_alloc(`/`@rt_set_elem(`/`@rt_tag_elem(`/`@rt_contains`), the form an agent can diff;
- `GUSTY_RECORD_CONTAINER_ELEMENT=<file>` dumps this file's own sources for `tools/recmerge`, and is scrubbed from
  every subprocess the suite starts (**Gap R.410** / **ADR 0409**).

## Alternatives rejected

- **Widen `elemKindTag` alone.** Tried first: the tag arrived while `literalNeedsTags`/`literalNeedsHeap` still said
  "no tags needed", so the literal went to the folded global, the global emitter refused the element, and the only
  change was which door said no. This is the cycle's actual finding: a gate and its door are one question.
- **Let the gates open and leave `elemKindTag` alone.** The literal built through the tagged path with an `int` tag
  beside a heap handle, and `print([f()])` answered the allocator's slot number — the wrong answer at the exit class
  of success.
- **Take every answer the scan proves**, tuples and forwards and hoisted lambdas included. Three measured wrong
  answers, one of them a segfault. Narrow beats sorry, and the declines are rows with pins.
- **A second literal road for container-answer elements** (a private "build the answer myself" path). A second
  builder is a second truth: it would tag differently from the door that prints, and **ADR 0189**/**ADR 0232** settled
  what an untagged or differently-tagged slot costs.
- **Refuse everything under `in` that is not a spelled literal** (the cheapest green option, since
  **ADR 0456**'s fold already handles the spelled cases). It keeps the answer in the road that folds, which is the
  reading **ADR 0456** already rejected for the haystack, and it would have left `print([f()])` — never a membership
  question — refused.

## Validation

```
go vet -tags=llvm20 ./pkg/lang/ ./integration/                                    clean
gofmt -l pkg/lang/ cmd/gustyc/ integration/                                          clean
go test -tags=llvm20 -count=1 -parallel $(nproc) -timeout 90m ./...    # `make testparallel`
  ok  github.com/donutloop/gusty/cmd/gustyc      71.2s
  ok  github.com/donutloop/gusty/integration    121.6s
  ok  github.com/donutloop/gusty/pkg/lang       250.7s
  ok  github.com/donutloop/gusty/tools/{parify,recmerge,testshards}
```

The two claims that could have been shipped as readings of the code are counts in the emitted module: printing
`[f()]` and asking membership of `[f()]` emit the same `@rt_alloc(`/`@rt_set_elem(`/`@rt_tag_elem(` counts and ask the
tagged scan exactly once, and `x in [1, 2, 3]` emits none of them at all. **Gap R.535**'s `llc` failure and
**Gap R.534**'s swallowed effect were both re-measured against a binary built from `HEAD` before the first edit of this
cycle (`/tmp/gustyc-base`), which is what makes them filed rows rather than this ADR's regressions.
