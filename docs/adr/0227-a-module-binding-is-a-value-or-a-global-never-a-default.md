# A module binding is a value or a global — never a default

Status: accepted. Closes roadmap Gap R.35's compiled half; records Gap R.36 with the shape that
reproduces it; fixes Gap R.38's third untrue refusal. Cites: ADR 0220 (the module is a scope too),
ADR 0225/0223/0226 (the error-swallow family this adds a fourth member to), ADR 0166 (a refusal, not
an invalid module), ADR 0219 (citations must resolve).

## Measured before anything was decided

Fourteen module-scope shapes, run through CPython, the interpreter, and the compiled backend. Five
agreed; eight did not, and one of those eight was a silent wrong answer:

| shape | CPython | interpreter | compiled (before) |
| --- | --- | --- | --- |
| `MAX = 40` read by a function | 80 | 80 | refuses `undefined name "MAX"` |
| same read by a **method** | 5 | 5 | **prints `0`**, exit 0 |
| same read by a **nested `def`** | 9 | 9 | **prints `0`**, exit 0 |
| name assigned *after* the def that reads it | 7 | 7 | refuses |
| body shadows the module name (`K = 1` vs module `K = 5`) | `1 5` | `1 5` | **prints `5 5`**, exit 0 |
| `len(xs)` of a module list in a body | 2 | 2 | refuses `len of a non-string variable` |
| `xs.append(2)` on a module list in a body | — | — | refuses `string method append on non-constant string` |
| undefined name | raises, exit 1 | traps, exit 3 | refuses, exit 1 |

Two of those compiled columns printed an answer for a program whose answer is something else, and the
program exited 0. That is the one failure a compiler is never allowed to have, and it is the third
emitter found this session doing the same thing: `emitClassMethod` (ADR 0223), `truthyValue` (ADR
0225), `valueText` (ADR 0226), and now `emitClosureDef`, whose body loop read

```go
for _, st := range fd.Body { g.stmt(b, st) }   // error discarded
```

so any statement that failed to lower cut the body short and the function fell through to a
`ret i32 0`. A closure that reached for a module name — a name the capture set had dropped, because
`closureInfoFor` only keeps names present among the enclosing parameters and locals — answered 0.

## Decision

**A module binding is reachable from a compiled body in exactly two shapes, and a third shape is an
error. Nothing is defaulted.**

1. **A literal the module never rebinds is a value.** `moduleEnvFor` collects the names bound exactly
   once, at module level, to an `IntLit`/`StrLit`/`BoolLit`/`NoneLit`, that no module-level statement
   rebinds (assignments inside a body do not count — they are that body's own, ADR 0220). Such a name
   cannot change, so a body reading it is given the value. `MAX = 40; def twice(): return MAX * 2`
   compiles to a multiply by 40 and prints 80.
2. **What the module rebinds is state, and state lives in a global.** `moduleSlotNames` gives such a
   name a `@gy_mod_<name>` global that main stores and any callee loads, and it is read at the point
   of use — which is what "resolve the name when the call runs" means once frames are gone:
   `LATE = 0; def read(): return LATE; LATE = 3; print(read())` prints **3**. A frame alloca could not
   do this: main's `%_LATE` is dead by the time the callee executes, which is precisely why the old
   code had nothing honest to offer.
3. **Anything else refuses, with the true reason.** Containers stay refused on purpose — handing a
   body a module list's heap handle without the container operations behind it would trade an honest
   refusal for a half-working answer. Float-valued module names stay refused too (their slots are
   `double`s, and L11.6 owns the float story).

Plus the rule that makes 1 and 2 safe: **a binding inside a body is the body's own**, decided by what
the body binds *anywhere inside itself* (`enterBody`/`collectLocals`) and not by the order slots happen
to be allocated in. Without that, `def f(): K = 1; return K` next to a module `K = 5` answered `5 5`.

## The one exemption, said out loud

A closure nested inside a function **used as a decorator** still emits an unlowerable body without
refusing. That is not a swallow: a decorated call runs the trampoline (`emitDecoratedFunc`'s `_impl`),
whose own body reports its failures with full propagation, so the closure object here is unreachable
and refusing would break programs that work today (`@add1 def f` printing 7). The deferred failure is
written into the module where a reader and the tests can see it:

```llvm
; note: closure wrap: body not lowered (codegen: unsupported call "g"); a decorated call runs the trampoline instead
```

## Diagnostics: the refusals stopped lying

The two sites that handled a body's read of a module container answered `len of a non-string variable`
and `string method append on non-constant string` about a **list** — Gap R.38's third instance, and the
most damaging kind, because it sends the reader to a line they never wrote. `moduleStateErr` now
checks whether the name is module-bound first and, if so, says what is actually missing:

```
codegen: "xs" is bound at module level, and a compiled function body cannot reach module-level
containers or state that changes (the interpreter answers this program; compiled module globals are
roadmap Gap R.35)
```

Every refusal message that claims what the interpreter does is now derived from a gate that knows what
the interpreter does; the "the interpreter reports the same error" sentence is kept only where the
interpreter really does report that error (an undefined name), and replaced by a claim about this
program where it does not.

## Gap R.36, measured and pinned rather than fixed here

The same "unwritten slot" bug has a second, still-open face, found while probing this family:

```python
def f(c):
    if c:
        x = 1
    return x
print(f(True))    # 1 everywhere
print(f(False))   # CPython: UnboundLocalError, exit 1; interpreter: traps, exit 3; compiled: prints 0, exit 0
```

CPython raises; the interpreter traps (with a class Gap R.39 owns); the compiled backend reads the
unwritten alloca and prints 0. `programs/probe_unwritten_slot.gy` + `TestUnwrittenSlotIsGapR36` pin
it, and the fix — a definite-assignment rule in the checker, extended past `IfStmt` — is its own cycle.
It is listed here so a reader of this ADR does not mistake the closure fix for the whole gap.

## Alternatives rejected

- **Keep refusing every module read from a body.** That is what shipped, and it refused programs
  CPython runs while printing 0 for two others. A value that cannot change does not need a slot to be
  read from; refusing it was conservatism about a thing that is not conservative.
- **Point the callee at main's allocas.** Unsound: the frame is dead when the callee runs. This is the
  tempting wrong fix, and the reason the rule is "global or value".
- **Capture module names in the closure environment instead.** Works for closures that exist, but the
  read must still happen at call time for a *named function*, and env captures carry their own
  representation questions (L11.7). A global is the shape module state actually has.
- **Emit `ret 0` and log a warning, as before.** Three emitters did this. A warning nobody reads plus a
  plausible answer is worse than a refusal, and the module comment now carries the one case where the
  body genuinely does not run.
- **Fold every module name into its last literal value.** Wrong whenever the module rebinds between the
  def and the call, which is exactly the call-time case CPython gets right.

## Verified by

`pkg/lang/module_slot_test.go` (the constant table, the global for a rebound name, the local-beats-module
rule, the two honest refusals, the closure body that no longer defaults to 0);
`integration/module_scope_test.go` (`TestModuleScalarsReachCompiledFunctionBodies` — CPython-checked
expectations, module verifier, compiled output; `TestUnwrittenSlotIsGapR36` names its own deletion);
`programs/module_scope_in_functions.gy` (both legs `80 7 5 40 1`) and
`programs/module_calltime_lookup.gy` (both legs `3 1`) are oracle match rows — the first of these was
this family's debt row until the compiled leg paid it; `programs/probe_unwritten_slot.gy` is the pinned
divergence. `TestRecordCitationsResolveToRealPrograms` caught the two records still pointing at the
renamed probe.
