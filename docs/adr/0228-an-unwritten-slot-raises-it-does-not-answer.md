# A slot the program never wrote raises; it does not answer

Status: accepted. Closes roadmap Gap R.36 and Gap R.39, closes one instance of Gap R.37, and fixes an
exit-code-contract hole of Gap R.17's kind. Cites: ADR 0211 (a failure class has one code), ADR 0212
(a built-in trap is a typed raise), ADR 0218/0223 (unwind and method plumbing the raise reuses),
ADR 0220 (the module is a frame too), ADR 0227 (the module bindings this leans on), ADR 0166 (a
refusal, not an invalid module).

## Measured before anything was decided

Fourteen shapes, three legs each — CPython, `--interp`, `--aot`. Nine were silent wrong answers on the
compiled path alone. The interpreter agreed with CPython about *that* a read failed but not about
*what it was called*, which is Gap R.39.

| shape | CPython | interpreter (before) | compiled (before) |
| --- | --- | --- | --- |
| `def f(c): if c: x = 1` then `return x`, called `False` | UnboundLocalError, exit 1 | NameError, exit 3 | **prints `0`, exit 0** |
| `while 0: w = 1` in a function, read after | UnboundLocalError | NameError | **prints `8555776`**, exit 0 |
| `try: a = 1//0` / `b = 2` / `except: pass`, read `b` | UnboundLocalError | NameError | **prints `518208`**, exit 0 |
| `for i in []: z = 1`, read `z` | UnboundLocalError | NameError | refuses, exit 1 |
| `if c: p, q = 1, 2`, read `p` on the untaken edge | UnboundLocalError | NameError | **prints `0`**, exit 0 |
| `match v: case 1: hit = 1`, read after a non-match | UnboundLocalError | NameError | **prints `0`**, exit 0 |
| `if c: t = 0` then `t += 1` | UnboundLocalError | NameError | **prints `1`**, exit 0 |
| `print(v)` before `v = 2` in a body | UnboundLocalError | NameError | refuses, exit 1 |
| module: `if 0: x = 1` then `print(x)` | NameError | NameError ✓ | **prints `64`**, exit 0 |
| module: `while 0: w = 1` then `print(w)` | NameError | NameError ✓ | **prints `64`**, exit 0 |
| module: `try:` cut short, then `print(b)` | NameError | NameError ✓ | **prints `518208`**, exit 0 |
| module: `for i in []: z = 1` then `print(z)` | NameError | NameError ✓ | refuses, exit 1 |

The numbers were not arbitrary. They are the frame's previous contents: `64` is a leftover word,
`8555776` and `518208` are stale heap handles. Reading them is not merely wrong, it is reading memory
the program was never given and printing it as a value — and exiting 0 afterwards.

## Why not simply refuse at compile time

Because `def f(c): if c: x = 1; return x` is a *program*. CPython accepts it; whether the read is an
error is decided by the argument at the call. Refusing would reject valid code, which is the mistake
Gap R.24 already documents for scopes and Gap R.37 documents for refusals fired too early. So this is
a runtime fact and needs a runtime representation.

## Decision

**One bit per slot the checker cannot prove was written.** Not per slot — see "what it costs" below.

1. **The checker decides, once.** `UnwrittenReads(prog)` re-runs the existing checking walk and reports,
   per `*FuncDef` (nil key: the module's own statements), the names read on a path that does not assign
   them. Codegen does not get its own dataflow rule; a second implementation of "is this certain?" is a
   second opinion waiting to disagree.
2. **Codegen consults it and asks one extra question of its own: can I hook every write?** A flag is
   attached only to names whose every binding form in that body is a store site that sets the flag —
   assignment, augmented assignment, tuple unpacking, walrus, match capture, handle store. A name bound
   by a `for` header or `with ... as` is left exactly as it was, because a check whose flag some writer
   forgot turns a silent zero into a *spurious trap*, which is worse than the bug being fixed. The
   eligibility answer comes from the source, not from what the emitter happened to reach.
3. **Entry clears it, every write sets it, every read tests it.** `emitBoundAllocas` writes the allocas
   immediately inside the function's brace (an instruction emitted before the `define` line lands in the
   global area and `llc` rejects the module — exit 2, the compiler blamed for a source error, ADR 0166),
   and for main, immediately *after* `beginScope`, whose reset otherwise wipes the slot bookkeeping and
   produced `multiple definition of local value named '_x'`.
4. **The raise is an ordinary raise.** `branchRaise` with the class name and CPython's own message, so
   `try`/`except UnboundLocalError:` catches it on both backends — the same rule as ADR 0212: a built-in
   trap is a typed raise, and `UnboundLocalError` got its own code in the canonical table rather than
   borrowing `NameError`'s, because the two say different things and a program may catch one and not the
   other.
5. **The class follows the frame.** Inside a function the name is *this frame's* and has no value yet →
   `UnboundLocalError`. At module level, or for a name no frame owns → `NameError`. That split was
   Gap R.39: the interpreter already trapped, but called everything a `NameError`.
6. **A name is local because the body says so, not because the walk got that far.** `seedLocalsFromBody`
   pre-records every name a body binds anywhere inside it, so a read above the write is a *possibly
   unbound local* — warned about, flagged, trapped at runtime — and not `undefined name`, which had been
   refusing `def f(): print(v); v = 2` outright. The same seeding had to be applied to the per-call-site
   re-walk of a body, or the second walk reported `undefined name "v"` for a read the first had excused.

## Three checker rules that were quietly wrong

The compiled bug was the visible one; the checker's certainty model was the cause, and fixing it
changed behaviour in ways worth naming:

- **A `for` body may run zero times.** Names first assigned inside it were being carried out of the loop
  as *definite* (`while` already restored the pre-loop state, `for` did not). Consequence beyond the
  flags: `for i in []: z = 1` then `print(z)` is a NameError in CPython and now traps on both backends.
- **A `match` may match nothing.** Names that every arm binds were marked definite on the theory that all
  paths agree, but the path that runs no arm was not in the intersection. Certainty now requires an
  irrefutable pattern.
- **A loop variable *is* certain inside its own body.** Over-correcting the first rule flagged `for i in
  range(n): total = total + i` as an unbound read of `i`, which is nonsense — the header binds it before
  the body runs. The flag set is exactly: inside the body, the loop variable is definite; after the
  loop, it is not.

## The exit-code hole this exposed

`main.raiseexit` ended in `ret i32 1`. The CLI still reported exit 3 for `--aot`, so nobody noticed, but
the *linked binary* was exiting 1 — the compile-error code — for a program that simply raised. Any
script running `./prog` was being told the compiler had failed. ADR 0211 says a failure class has one
code whichever path produced it; the binary now returns 3, and two tests that pinned the emitted `1`
were updated rather than deleted, with the cause named in the comment.

## What it costs

Nothing, where it is not needed. A name the checker proves is definitely assigned gets no alloca, no
zero-store, no load, no compare, no branch — `fn_else_only_read_in_then` compiles to exactly what it
compiled to before, and the test asserts that (`bnd_` must not appear in the module at all). Where it
is needed, the cost is one byte per risky slot and one load+cmp+branch per read. The flag machinery is
also deliberately silent in the source-language sense: no new syntax, no new keyword, no annotation.

## Alternatives rejected

- **Refuse at compile time.** Rejects programs CPython runs; that is Gap R.37's error and Gap R.24's,
  and it is what three of the fourteen shapes were doing already — with a message claiming the
  interpreter reported the same error, which it did not.
- **Poison value / sentinel in the slot.** Ints are immediate: every `i32` is a legal integer, so no bit
  pattern is free to mean "unbound" (the same reason `None` is a heap object, ADR 0172). A sentinel
  would be a wrong answer with better branding.
- **`memset` the frame or zero-initialise all allocas.** Turns the trap into an *answer* of 0 for every
  local, which is precisely the bug; and it taxes every function to catch a few reads.
- **A flag on every local, decided by codegen.** Costs every body, and puts the certainty decision in
  the wrong component: two implementations of "was it assigned on all paths" would drift, which is what
  happened to `for` versus `while` here.
- **Reuse the closure environment.** Env captures carry their own representation questions (Gap R.16,
  L11.7), and the facts here are per-frame-slot, not per-capture.

## Also found, deliberately not fixed

`global` is not in the language. `def touch(): global gz; ...` parses as the expression `global gz`, so
CPython says `NameError: name 'gz' is not defined`, the interpreter says `name 'global' is not defined`,
and the compiled backend refuses — three engines, three answers. Recorded as Gap R.48 with
`programs/probe_global_statement.gy` pinned, rather than fixed off-plan in a cycle that is already
changing the checker's certainty model.

## Verified by

`pkg/lang/bound_flag_test.go` — the flag exists where the checker cannot prove the write, and exists
*nowhere* where it can; the module's frame raises `NameError` and a function's raises
`UnboundLocalError`; the raise is catchable by class on both backends; `UnwrittenReads` answered for
nine shapes including the three checker rules above.
`integration/unwritten_slot_test.go` — twelve shapes: CPython first (the test refuses to run if
CPython disagrees with the table), then both engines on stdout, class and exit code 3; plus the
not-over-eager and catchable cases.
`programs/unwritten_slot_trap.gy` is a pinned probe whose oracle row says what all three legs do;
`TestConformanceMatrix` counts it. Suite: `cmd/gustyc`, `integration`, `pkg/lang` all green.
