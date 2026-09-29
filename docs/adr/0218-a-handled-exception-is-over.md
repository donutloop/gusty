# A handled exception is over

## Status

Accepted (cycle 166, Gap R.21 compiled half). Completes the rule ADR 0213 wrote for the interpreter.

## Context

The compiled backend keeps the exception state in module-wide globals: `@exn_flag` (one bit: an
exception is in flight and nobody has handled it), `@exn_code`, `@exn_msg`, `@exn_frame`. After every
call to a user-defined function the codegen emits a check — load `@exn_flag`, and if it is set, branch
to the innermost handler in scope, or to the function's raise-exit if there is none.

That scheme is sound and it is not LLVM's personality-based unwinding, so the program itself owns the
state — which means somebody has to say when an exception stops being in flight. Nothing did.
`tryStmt` dispatched the arms, ran the matching one, and branched to `finally` with the flag still 1.
The exception had been handled; the flag disagreed. The next user-function call's check found it set
and branched back to a handler — or, once the `try` was out of scope, straight to the raise-exit,
printing a report for an exception the program had already caught. Measured:

```gusty
try:
    crash = 1 // 0
except:
    recovered = 1

def f() -> int:
    return 5

print(f())
```

CPython prints `5`; the interpreter printed `5`; the compiled program died with an uncaught
`ZeroDivisionError`, exit 3. Whether the bug appeared was decided by the *statement after* the try:
`print(5)`, `print(len("ab"))` and `print(recovered)` were fine — no call, no check — while anything
calling a user-defined function resurrected the exception. A call before the `try` was harmless.
Putting `print("handled")` after the arm made the compiled program print `handled` and *then* die:
the arm had run, the function had continued, and the exception came back later.

Two more shapes fell out of the same measurement and had a second cause. An arm whose last statement
is `return` (`except: return 42`) never reached the clear at the end of the arm, and neither did a
`break`/`continue` out of one — the control transfer escaped the edge where the clear lives.

## Decision

**An exception stops being in flight the moment an arm accepts it, and every edge leaving the arm
must say so.** The compiled lowering now stores `0` to `@exn_flag` on:

- the arm's normal fall-through into `finally`;
- a `return` from inside an arm, before the `ret`;
- a `break` or `continue` from inside an arm, before the branch.

The second group needs to know it is in an arm, so `irGen` carries `handledArms`, incremented while an
arm body is being lowered and zeroed by `funcDef` — a `def` written inside an arm is a different
function, and its `return` is not that arm's exit.

What is deliberately **not** cleared: the arm's own `raise` (a new exception, travelling outward —
`TestRaiseInsideAnArmDoesNotGetCleared` requires that a program whose only arm always raises emits no
clear at all), and the no-arm-matched path, which stores `1` again and hands the exception to the next
handler out or to the raise-exit, exactly as ADR 0213 specified. The uncaught-report path reads
`@exn_msg`, not the flag, so clearing cannot silence a real report; `TestUncaughtTrapStillTrapsOnBoth
Backends` keeps that honest, because a fix that simply muted the flag everywhere would pass every
positive case.

`blockEndsInTerminator` is what keeps the emitted IR honest: if the arm body already ended in `ret` or
`br`, the codegen does not append another clear and an unreachable `br` after it.

## Agentic rationale

The class of failure is the one this loop has chased longest: the same program giving two answers
depending on which engine runs it, with the *wrong* answer looking confident. A compiled program that
dies with a traceback for an exception it handled is worse than a refusal — an agent reading the
report fixes code that was never wrong. The measured trigger ("the next call, not the next line") makes
this a hidden bug in existing programs rather than an unsupported feature, and it explains why the
earlier probes, which put a call before their `try`, looked like a different problem entirely ("raises
from function bodies are unsupported"). A bug whose trigger you have not characterised gets diagnosed
into the wrong file, so the grid — eleven shapes, three engines — is in the commit and the reproduction
is in this ADR.

## Codegen / IR implications

Two instructions per accepting arm, both stores to module globals: `store i32 0, i32* @exn_flag` on
the accepting edges. Modules grow by a line or two per `try`, no new functions, no new globals, no
change to the GC's root discipline (the flag is an `i32`, never a heap value). `@exn_flag`'s meaning is
now stated once and honoured everywhere: `setExn` sets it, the no-match path re-sets it, `clearExn`
clears it, and `checkExn` is the only reader.

## Alternatives rejected

- **Clear at the end of the whole `try`, after `finally`.** Rejected: the no-arm-matched path leaves
  the statement by branching outward, and its exception must survive; clearing at one common exit
  would swallow precisely the exceptions ADR 0213 made sure stop being deleted.
- **Make `checkExn` compare a per-`try` "generation" instead of a bit.** Rejected: it would need a
  counter saved and restored at every handler entry and exit, which is a landing-pad redesign, and the
  bug is not in the dispatch — the dispatch found the right handler every time — it is in nobody ever
  saying the search was over.
- **Clear only on the arm's fall-through edge and document that an arm must not `return`.** Rejected:
  `except: return default()` is the most idiomatic error-handling shape in the language, and refusing
  or mis-running it is the trade this loop has refused repeatedly.
- **Switch to LLVM's `landingpad`/`resume` unwinding.** Rejected for now, and it is the honest long
  alternative: it would make the state per-frame instead of module-global and structurally prevent
  this class of bug, at the cost of personality functions, real call stacks (Gap K.8/L8.5) and a much
  larger lowering. Recorded here so the next reader knows the option was seen, not missed.
