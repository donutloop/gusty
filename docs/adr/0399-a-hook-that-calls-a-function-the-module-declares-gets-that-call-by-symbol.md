# A hook that calls a function the module declares gets that call by symbol; the decline narrows to the function read as a value

**Roadmap:** **Gap R.378** (closed here — a `key=lambda` that merely CALLS a top-level `def`/`class` was refused
with the key door's "a lambda is a function VALUE" sentence) · **L11.7** (functions are values that compile; this is
the hook side of that row) · **ADR 0396** (the hook road this completes: it took the hook onto the shared capture
door and filed this shape as the residue) · **ADR 0345** (a callable is a declared `define` and not a value — §1 is
the rule this row stops over-applying, and the rule the narrowed decline still enforces) · **ADR 0370** (the one
capture door every road asks) · **ADR 0232** (one question, one asking place — this row deletes the third spelling
of "may this hoist happen?" and leaves one) · **ADR 0340** (the key door that calls the hook once per element) ·
**ADR 0363** (the hidden call site a hoisted hook already needs) · **ADR 0395 §4** (ask the gate where the value
lives — kept, unchanged, at a third road) · **Gap R.297** (a callable in a value slot: the half that stays refused,
now named by the refusal that keeps it) · **Gap R.35** (a module name the module rebinds — the reason the ordering
rule had to be conservative rather than clever) · **Gap R.332** (a capture whose value is not one word; untouched) ·
**ADR 0166**/**ADR 0249** (a refusal names what is missing and never the compiler's own machinery) ·
**ADR 0302** (the newly answered sources are recorded from the reference, not self-certified) ·
**Gap R.379** (filed by ADR 0398; this row names three more of its doors without opening them)

## Status

Accepted. Landed 2026-10-22.

## Context

ADR 0396 put the ordering doors' hook on the shared capture door: a `key=lambda` that reads a name of the frame its
call is written in is hoisted to a `define` of its own with one trailing parameter per such name, and the existing
call road fills those words by name from the caller's own reading. That row left one program refused, and filed it as
this one:

```python
def helper(v):
    return 0 - v

print(sorted([3, 1, 2], key=lambda x: helper(x)))      # CPython: [3, 2, 1] — before this ADR: exit 1
```

> `` `key=lambda ...` is refused: a lambda is a function VALUE, and this backend has none — `def` bindings are
> declared, not assigned, so the key has no word to carry to the call (roadmap L11.7) ``

The sentence was written for the right disease and diagnosed the wrong patient. The disease is real: this backend has
no function **value**. ADR 0345 §1 settled that — a callable is a `define` with a symbol, and the module that ships
contains no `fnptr`, no `ptrtoint`, no `inttoptr` and no indirect `call`. What follows from it is that *no word can
carry a function*: an `i32` slot can name an `int`, a `float` or a tagged heap object, and "the function `helper`" is
none of those.

What does **not** follow from it is that a hoisted body may not mention a function. The hoisted hook is itself a
top-level `define`, and every other `define` in the module calls `helper` by emitting `call i32 @gy_helper(...)`. The
gate that refused this program was `callableCapturesAreValueNames`, asked by the hook road alone after the shared
capture door had already answered:

```go
caps, capturable := callableLambdaCaptures(r.encs[rank], prog, l)
if !capturable || !callableCapturesAreValueNames(r, caps) {
    continue    // ← the whole family dies here, including the program above
}
```

`callableCapturesAreValueNames` looked a capture name up in the pass's own census of `def`s and `class`es and refused
the hoist if it found one. It never asked what the body **does** with the name — whether it calls it, prints it,
stores it, hands it to another call, or reads an attribute off it. So the shape that needed no word at all was
refused by the rule about words, and the three roads that hoist lambdas in this compiler disagreed with each other
about it: the applied road (`callableAppliedLambda`) and the entry road (`entryLambda`, ADR 0395) ask the same
`callableLambdaCaptures` and have **no** such gate — which is why `(lambda x: helper(x) + 5)(3)` had been printing
`2` for cycles while `sorted([3, 1, 2], key=lambda x: helper(x))` refused. ADR 0232's lesion, at the third road: a
question asked in one place, answered in a second, contradicted in a third.

## Decision

**Delete the extra gate. Ask the shared door three questions about the program, and let the answers decide.**

`callableLambdaCaptures` now consults, per free name:

1. **Does the module DECLARE it at its top level?** `callableModuleDeclarations(prog)` is the census of the module's
   own statement list — every `*FuncDef` and `*ClassDef` whose symbol the emitter declares, and nothing else. A `def`
   nested inside another body is not in it: only a name whose definition dominates the hoisted `define` can be called
   by symbol from it.
2. **Does the body read it outside a call position?** `callableFreeNameSet(l, callableCalleeNameNodes(l))` walks the
   body once with the callee `*Name` **nodes** marked out. The nodes, not the spellings: one body can spell `helper`
   twice, once calling and once reading — `key=lambda x: helper(x) + len(str(helper))` — and the two occurrences have
   different answers. If any reading survives, the body wants a function **value**, and the road declines with the
   sentence that sentence belongs to (**Gap R.297** owns that half).
3. **Does the frame the call is written in OWN the name?** `callableFrameOwns(enc)` — parameters, everything the body
   binds, `for` and `with ... as` and comprehension targets — is extracted from the body of this same door so the
   hook road and the capture road ask one question (ADR 0232). A frame binding shadows the module symbol:
   `def use(): helper = 3; … key=lambda x: helper(x)` reads the frame's number, and CPython's answer is
   `TypeError: 'int' object is not callable`. A door that answered `[3, 2, 1]` there would be a plausible handle at
   the exit code of success, which is the worst thing a compiler can do to a program.

A name that passes all three needs **no word**: it is dropped from the capture list, and the hoisted `define` calls it
the way the rest of the module does. `--callables` reports the difference directly — `capture_parameters` is `0` for
a hook that only calls, `1` for a hook that calls *and* reads one name of its frame.

Two rules were kept deliberately conservative, both because the alternative fabricates an answer:

* **Declaration order.** `decl.Span().Line > l.Span().Line` keeps the refusal. The front end already refuses
  `print(helper(1))` written above `def helper` (`undefined name`, where CPython raises `NameError`); a lambda body is
  not asked that question, and a hook hoisted above its `def` would print the function's answer where the reference
  raises.
* **A module that rebinds the name.** Any binding of the name in module-level code — before the call, after it,
  inside a module-level `if` or `for` body — leaves the shape a capture and so at the door. This is the soundness
  half of the row: the hoisted `define` reaches `helper` by symbol and can never see a slot, so a hook hoisted beside
  a rebind would print `[3, 2, 1]` where CPython raises `TypeError: 'int' object is not callable`. `moduleEnvFor`'s
  binding set covers the top-level assignments and `callableFrameOwns`/`collectLocals` covers the nested module blocks,
  which is why the conservative answer is also the cheap one.

## What moved, measured

55 programs — the 37 of the probe harness (the family plus the controls that had to stay answering), 16 soundness
programs written to try to make this row fabricate an answer, and the four table programs not already among them,
deduplicated by source. Each was asked of CPython first; then of a build of the parent commit (`6263dee`) and a build
of this commit, side by side, so the *before* column is a measurement and not a memory. "Agreeing" means the compiled
stdout equals the reference's byte for byte at exit 0.

| | agreeing with the reference |
|---|---|
| before (`6263dee`) | **13** — the controls, and nothing that calls a declared name from a hook |
| after | **36** |
| newly agreeing | **23** |
| regressed | **0** |
| programs where the compiled leg prints something the reference does not | **0** |

The last row is the one this ADR is really about, and it is the reason the two conservatism rules above stayed
refusals: a row that turns 23 refusals into answers while quietly turning one refusal into a wrong number is a net
loss, and the harness prints it as a failure rather than a statistic.

What came in: the module-level hook calling its function, the same hook under `min` and `max`, a hook that calls and
also reads a frame local (the two stages in one body — ADR 0396's fix holds beside this one), a callee that reads a
module name of its own, a callee with a second argument, a callee with a default the hook does not supply, the same
call with a **named** argument, the call over texts, a callee declared as a `class` (`Box(x).v`), a callee whose answer
comes from a branch, a hook written in a method's frame, two hooks of one frame, one hook called over two containers,
two calls of one function from one hook, a hook in a `while` body, the answer bound and measured and walked, a hook
whose callee calls a third function, a builtin called beside the declared one, a comprehension inside the hook, and
the comparison of two keyed calls.

Sixteen soundness programs were written to attack the gate rather than to flatter it, and 11 of them are refusals on
both builds — including three where **the reference answers a value and this backend still declines**: a module that
rebinds the function after the call, a hook that calls an alias the module bound to the function (`alias = helper`), and
a hook that hands the function to another function. Those three are the row's own evidence that the decline is narrow
rather than gone: they are the function-as-value shapes, they stay exit 1, and the sentence says so.

## The IR, asserted

A table of printed numbers cannot make the claim that matters here, so the module is read directly
(`TestTheHookCallsTheDeclaredFunctionBySymbol`, `TestTheNamedCallHookEmitsAModuleTheToolchainAccepts`):

```llvm
define i32 @gy_helper(i32 %p0)                      ; the module's `def helper` — a symbol, as always
define i32 @gy_sorted$lambda$…_0(i32 %p0)           ; the hoisted hook: ONE parameter, the element
  %… = call i32 @gy_helper(i32 %p0)                 ; the callee reached by SYMBOL — no word, no slot
  %… = call i32 @gy_sorted(... i32 %… )             ; the door still calls the hook by name
```

Three claims are asserted negatively across the whole family: the hoisted hook's parameter list holds no second
parameter for the callee (that would be ADR 0345 §1's bits coming back through the capture door), no operand is a
`load` from a module slot for the callee's name, and the module contains no `fnptr`, `ptrtoint`, `inttoptr` or
indirect `call`. The traceback keeps the program's spelling — `rt_frame_push(i8* @rt.frame.name.helper, i32 4)` —
because a hoisted hook calling a function is not a reason for a reader to lose the frame's name (ADR 0320).

## Agentic path

No flag changed, no schema field added, no exit code moved. What an agent can read:

* `--callables` is the machine path to the distinction this row is about: `capture_parameters` is `0` for
  `key=lambda x: helper(x)` and `1` for `key=lambda x: helper(x) + base`. Before the row both said "refused"; the
  account now separates the two shapes without being asked to.
* `--json` still answers a remaining refusal as a codegen-phase exit 1 whose `error` carries the door's sentence. The
  three shapes the row does not reach (`alias = helper`, a hook that hands a function on, an applied body that reads
  one) keep the capability codes the registry already had (`lower.unsupported.indirect-call`,
  `lower.unsupported.function-read-as-value`); the key road's own sentence is not in the registry today, and the table
  pins that so a code cannot appear or vanish without the table being read (ADR 0346).
* Exit 2 is forbidden in every loop of both new files: a hook hoisted to a `define` the assembler dislikes is exactly
  how this row could have failed, and a module that reached `llc` would mean the pass's decline was ignored.

## Alternatives rejected

1. **Leave the decline, since ADR 0345 says so.** Rejected by the measurement: 23 programs the reference answers were
   standing behind a rule about *words* applied to a *call*. ADR 0166's rule is that a refusal is the right answer for
   a program the backend cannot state, not for one whose only problem is a gate that asked the wrong question.
2. **Narrow `callableCapturesAreValueNames` instead of deleting it.** Rejected: its input is the pass's census of
   declarations keyed by spelling, which cannot see that one body spells `helper` both ways. Keeping the name would
   have kept a second answer to a question the shared door already answers, which is the disease and not the cure. The
   gate is deleted, and the door's own answers replace it — the same resolution ADR 0391/0392/0394 reached at the
   container roads.
3. **Hoist the hook but rewrite the callee call to read `%_helper`, the module slot.** Rejected as the shape the gate
   existed to prevent: `helper` has no slot when the module never rebinds it — the emitter declares a function as a
   symbol, and reading one as a value is the thing ADR 0345 forbids. This is the answer the row would have had to
   un-ship in the next cycle.
4. **Answer the alias case (`alias = helper`) too, by treating a module-level alias as a rename.** Rejected: the
   reference's answer comes from asking the *value* what it is at the moment of the call, and the module can rebind
   `alias` at run time. That is **Gap R.297**'s missing representation arriving through a side door; the refusal stays
   and names it.
5. **Decide the ordering rule by dataflow (is the `def` executed before this call?)**. Rejected: the cheap conservative
   line-ask costs one comparison and can never fabricate, and a gate that can be argued out of its answer by control
   flow will be argued out of it again — ADR 0397 rejected the same kind of upgrade for the same reason.
6. **Let a hook call a `def` declared inside a module-level `if` or `for`.** Rejected: the symbol's definition does not
   dominate the call, and the emitter declares functions that the front end treats as unconditional. Refusing is also
   what the ordinary call already does (`TestWhatTheNamedCallHookRefusesToAnswer` pairs it with the reference's answer).
7. **Fix the three emitter doors this row exposes (`unsupported attr expression`, the module-state sentence) in the
   same commit.** Rejected as bundling: they are **Gap R.379**'s family — a refusal with no line, no roadmap row and no
   program vocabulary — and they are pinned here as *filed, not fixed* rather than quietly widened into this row.

## Consequences

* Three roads hoist a `lambda` in this compiler — applied body, entry, hook — and all three now ask
  `callableLambdaCaptures` and take its answer without a second gate. A fourth road inherits the same question, which
  is the point of keeping it one function.
* `callableModuleDeclarations`, `callableFrameOwns`, `callableFreeNameSet`, `callableCalleeNameNodes` are new, and each
  is asked from exactly one place. `callableFreeNames` is now a thin wrapper over `callableFreeNameSet`, so the "what
  does this body read" census exists once.
* `callableCapturesAreValueNames` is gone — from the pass, from `--callables`' behaviour, and from two test tables that
  pinned its refusals. Deleting a pin rather than rewriting it is the house rule (Gap R.337's residue), and the shape
  it refused is now an answered family member instead of a decline case.
* The row leaves three honest refusals standing and names them in `TestWhatTheNamedCallRowLeavesStandingIsFiledNotFixed`
  with the reference's answer beside each: a hook reading an attribute of a module-bound object, a callee that mutates
  a module-level container, and a hook calling a method of a module-bound object. The first two print the *program's*
  half of the sentence badly (no line, no roadmap row) — **Gap R.379**'s family, with three more doors for it to read.
* The key road's own sentence is still absent from the capability registry. This row does not add it, because a code
  added while the sentence is being reworded is a branch an agent will silently stop taking; the table pins the current
  answer so the change has to be made on purpose.
* **Gap R.297** — the function value — is now the named owner of every remaining hook refusal that the reference
  answers, which is what makes the residue of L11.7 readable as one row rather than as a scattering of key-door
  quirks.
