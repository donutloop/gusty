# ADR 0333: a container returned from a call is a value

Status: accepted (2026-10-14)
Amends: [ADR 0163](0163-a-container-binding-is-materialised-into-the-heap.md) (a literal binding is materialised — this is the same rule at a `return`), [ADR 0188](0188-a-global-array-name-never-escapes-its-own-instruction.md) (a global array name never sits in a value position), [ADR 0249](0249-a-gate-and-a-door-that-test-different-things-ship-a-refusal-nobody-measured.md) (a gate must be true of everything the door asks)
Extends: [ADR 0232](0232-mixed-dicts-and-sets-answer-by-the-slots-they-carry.md) (the per-slot tag), [ADR 0258](0258-str-and-repr-are-one-pair-per-backend.md) (the one renderer this answer is printed by), [ADR 0331](0331-a-list-literal-is-read-by-the-door-a-list-elements-are-read-by.md)/[ADR 0332](0332-a-literals-element-that-is-itself-a-read-is-stored-as-the-pair-that-read-answers.md) (the element roads a returned container is built from)
Supersedes: nothing
Related: roadmap L11.1, Gap R.67 (closes), Gap R.214 (unblocked: the answer now arrives at the subscript door), Gap R.228 / R.229 (newly measured, filed here), Gap R.35, R.80, R.115, R.154, R.175 (the doors left standing, each named below)
Owner: `pkg/lang/container_return.go`, `pkg/lang/heapargs.go`

## Context

Three shapes of one defect, all measured on 2026-10-13 while closing Gap R.224, and all of them the same
missing fact: **a function that hands back a container had no answer**.

```
def build():                       def build():                      def build():
    return [1, 2]                      xs = []                           d = {}
                                         xs.append(1)                      d["a"] = "x"
print(build())                         return xs                       return d
# llc: global variable reference   print(build())                    print(build()["a"])
#        must have pointer type     # 0 — the handle, printed        # 0 — CPython: x
# gustyc exit 2
```

The first is the worst class the contract recognises. `return [1, 2]` lowered the literal to its
compile-time global — the same `@.lst1` that a *binding* materialises into the heap (ADR 0163) and that
ADR 0188 forbids in a value position — and put it in the `ret`:

```
@.lst1 = private unnamed_addr constant [2 x i32] [...]
define i32 @gy_build() { ret i32 @.lst1 }        ; llc: global variable reference must have pointer type
```

`llc-20` rejected the module, `gustyc` printed `LLVM rejected the module; this is a compiler bug, not a
source error` and exited **2** — the code the exit-code contract reserves for a broken compiler — on
`def build(): return [1, 2]`, a two-line program CPython answers with `[1, 2]`. Same for `{1, 2}`, for
`{1: 2}`, for the empty `[]` and `{}`, for a nested `[[1, 2], [3]]`, and for a computed `[n, n + 1]`.

The second and third shapes built a module and were *wrong at exit 0*, which the queue ranks above any
refusal: a run-time-built container's variable already holds a heap handle, so `return xs` compiled, and
then nothing in the caller knew what the word meant. `print(build())` wrote `0` — the handle as a number.
A `for` over the answer printed nothing at all. `if build():` was false for a two-element list. `str` and
`repr` wrote digits. `not build()` was `True`. The three roads that had an answer for `ys = build()` —
print, loop, truthiness — ask a *name*, and the call was not a name.

The fix has to be a **record**, not a peephole: the caller holds one word and a function's name, and it
cannot see the body. That is the same shape as `strFuncs` (ADR 0265) and `pairRetDone` (ADR 0309), which
is why this table is a whole-program scan. It is also where those tables' worst lesson lives: Gap R.219
found `strFuncs` registering a function as text-returning on the evidence of **one** arm, so a caller of
the arm that returns a number printed whatever byte string shared that interned index. A record that opens
a door must be true of **every** road through the body, and it must be asked of the program rather than of
the emission order (ADR 0273's rule for the pair road's arity, applied one construct over).

## Decision

**A container returned from a call is a value: the `return` materialises it, and the call site reads the
answer as the container it is.** Two halves, one file (`pkg/lang/container_return.go`).

- **The `return` builds an object.** A literal arm is lowered by `containerOperand` — the same
  `heapListFrom` / `heapDictFrom` / `heapSetFrom` a binding uses (ADR 0163) — so the word that crosses the
  `ret` is a **heap handle in a register**. No emitted module under this row contains `ret i32 @.`;
  `TestNoEmittedModuleReturnsAGlobal` and `TestTheProgramsThatBrokeTheVerifierNowBuildModules` fail on any
  module that does, and every one is checked with `llc` (`mustVerifyWithLLC`).
- **The kind is recorded, per function, from every arm.** `containerReturningFuncs(prog)` answers
  `list` / `set` / `dict` for a function only when **all** its `return`s answer one and the same kind, its
  body cannot fall off its end (`bodyEndsInReturn`), no arm is bare, no arm is `None`, no arm is a number,
  text or tuple, and the walk saw everything: `containerBodyIsOpaque` condemns a body containing any
  statement form it does not visit rather than let it escape the scan. A function the scan declines is
  left on exactly the road it took before, which is what keeps this row from being a regression machine —
  `TestTheScanRecordsAFunctionOnlyWhenEveryArmAnswersIt` asks the scan directly, with nothing emitted, over 28 bodies: 18 it
  must decline and 10 it must claim.
- **The answer is read by the roads a container variable already uses.** `containerAnswerOperand` lowers
  such a call to a fresh rooted name holding the handle and writes the notebooks every road consults today
  (`listVars`, `runtimeDicts`, `runtimeSets`, and the element-kind notebooks `listElemStr`/`listElemInt`,
  `setElemStr`/`setElemInt`, `dictKeyStr`/`dictKeyInt`, `dictValStr`/`dictValInt`). Callers then
  re-dispatch through that name. The consequence is the property this row is really about: `print(build())`
  answers what `ys = build(); print(ys)` answers, because they are the same call on the same name. That
  covers the print door, `str`/`repr`/f-string fields (through `renderPairContainer`, ADR 0258's one
  renderer with a switched sink), `len`, `in` / `not in`, `not`, truthiness, a `for` iterable, `==`, and a
  binding — each of which was a wrong answer or a silence before.
- **An unasked position refuses by name instead of answering with the handle.** The ordinary one-word call
  road (`g.containerAnswerOf(fnName)` without `containerAnswerIsAsked`) returns
  `containerAnswerRefusal(fnName)`, which names the function and the door that *does* answer, and exits 1.
  A word that means a container has no honest one-word answer, and the rule ADR 0249 states — never turn a
  working answer into a refusal, never let a refusal print at exit 0 — cuts both ways here: this backend
  already had no answer for those positions, and it now says which door it is waiting for rather than
  printing the handle.

## The gate is the row

Two carve-outs keep the scan from stealing programs whose refusal sentences are another road's contract.

- **A parameter is a hole through which the caller's value becomes a slot of the answer.** If any call site
  in the program hands a double, a float-typed name, a pair-bound name, a float-returning call or a
  pair-returning call into a parameter that lands in a slot
  (`containerAnswerCallSites` → `argCarriesPair` → `containerAnswerHandsAPair`), the whole function is
  declined and the **pair road** keeps it, with its honest `(payload, tag)` sentence. The two rows that
  prove the carve-out differ by one character in the call: `print(b(2))` is claimed and answers `[2, 1]`;
  `print(b(2.5))` is declined and keeps the pair road's words. `containerAnswerCallIsCarriable` applies the
  same test per call site, at the four `*Call` arms in `heapargs.go` and on the ordinary road.
- **The tag question is asked of the same predicate as the build question.** `containerAnswerCarriesTags`
  is `literalNeedsHeap(lit) || g.literalNeedsTags(lit) || g.literalHasPairElement(lit)` — not the narrower
  `literalNeedsTags` alone. Asked narrowly it shipped a wrong answer in the first hour of this row:
  `[1, "a"]` printed `[1, 0]`, because the string slot's tag was never written and the printer read the
  interned index as a number. A gate that asks a narrower question than the builder asks is ADR 0249's bug.
- **The element-kind notebooks are the binding road's, not a copy.** `recordContainerAnswerElementKinds`
  exists because the loop road and the printers ask `dictKeyStr`/`listElemStr` and not only
  `runtimeDicts`; without it `for k in build(): print(k)` over a dict printed `0`, `1` — the interned
  indices — where CPython prints the keys.
- **Run-built arms are read from the writes, not the bindings.** `containerAnswerSlotWrites` walks
  `append`/`extend`/`insert`/`add`/subscript-store through every compound statement (`if`, `for`, `while`,
  `try`, `with`, `match`), so `xs = []` / `for i in range(3): xs.append(i)` / `return xs` is known to
  answer a list of ints even though the binding it was born with is empty.

## What the row leaves standing, filed rather than bundled

Each of these carries this backend's answer and the reference's beside it in
`TestWhatTheContainerAnswerStillCannotReachIsFiledNotFixed`, and fails the day a door grows:

| Position | Reference | This backend | Row |
|---|---|---|---|
| `build()[7]`, `f()["b"]` | `IndexError`, `KeyError` | refuses | **Gap R.214** — unblocked by this row: the answer arrives, the subscript door has no arm for it |
| a dict answer bound, then read | `x` | `0` | Gap R.221 (one word, kind in the tag the write left) |
| `def f(): return None` / `print(f())` | `None` | `0` | **Gap R.228**, filed here |
| `[build(), 3]` | `[[1, 2], 3]` | refuses | **Gap R.229**, filed here (the element door beside ADR 0332's) |
| `build().append(3)` | `None` | refuses | **Gap R.229** (the method receiver) |
| `takes(build())` where `takes` is `len(xs)` | `2` | refuses | Gap R.154 (a container through a parameter) |
| `build() + [9]` | `[1, 2, 9]` | refuses | Gap R.175 (no runtime concatenate helper) |
| `la = [1,2]` / `return la` | `[1, 2]` | refuses | Gap R.35 (a function reads module-level containers) |

Nothing was relaxed to make the suite green: every refusal above is asserted to *stay* a refusal, with its
sentence pinned and exit 2 forbidden, and the pair road's rows are asserted to keep their own sentence —
`TestThePairRoadKeepsWhatTheContainerAnswerLeavesIt` fails if this scan claims a program the pair road owns.

## IR / codegen consequences

- The `ret` is a register: `call i32 @rt_list_from(...)` / `@rt_dict_from(...)` / `@rt_set_from(...)`
  followed by `ret i32 %tN` — never `ret i32 @.lstN`. A global in a value position is a verifier error, so
  the row's own exit class is the IR-shape assertion (`TestNoEmittedModuleReturnsAGlobal`, unit and CLI).
- The answer announces its tagging with `rt_mark_estr`'s bit 8 when (and only when) its slots describe
  themselves, decided by the same predicate that chose the builder — a uniform `[1, 2]` keeps the cheap
  static print path.
- The materialisation happens **once per call site position**: `permitContainerAnswer` returns a permit
  whose `Done()` retires it, so a print that lowers the answer, records it under a root name and then
  prints that name does not build the object twice.
- Every module asserted here passes `opt -passes=verify` and `llc`; the CLI rows run `--verify-llvm` beside
  the run, so a row cannot pass by printing the right thing out of a module the backend would reject.

## Alternatives rejected

- **A runtime tag word beside the return** (return a `(payload, tag)` pair, as the slot roads do).
  Rejected: it changes every caller's ABI for one construct, and the caller already cannot see the body —
  which is exactly what makes a compile-time record sound here. `pairRetDone` (ADR 0309) did the pair
  version for one shape and pays a register per call; this row needs the *kind*, which is a fact about the
  callee's own source.
- **Widen the existing `strFuncs`-style scan to containers and trust the first arm.** Rejected by Gap
  R.219: one arm is not every road, and the caller cannot check.
- **Materialise at the call site instead of the `return`.** Rejected: the same literal returned from two
  positions would then be built by two roads, and the run-built arms (`return xs`) have no literal at the
  call site to build — the handle is created inside the callee, so the kind has to be known there too.
- **Let the ordinary call road answer with the handle and teach each printer to notice.** Rejected: that is
  the status quo that printed `0`, and it fails ADR 0249's rule in reverse — a value whose kind the compiler
  knows must not be printed as the index it happens to be.
- **Claim the pair road's and the method road's programs too, and refuse them later.** Rejected: a refusal
  sentence is a contract (ADR 0249), and an over-claiming scan that steals a program from the road which
  names its missing half produces a *lie* or a worse sentence, never a better one. Hence the call-site
  carve-outs, asserted in both directions.
- **Record `None` returns on the same table.** Rejected: `None` is not a container, its answer needs its own
  printer decision, and mixing the two tables is how Gap R.228 would become invisible. It is a row of its
  own.

## Verification

- `pkg/lang/container_return_test.go`:
  - `TestAContainerReturnedFromACallAnswersTheContainer` — the answered rows, value-for-value against the
    reference's words;
  - `TestTheReturnedContainerIsBuiltAndPrintedAsAnObject` — the object doors: `rt_list_from`/`rt_dict_from`
    /`rt_set_from` present, handle not a number, bit 8 when the slots describe themselves;
  - `TestNoEmittedModuleReturnsAGlobal` — no `ret i32 @.` in any module, `llc`-checked;
  - `TestAnAnswerThatIsNotAContainerIsLeftOnTheOrdinaryRoad` — the negative control: a number, a text, a
    tuple, a generator, an `async def`, a decorated body and a mixed-arm body all keep the road they had;
  - `TestTheScanRecordsAFunctionOnlyWhenEveryArmAnswersIt` — 19 decline rows + the four pair-carve-out rows,
    asked of the scan directly with nothing emitted;
  - `TestAPositionWithoutAContainerRoadRefusesByName` — a refusal that names the function **and** the door;
  - `TestTheProgramsThatBrokeTheVerifierNowBuildModules` — the exit-2 rows, now `llc`-clean;
  - `TestWhatTheContainerAnswerStillCannotReachIsFiledNotFixed` — the ledger table above, compiled answer
    and reference answer both pinned;
  - `TestTheContainerAnswerRowsAreRecorded` — the record leg holds an answer for every row, so the door
    cannot be removed without removing the evidence;
  - `TestAPairElementListReturnedFromACallAnswersTheList` — ADR 0332's element door inside this row's return
    door, which ADR 0332 had filed as answering `0`.
- `integration/container_return_test.go`: the answers through the CLI against CPython with expectations
  derived at run time, the refusals by exit class (exit 2 forbidden, stdout empty, no `codegen:` at exit 0),
  `TestThePairRoadKeepsWhatTheContainerAnswerLeavesIt`, `TestNoAnswerModuleReturnsAGlobalToAValuePosition`,
  `TestTheContainerAnswerProgramMatchesTheReferenceThroughTheCLI` and
  `TestTheContainerAnswerProgramIsAnAssertedMatrixRow`.
- `integration/programs/container_returns.gy` + `integration/testdata/container_returns.expected`, matrix row
  `programs/container_returns`, oracle `match`: three kinds, all the empty forms, `set()`/`list()`, a mixed
  list with text/`None`/`True`/a nested list, a nested list, a run-built list and dict, a loop-filled list, a
  comprehension answer, a forwarded answer (`return build()`), a computed arm, `str`/`repr`/`len`/`in`/
  `not in`/`not`/truthiness/`==`, loops over a list answer and a dict's keys, and a bound answer that is then
  mutated and compared.
- `go test -tags=llvm20 -count=1 ./...` and `make testshards` green; every emitted module passes
  `opt -passes=verify` / `llc`.
