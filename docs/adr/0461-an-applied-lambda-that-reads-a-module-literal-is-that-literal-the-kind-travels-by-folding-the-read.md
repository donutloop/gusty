# ADR 0461 — an applied `lambda` that reads a module literal **is** that literal: the kind travels by folding the read

**Status:** Accepted
**Date:** 2026-10-29
**Decides:** roadmap **Gap R.331** (applied-`lambda` rows), the answer direction of **ADR 0368** at the
`Name` arm, the hoist pass of **ADR 0401** / **ADR 0402**, and the module-level read of **ADR 0227**
**Filed:** **Gap R.546** (a name the program rebinds), **Gap R.547** (a `def` whose `return` reads a
module-level `None`), **Gap R.548** (a dict literal's key word lost across a sibling print argument),
**Gap R.549** (a source-string flag handed another flag measures the flag's own name at exit 0)
**Keeps open:** **Gap R.333**'s exit-code half, **Gap R.343**, **Gap R.386**, **Gap R.387**, **Gap R.35**
**Related:** ADR 0166, ADR 0227, ADR 0232, ADR 0273, ADR 0294, ADR 0302, ADR 0321, ADR 0345, ADR 0368,
ADR 0372, ADR 0401, ADR 0402, ADR 0448, L11.7, L11.1

## The rule

**Where a program reads a fact the module states once, the compiler is allowed to read it too — and when the
only thing standing between that fact and a wrong answer is the fact's SPELLING, the fix is to put the
spelling back, not to keep a second table of what the spelling would have meant.**

## Context

Two lines, and the answer is a digit nobody wrote:

```python
t = "hey"
print((lambda: t)())      #  gusty: 0        CPython: hey
```

`0` is the index the intern table handed `hey`. Nothing about the read was broken: the module road carries an
interned text through one `i32` exactly as **ADR 0227** designed, and it is the same word that makes
`NAME = "world"` / `def greet(): print(NAME)` answer `0` too (**Gap R.343**, one construct away, still open).
What was missing was the **kind at the call boundary**. **ADR 0368**'s answer direction registers the kinds
a body can hand back, and it registers a kind only where the source *spells* one — a `StrLit` spells a text,
a `True` spells a verdict — and finds no spelling in a `Name`. So the answer fell to the one-word road, the
print door downstream was never told the word it held was an index, and the module printed the index.

The control that localised the row to the spelling and not to the road is the same program written as a `def`:

```python
t = "hey"
def f():
    return t
print(f())                #  gusty: hey      CPython: hey
```

Measured on the toolchain before this commit, with the live reference beside it, over every kind the module
table admits (`moduleEnvFor` holds a name bound **once**, at module level, to an `IntLit`, `StrLit`, `BoolLit`
or `NoneLit`, never rebound and never declared `global`):

| program | the compiler | CPython |
|---|---|---|
| `t = "hey"` / `print((lambda: t)())` | `0` — the intern index | `hey` |
| `b = True` / `print((lambda: b)())` | `1` — the verdict's payload | `True` |
| `z = None` / `print((lambda: z)())` | `0` — the void's payload | `None` |
| `n = 7` / `print((lambda: n)())` | `7` ✓ | `7` |
| `t = 2.5` / `print((lambda: t)())` | refused in words (exit 1) | `2.5` |
| `xs = [1, 2]` / `print((lambda: xs)())` | refused in words (exit 1) | `[1, 2]` |
| `t = "hey"` / `print((lambda x: t)(9))` | `0` | `hey` |
| `t = "hey"` / `x = (lambda: t)()` / `print(len(x), x.upper())` | `0 0` | `3 HEY` |

Four silent wrong answers at the exit code of success — the class **ADR 0166** ranks above every other
defect, because the program is told nothing happened. And one more defect hiding under the first row, which
only the other leg showed: that same program, compiled through the leg every `pkg/lang` case walks, emitted
`%t1 = call i32 @rt_str_intern2(...)` in a `define i32 @gy_lambda_0()` with **no** `define` or `declare` of
`@rt_str_intern2` anywhere in the module — `llc-20`'s `use of undefined value`, **ADR 0166**'s exit 2,
arriving instead as an exit 0 with a wrong word on stdout. That is **Gap R.333**, filed as a separate row
because it is a separate failure: a missing request, and an exit code that hides it.

The row's own `Next concrete action` said to *register* the missing kinds — "register an applied lambda's
answer kinds the way `funcReturnsFloat` already registers its float". That road was considered, and refused
for the reason **ADR 0368** itself gives: *"A gate that guesses is worse than a gate that declines."* A
registry keyed by a name the module happened to bind is a second opinion about a program the source already
answered, and **ADR 0294**/ADR 0273 forbid a second opinion in a family that already has one door.

## Decision

**A body that is nothing but a read of a never-rebound module literal is folded into that literal, before any
answer scan reads the tree.** The fold lives in `pkg/lang/lambda_answer.go`, in the pass **ADR 0401** already
built for the same purpose — give an applied `lambda` a declaration its answer can travel by — and it is
asked once, at the top of `hoistAppliedLambdaAnswersTelling`, ahead of the candidate walk and of every
whole-program read in `GenerateIRReport`.

1. **One question, asked of one table.** The fold consults `moduleEnvFor` — the same table
   `codegen_value.go` consults when a compiled body reads a module name (**ADR 0227**). It admits a name
   bound once, at module level, to one of four literal kinds, never rebound, never declared `global` by any
   body (**Gap R.48**/**ADR 0448**'s lesson). The fold and the read cannot disagree about which names are
   values, because there is no second list: `TestTheFoldFiresOnlyWhereTheTableAndTheScopesAgree` asks the
   fold as a function over 14 shapes, and `TestTheFoldSharesNoNodeWithTheModulesOwnStatement` pins that the
   folded body is a fresh node — two emitters walking one AST node is how a per-node cache starts answering
   two programs at once.

2. **The fold is a reader, not a writer.** It rewrites an `*Expr` field of a `Lambda` node on the tree
   codegen was handed (`copyProgramAST`'s copy, the same discipline the defunctionalisation pass and
   **ADR 0401** keep), so `--emit-ast`, `gusty fmt` and the checker still see the `lambda` the human typed
   (**ADR 0320**). Its span is the READ's, not the binding's, so the traceback points at the program's line
   (**ADR 0449**).

3. **Three binders make the name not the module's, and all three are asked.** A parameter of the `lambda`
   itself (`(lambda t: t)` answers the argument, not `t = "mod"`), a parameter of an enclosing `lambda`
   (`(lambda t: (lambda: t)())("own")` prints `own`), and any frame below module level — the walk does not
   descend into a `FuncDef` or a `ClassDef` at all, because a body reads its own frame and the rows that own
   those reads are **Gap R.386** / **Gap R.387**. Containers are excluded by the table itself, which is what
   keeps the fold from minting a fresh object where the reference shares one.

4. **What the fold buys is the door it inherits.** After the fold the body *spells* its kind, so the answer
   goes where a spelled answer already goes: text, verdict and void bodies are hoisted to a declaration of
   their own by **ADR 0402**'s arm and are registered, materialised at the `ret` and read by the eleven
   positions that read a `def`'s answer; a number body stays on the emitter's road, which already answered
   it. There is no "folded read" road to fall out of step with, because the fold wrote the answer's spelling.

5. **A fold does not have to buy a hoist, and the account says so separately.** `gustyc --lambda-answers`
   gains `reads_folded` and `folded_reads[]` (`{name, literal, line}`), with the schema's
   `definitions.lambdaAnswerFold`; they are a separate claim from `rewritten` and from `declarations`. The
   measured case is a NUMBER body: `n = 7` / `print((lambda: n)())` reports `reads_folded: 1` with
   `folded_reads: [{name: "n", literal: "7", line: 2}]` and `declarations: []` — the body was rewritten, no
   declaration was bought, because the one-word road already answers a number and the fold simply made the
   program say so. What the fold does NOT reach stays exactly where it was, measured rather than assumed:
   `t = 2.5` / `print((lambda: t)())` is refused at exit 1 by `a call has no number the compiled backend can
   lift … (roadmap L11.1, Gap R.82, ADR 0166)` and `xs = [1, 2]` / `print((lambda: xs)())` by `"xs" is bound
   at module level, and a compiled function body cannot read module-level state that changes … (… roadmap
   Gap R.35)` — the candidate is declined by the pass's own `captures_frame` gate (`reads_folded: 0`) and the
   ordinary road refuses, which is the honest half of each answer. The two fields are added **additively** to
   `gusty.lambda-answers.v1`: a consumer that ignores unknown fields is unaffected, and the tag a pinned test
   reads does not churn for a superset.

## Where the fold reaches, and where it does not

| program | before | after |
|---|---|---|
| `t = "hey"` / `print((lambda: t)())` | `0` | `hey` |
| `b = True` / `print((lambda: b)())` | `1` | `True` |
| `z = None` / `print((lambda: z)())` | `0` | `None` |
| `n = 7` / `print((lambda: n)())` | `7` | `7` (unchanged) |
| `t = "hey"` / `x = (lambda: t)()` / `print(x == "hey", len(x), x.upper())` | `0 0 0` | `True 3 HEY` |
| `t = "hey"` / `print(f"[{(lambda: t)()}]")` | `[0]` | `[hey]` |
| the same program through the leg that runs `llc` | module rejected, exit 0 | module defines `@rt_str_intern2`, answers |
| `t = "hey"` / `t = t + "!"` / `print((lambda: t)())` | `0` at exit 0 (CLI), `llc` rejection on the other leg | **unchanged** — **Gap R.546** |
| `t = None` / `def f(): return t` / `print(f())` | `0` | **unchanged** — **Gap R.547** |
| `t = 2.5` / `print((lambda: t)())` | refused | refused (L11.1's tagged word) |
| `xs = [1, 2]` / `print((lambda: xs)())` | refused | refused (**Gap R.35**'s door) |
| `(lambda t: t)` beside `t = "mod"` | answers the argument | answers the argument (control) |

**Gap R.333** is recorded as PARTIAL, not DONE, and honestly so: its *named program* now compiles and answers
`hey`, because the road that emits the intern call is the `def` road, which asks for the runtime block it
lives in. What that row still owns is its second half and its own DoD clause — *a module that calls a runtime
entry without defining it cannot leave the toolchain at any exit code but 2* — which is a general audit the
compiler does not have, and which this commit did not build. A fold cannot close it: any other road that
emits a helper without requesting its block reproduces the class.

Three defects measured on the way are filed with their own IDs rather than folded into this one, because
none of them is the fold's to fix:

- **Gap R.546** — the module writes the name a second time. The table calls it state, the fold correctly
  declines, and the CLI answers the intern index at exit 0 while the reference answers `hey!`. Closing it
  needs **Gap R.343**'s module-slot kind, not a fold over mutable state.
- **Gap R.547** — the mirror image on the other side of the boundary: a `def` whose `return` is a read of a
  module-level `None` prints `0`, where `def f(): return None` has printed `None` since **Gap R.228**. Same
  missing spelling, different door.
- **Gap R.548** — `print({"k": (lambda: 7)()}, (1,))` writes `{1: 7}`: the dict's text key prints its intern
  index when the statement goes on to print a container, and the order decides (`print((1,), {…})` is right).
  Narrowed by measurement rather than by shape-guessing, on a build of the parent commit (`5f72e56`) beside this
  one and byte-for-byte identical between them: the dict's value has to be an applied `lambda` — a spelled
  `{"k": 7}`, a `{"k": x}` over `x = 7`, and a `{"k": g()}` over `def g(): return 7` all print correctly in the
  same seat — and the following sibling has to be a container (a text sibling is harmless). Widened one step more,
  the list is read as a word as well: `print([(lambda: 7)()], {"k": (lambda: 7)()}, (1,))` is `[1] {1: 7} (1,)`
  against the reference's `[7] {'k': 7} (1,)`. That is what says it is **ADR 0173**'s per-side element-kind claims
  leaking across a sibling argument, and not this fold's: the fold is not asked for the dict's value in any way
  that changes its kind. The conformance program keeps that shape out, for the same reason **ADR 0460** kept the
  quadruple-printing `str(len([f()]))` out of its own corpus row: a corpus row certifies whatever it contains.

## Console

```
$ printf 't = "hey"\nprint((lambda: t)())\n' > /tmp/read.gy
$ gustyc --file /tmp/read.gy                 # was: 0
hey
$ printf 'b = True\nz = None\nprint((lambda: b)(), (lambda: z)())\n' > /tmp/kinds.gy
$ gustyc --file /tmp/kinds.gy                # was: 1 0
True None
$ gustyc --file integration/programs/applied_lambda_module_read.gy   # line-for-line the reference's bytes
hey
True
False
…
# NB: `--lambda-answers` takes a SOURCE STRING, not a path — `--lambda-answers --file prog.gy` measures the
# program the string `--file` spells (an expression, at exit 0) and is filed as **Gap R.549**.
$ gustyc --lambda-answers 't = "hey"
print((lambda: t)())' | tail -9
  "reads_folded": 1,
  "folded_reads": [
    {
      "name": "t",
      "literal": "\"hey\"",
      "line": 2
    }
  ]
}
```

## Verification

- `pkg/lang/lambda_answer.go` — `foldAppliedLambdaModuleReads`, `appliedLambdaModuleReadFold`,
  `copyModuleLiteral`, `appliedLambdaFoldSpelling`, and two new hooks on the pass's own walker
  (`skip`, `enter`) which is what lets the fold track binders and refuse a `def`'s subtree instead of
  reusing a walk that could not.
- `pkg/lang/lambda_module_read_test.go` —
  `TestWhatAFoldedModuleReadAnswersWhatTheReferencePrints` (20 sources, CPython's bytes in the table),
  `TestWhatAFoldedModuleReadAnswersWhatTheRecordRecorded` (the same sources on the record leg, failing if NO
  row is covered, so the leg cannot quietly stop being walked),
  `TestWhatAFoldedModuleReadRefusesNothingItClaimsToAnswer`,
  `TestTheFoldedReadCarriesTheKindIntoTheModule` (the module both CALLS and DEFINES `@rt_str_intern2` —
  **Gap R.333**'s first half, asserted in the emitted text),
  `TestTheFoldFiresOnlyWhereTheTableAndTheScopesAgree` (14 decision rows asked of the gate as a function:
  the four kinds fold, a rebound name, a container, a loop target, an enclosing-`lambda` name, a `def` local,
  a parameter, an embedded read and an already-literal body do not, with the fold COUNT asserted too so a gate
  that widens fails a row),
  `TestTheFoldSharesNoNodeWithTheModulesOwnStatement`,
  `TestTheFoldedReadAccountSaysWhatItFolded` (the machine path, and the zero case),
  `TestWhatTheFoldedModuleReadLeavesStandingIsFiledNotFixed` (**Gap R.546**'s two legs, **Gap R.547**,
  **Gap R.548** — each with today's bytes, the reference's, and the sentence that says DELETE when they
  agree), and `TestWhatTheFoldedModuleReadStillRefusesInWords`.
- `integration/lambda_module_read_test.go` — the same family through the CLI against a live `python3`
  (`--eval`, 23 sources, exit code asserted in every row),
  `TestTheFoldedReadModuleDefinesWhatItCalls` (every `@rt_*` the emitted module calls is defined or declared
  in it, plus `--verify-llvm`), `TestWhatTheFoldedModuleReadLeavesStandingAtTheCLI` — which is where
  **Gap R.546**'s wrong value at exit 0 is visible, the `pkg/lang` leg having died in `llc` instead —
  `TestTheSourceStringAnswerFlagsRefuseToMeasureTheirOwnName` (the machine path's own trap, held beside the one
  account door that already refuses it: today the answer flags report a well-formed account of the program `--file`
  at **exit 0** where `--verify-llvm` answers the usage class — **Gap R.549**), and
  `TestWhatTheFoldedModuleReadStillRefusesAtTheCLI` with exit 2 forbidden everywhere.
- `integration/programs/applied_lambda_module_read.gy` (matrix row `programs/applied_lambda_module_read`,
  oracle `match`) — the five kinds, every position the answer reaches, a parameter the body ignores, and the
  two controls (an already-spelled body; the same read as a `def`).
- **23 sources** came in from the reference — 21 through this row's own recorder
  (`GUSTY_RECORD_LAMBDA_MODULE_READ`) and `tools/recmerge`, which refuses any entry the compiled leg will not
  agree to — and the corpus program's own text, plus the control the answered table gained last, through `tools/recmerge -sources` directly — the artifact going **10626 → 10649**.

## Alternatives rejected

- **Register the answer kinds for a `Name` body, as the row's `Next concrete action` proposed.** Rejected on
  **ADR 0368**'s own terms: the scan spells a kind out of the source, and a body of `return t` spells none, so
  a registry keyed by the name would be a *guess by table*. **ADR 0219**/**Gap R.219** is what a
  name-keyed kind table does when the name and the value come apart — `print(f(1))`/`print(f(0))` printing
  `x`/`x`. The fold puts the spelling in the program, where every existing reader can see it and none of them
  has to be taught a new fact (**ADR 0232**).
- **Teach the print door (and `str`, `len`, `==`, an f-string, a binding …) to ask what a module name holds.**
  Eleven positions, eleven chances to diverge — the exact family **Gap R.261** documents, and the reason
  **ADR 0401** gave an applied `lambda` a declaration instead of a per-position door.
- **Fold a name the module rebound to its last binding.** Rejected: `moduleEnvFor` disqualifies it for the
  same reason **ADR 0448** gives for `global` — `k = "a"` folded the print to `a` after a setup function had
  written `b`. The fold is sound precisely because the name never changes; **Gap R.546** is the price, and
  the price is a filed row rather than a guess.
- **Fold container literals too.** Rejected: `x = (lambda: t)(); x.append(3)` would then mint a fresh list per
  call where the reference shares one, trading today's honest refusal (**Gap R.35**) for an answer no program
  can defend.
- **Hoist instead of fold** (point the callee at a minted `def` whose body is `return t`). This is what
  **ADR 0401** does for provable bodies, and it does not help here: the hoisted body would still read the
  name, still spell no kind, and the answer would still travel as one untagged word. The fold is what makes
  the hoist worth doing.
- **Refuse the shape until the tagged value word of L11.1 arrives.** Rejected per **ADR 0166**: the program
  compiles, the reference answers it, and a refusal is the wrong answer to a program that has an answer —
  especially when the same program written two characters differently (`def` instead of `lambda`) has always
  been answered correctly.
