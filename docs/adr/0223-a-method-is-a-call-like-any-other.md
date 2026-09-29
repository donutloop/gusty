# A method is a call like any other

## Status

Accepted (cycle 171, Gap R.41). The value-representation half of the same claim — a method returning
a `str` — is recorded as Gap R.42 and verified as pre-existing, not fixed here.

## Context

Three things were missing from `emitClassMethod` that `funcDef` has always had, and each produced a
different wrong interface for the same reason: methods were emitted by a code path that was written as
an afterthought to classes and never brought back to parity with functions.

```gusty
class C:
    def m(self) -> int:
        try:
            return 3
        finally:
            print("m fin")     # compiled: exit 2, `br label %` — an empty branch target

class D:
    def raiser(self) -> int:
        raise ValueError("boom")

print(D().raiser())            # compiled: prints 0 and exits 0; CPython: traceback, exit 1
```

Measured before the change, and re-checked afterwards against CPython:

| shape | compiled before | CPython |
|-------|-----------------|---------|
| `try`/`finally` in a method | exit 2, `ret`/`br` to an empty label | `m fin`, `3` |
| `try`/`except` in a method | exit 2, same | `handled`, `4` |
| `break` through a method's `finally` | exit 2, same | `fin`, `9` |
| `raise` out of a method, uncaught | **prints `0`, exit 0** | traceback, exit 1 |
| `raise` out of a method, caught by caller | **prints `0` then the arm** | just the arm |
| a nested `self.bad()` that raises | **prints `0`, arm never runs** | the arm |
| a construct codegen refuses, inside a method | **exit 2** (half a function emitted) | — |

The empty label was `g.funcRaiseExit`, which `funcDef` sets and the method path never did. The silent
`0` was two things at once: no unwind target, and — worse — **no call-site exception check**, so a
method call looked at nothing; the exception flag was still set, but the caller had already taken its
value and moved on. The refusal case was the sneakiest: `emitClassMethod` called `g.stmt` and threw
the returned error away, so any construct the compiler refuses inside a method produced a truncated
function and an `llc` rejection — exit 2, "the compiler is broken", for what is a source the front end
should name (ADR 0166's rule).

## Decision

**Emit a method the way a function is emitted.** `emitClassMethod` now:

- sets `g.funcRaiseExit = funcName + ".raiseexit"` and emits that block, closing the root frame the
  prologue opened and returning — so an unwind out of a method is a return like any other (and does
  not leak a GC frame, ADR 0181);
- clears `g.handlerStack`, `g.handledArms` and `g.deferred` for the body, so a raise inside a method
  cannot branch into the handler of whatever was being emitted when the class happened to be
  registered, and a `return` in a method cannot run an enclosing statement's deferred bodies;
- names the traceback frame `Class.method` (`g.curFnSrc`), like `funcDef` names a function;
- records the first `g.stmt` refusal in `g.emitErr`, and `GenerateIR` returns it — a refusal inside a
  method is a compile error (exit 1), never a half-built module for `llc` to reject (exit 2);
- and every call site into program code checks the flag: the statically-known dispatch, the
  `super().m()` dispatch, the class-id `switch` dispatch, and the `__init__` call of a constructor.

Two details in the dispatch check are the difference between correct and invalid IR. Inside a
`switch` arm the check must complete the arm's terminator, so `checkExnLabel` returns the
continuation block and the arm ends in it; and the join's `phi` must then name that continuation as
its predecessor block, not the arm — a call site that can raise cannot also be a value producer for
the join. That is why `checkExn` grew a label-returning form instead of being duplicated.

## Agentic rationale

A method call that silently returns `0` after a raise is the worst failure in this table: exit 0, a
value printed, and no diagnostic — an agent reading the run would conclude its program worked. Exit 2
for a source error is the second-worst, because it points the reader at the compiler. Both are now
gone, and the assertions are on artifacts: `pkg/lang/method_unwind_test.go` requires the method's own
`gy_C_m.raiseexit:` block, forbids a branch to an empty label, requires an `@exn_flag` load after a
method call, requires `rt_frame_close` on the unwind path, and requires `VerifyModuleIR` to pass; the
refusal case asserts that a construct inside a method yields a compile error naming itself.
`programs/method_try.gy` (five shapes, `deferred_bodies` next to it) is a standalone parity program
whose CPython file is byte-identical on both legs.

The exit codes are asserted from the table in `docs/operations.md`, not from CPython: an uncaught
raise out of a method is class **3** on both backends, and a construct the compiler refuses inside a
method is currently class 1 compiled vs class 3 interpreted — that asymmetry is Gap R.37 and the test
names it rather than averaging it away.

## Codegen / IR implications

- Method bodies are emitted into the globals buffer with their own function scope; the four registries
  above are saved and restored around it, exactly as `funcDef` does.
- `phi` incoming blocks follow control, not definitions — see the dispatch note. Any future call-site
  check added inside a `switch` arm must go through `checkExnLabel`.
- What is still not right, recorded with a minimal repro and checked against two pre-change binaries:
  a method that returns a string returns the raw `@.strN` global in an `i32` slot
  (`ret i32 @.str1`, `class Dog: def sound(self) -> str: return "woof"`), so `llc` rejects the module
  and the run exits 2 where the interpreter and CPython print `woof`. `funcDef` solved this for
  functions by interning on return (ADR 0174); the method emitter never got it. Gap R.42.

## Alternatives rejected

- **Give methods a shared raise-exit with `main`.** Rejected: the method's frame is its own; branching
  to `main.raiseexit` unwinds frames that are still live and reports the raise as if it happened in
  module code (a unit test now forbids exactly that string inside the method body).
- **Keep the emitted error in a log and let `llc` report the problem.** Rejected by ADR 0166's rule: a
  source error and a compiler bug must never look alike, and exit 2 is reserved for the latter.
- **Skip the constructor check** (`__init__` "canonically" cannot raise). Rejected: `Q(0)` raising
  `ValueError` is ordinary input validation, and without the check the half-built instance was handed
  to the caller as if construction had succeeded.
- **Fix the string-returning method here too.** Rejected under one-commit-per-feature: it is a value
  representation (ADR 0174's territory), not control transfer; Gap R.42 carries the repro.
