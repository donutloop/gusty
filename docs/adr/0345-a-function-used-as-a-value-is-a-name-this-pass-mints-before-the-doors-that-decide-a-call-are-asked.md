# A function used as a value is specialised into names before the doors that decide a call are asked

Status: accepted
Date: 2026-10-15
Related: ADR 0340 (the ordering builtins' `key=` road, which calls a *named* callable and refused a lambda for
having no name), ADR 0344 (the container-answer scan's declined half, which this pass can now fill by giving
the appended value a name), ADR 0333 (the container-answer scan), ADR 0284 (`cb: function` /
`Callable[...]` — the declared-type road for a parameter that is called, which this pass deliberately does not
enter), ADR 0283 (a declared name is not a variable), ADR 0320 (a rewritten call still reports the call the
program wrote), ADR 0273 (a scan that opens a door must not speak over the road that named what it could not
carry), ADR 0271 (arithmetic does not read a payload's tag), ADR 0245 (the nested-def lift, whose two passes do
not compose with this one yet), ADR 0232 (one question asked once), ADR 0166 (a refusal names what is missing,
at exit 1), ADR 0181 (the rooted slot an answer travels in), ADR 0340's determinism rule, roadmap L11.7 (this
row), roadmap Gap R.255 (the `key=` shapes still refused).

## Context

One backend decides what a program means, and it decides a call by looking at the *name* in the callee slot.
That is the whole reason a function used as a value has been refused for the backend's entire life:

```text
def apply1(fn, v):
    return fn(v)          # codegen: "fn" is called but is not a function this backend can build a call to:
                          # the reference asks the value what it is at the moment of the call and raises
                          # TypeError (`'int' object is not callable`) … (roadmap L11.1, ADR 0166)
```

The sentence is honest and the answer is still wrong-by-omission: `apply1(inc, 41)` is a program CPython runs,
the retired engine ran, and the record has answers for. Every door downstream of the callee — the ordinary call
road, `call_key.go`'s keyed comparator, the container-answer scan, the float/text return scans, the pair road's
arity — asks its question of a callee it can name. A lambda has no name. A name bound to a function is a
variable holding a handle. So each door had its own "cannot build a call to this", and the shapes they could not
answer were the same shapes, seen from seven directions.

Two facts decide this ADR.

The first is that the program's *own* choice of function is a compile-time fact in almost every program that
gets written: `twice(d2, 3)` says which function `fn` is, and says it at every call site, and the body only ever
calls it. The second is that the backend already has everything needed to act on that fact — a `FuncDef` is a
value in the AST, the call road already calls a named function, and the clone is a definition, not a new
lowering. What was missing was a place to rewrite the tree *before* the doors read it.

## Decision

`pkg/lang/callable_value.go` is a pass, `defunctionalizeCallables`, called as the first act of
`GenerateIRReport` — before `resolveImports`, before the container-answer scan, before any whole-program read of
the tree that is about to change:

```go
func GenerateIRReport(prog *Program, opts *IRGenOptions) (string, *DebugInfo, error) {
	prog = defunctionalizeCallables(prog)
	imports, err := resolveImports(prog, moduleSearchFor(opts))
	...
```

1. **Read, then plan, then rewrite — three functions, and only the third mutates.**
   `callableReadProgram` walks the tree with reflection (`callableWalk`, `callableWalker`) and records the
   program as data: every call, every name occurrence and who called it, every binding, which names are called
   and where. `planDefunctionalization` turns that data into a `callablePlan` or returns a `*callableDecline`.
   `applyDefunctionalization` copies the tree (`copyProgramAST`, deep through `copyASTNode`/`copyASTValue`) and
   writes the plan into the copy. The caller's tree is never touched, which is what makes a decline free.

2. **A function whose parameter is called is cloned once per distinct tuple of functions the call sites ask
   for.** `twice` is cloned for `(d2)`: the clone's body is the source body with the callee of each call of the
   parameter replaced by the named entry, and the original definition retires (`FuncDef.SkipEmit`) so the module
   carries one definition per thing the program can actually call. A clone whose entry table disagrees with
   itself — a parameter called with two different arities, an entry declared with a different arity, a body that
   calls its parameter where some site passed a function of another shape — declines the whole program rather
   than answer one call wrongly (`callableInnerAgrees`).

3. **A lambda that needs a name is given a declaration.** Three positions: a lambda bound to a module-level
   name (`h = lambda x: x + 1`), a lambda in an argument position of a call this pass does not otherwise own
   (`sorted(xs, key=lambda x: 0 - x)`, `max(xs, key=...)`, `min(...)`, which is ADR 0340's key road finally
   being handed the name it asked for), and a lambda passed to a parameter this pass specialises. A lambda is
   hoisted only when it is *self-contained* — its body reads nothing but its own parameters — because a hoisted
   body that read an enclosing variable would be a different program at the new place (`callableFreeNames`,
   `callableLambdaIsSelfContained`, `callableLambdaIsClosed`). Lambdas that capture stay where they are, and the
   closure road's refusal stays the answer.

4. **A name that only ever aliases a function is retargeted.** `g = d2` followed by `g(4)` becomes a call of
   `d2`, provided `g`'s whole history is that binding plus calls: `g` bound exactly once, never rebound, never
   read in a position that is not a call. `print(g)`, `xs = [d2]` and `g = 3` after the alias each defeat the
   rule, and the program keeps the ADR 0283 refusal it always had (`callableBoundExactly`,
   `callableBoundOnlyByDef`, `nameIsOnlyCalled`).

5. **A decline is the caller's program, unchanged.** `defunctionalizeCallables` returns `prog` itself when the
   plan declines — no copy, no partial rewrite, no new refusal. That is why the pass can run first: everything
   downstream answers the program it would have answered, and the refusal a reader sees is the honest one that
   was already written (`ADR 0273`'s rule, applied to a rewrite instead of a scan).

6. **What a rewrite names is decided once.** A clone reports the function the program wrote (`FuncDef.CloneOf`
   → `callableDisplayName`), so a traceback and `--debug-info` say `twice`, not `twice$callable$1a2b3c4d`;
   a hoisted lambda reports `<lambda>`, which is the reference's own word for an anonymous function however it is
   bound — `g = lambda x: x` / `g(1, 2)` gets `too many arguments for <lambda>`, CPython's sentence. Both go
   through `callableSurfaceName`, and the minted spelling appears nowhere a human reads.

7. **A rewrite is a function of the source.** Names are minted from a node's *ordinal in the plan*
   (`nodeSeed`), not its address: `%p` on a Go pointer is randomised per process, so an address-seeded symbol
   gives one program two different modules across two runs, and the golden record, the lit tests and every
   cached artifact read those symbols. Iteration is over `plan.order` and sorted keys, never over a map.

8. **The pass accounts for itself.** `gustyc --callables <src>` prints the pass's own report — `rewritten`,
   `clones`, `hoisted_lambdas`, `rewritten_call_sites`, `higher_order_functions`, `symbols`, and the `refused`
   sentence with its roadmap citation — described in the schema as `definitions.callablePassReport`, so an agent
   can ask why a program was not rewritten without reading the pass.

## Agentic rationale

An agent driving this compiler needs three things from a rewrite pass, and each has a machine path here:

* **Ask what happened.** `--callables` is the pass's account of one source; `--schema` describes its document;
  `refused` carries the decline sentence, which cites the row that owns the shape. A tool that rewrote a tree
  silently is a tool whose output cannot be debugged without its source.
* **Ask why a program is still refused.** A decline leaves the *original* refusal in place, whose sentence names
  the missing half and the row (ADR 0166). The pass's own decline text is a separate document field, so the two
  are never confused for each other.
* **Expect the same bytes.** `hash` in the report, and a determinism row in the tests that compiles every probe
  twice and compares. Gap R.162 was filed against exactly this property on another pass; the counter that minted
  names from a pointer address is the same bug shape.

The pass deliberately has no "best effort" mode. A partial rewrite would mean the doors answer a program that is
neither the source nor a rewritten one, and the failure would be silent.

## Codegen / IR implications

* Specialised definitions carry the program-owned prefix like any user function: `gy_twice$callable$<hash>_0`,
  `gy_sorted$lambda$<hash>_0`. The suffix is invisible in diagnostics and in `--debug-info`'s `Name`.
* The retired original is not emitted (`funcDef` returns at `fd.SkipEmit`), so no call can reach an unspecialised
  body and no orphan symbol can have its name taken by the clone that replaced it.
* The call the clone writes is an ordinary direct call — `call i32 @gy_d2(i32 %t…)`. No `fnptr` operand, no
  `inttoptr`/`ptrtoint`, no indirect `call`. Two test tables forbid the casts across the whole probe set: the
  handle-as-a-number call is the exact shape the bug had before this pass, and it must not come back.
* The hoisted lambda for a `key=` hook is called by ADR 0340's comparator, which already knew how to call a named
  callable; that door changed not one line.
* A container slot filled by a hoisted lambda's call is a value the container-answer scan can follow, which is
  the boundary ADR 0344 drew: this pass shrinks the declined half by giving the appended value a name.
* `GenerateIRReport` is the single entry the pass is wired into, so `--emit-llvm`, `--verify`, `--build`,
  `--eval`, `--repl`, `--debug-info` and the conformance harness all see the same rewritten tree. There is no
  second engine to keep in step (ADR 0302) and no flag to opt out of the rewrite.

## Alternatives rejected

* **A `fnptr` value word plus an indirect `call` (the row's original "Next concrete action").** Rejected because
  it makes the callee a number in a slot, which is exactly what ADR 0166 forbids the rest of the backend from
  printing and what ADR 0271 exists to keep out of arithmetic: an `i32` handle passed as a function would answer
  `print(g)` with what the allocator handed out, and the whole "a value carries its own kind" argument (L11.1)
  says the indirect-call road needs the tagged word first. Defunctionalisation answers the *called* cases, which
  is the subset both witnesses agree on, without inventing a word for the *unreadable* ones.
* **Teaching each door its own lambda shape.** Rejected after seven `cannot build a call to` sites were counted:
  the doors would keep disagreeing about what a lambda is, and every new door must re-derive it. The pass asks
  the question once (ADR 0232's shape, applied to a rewrite).
* **Rewriting after the whole-program scans.** Rejected: the container-answer scan, the float/text return scans
  and the pair road's arity all read the tree this pass changes. A rewrite that arrives late is a second opinion
  about a program they already answered, and the disagreement shows up as an answer that depends on pass order.
* **Rewriting what the pass can and leaving what it cannot, inside one program.** Rejected as unsound: a partial
  clone can call a parameter that some other call site filled with a different function, and the arity audit is
  only valid program-wide. All-or-nothing is also the only way a decline costs nothing.
* **Entering ADR 0284's declared-type road for `cb: function` parameters.** Rejected for now: that machinery
  decides the shape of such a call and its trap today, and a clone written over the top of it would answer the
  same program by two roads. `callableAnnotIsCallable` is the guard, and the decline says whose road it is.
  A plain annotation (`x: int`) is not a reason to decline, and the first draft's blanket ban on annotated
  parameters declined half the programs in the record for a reason this pass has no opinion about.
* **Naming a hoisted lambda after its binding.** Rejected against the reference: CPython's message for a lambda
  bound to `g` is `<lambda>() takes 1 positional argument but 2 were given`, and an arity sentence is the message
  a reader acts on.

## Consequences

* L11.7 moves from "every function-value shape is refused" to "the called shapes answer; the unreadable ones
  refuse in words" — `apply1(inc, 41)` → `42`, `twice(d2, 3)` → `12`, `g = d2` / `g(4)` → `8`,
  `sorted([3,1,2], key=lambda x: 0-x)` → `[3, 2, 1]`, `max`/`min` with a `key=lambda` likewise, and the two
  oracle probes that recorded this debt (`probe_fn_value`, `probe_fn_name`) are conformance rows.
* Two witness legs, both walked: the reference leg is the `want` column written from CPython and the CLI table
  in `integration/call_key_test.go`; the record leg asks `testdata/interpreter-golden.json` where the retired
  engine left a record, and skips where it did not — with a guard that fails if the probe set matches no record
  row at all, so a skip cannot quietly become the whole test. The two newly asserted corpus programs are recorded
  from the reference through `tools/recmerge`, which refuses an entry the compiled leg will not agree to.
* `--interp` and the retired engine are not involved anywhere: no probe, no expectation, no entry point.
* The pass is a new place a wrong answer could be manufactured, so the tables forbid the specific shape rather
  than trusting the intent: no `ptrtoint`/`inttoptr` in any probe module, a determinism row per probe, and a
  refusal table that fails if a program compiles clean where it must not.
* What stays refused, with a row each: a function value that is never called (`print(g)`, `xs = [d2]`), a lambda
  that reads its enclosing variable, a lambda that calls its own parameter (Gap R.265), a function returned from
  a function and called (Gap R.266), a higher-order function used as an entry (Gap R.267), a `lambda` with a
  default parameter (Gap R.263, a front-end parse error), and a hook lambda whose body asks a door the call site
  cannot answer (Gap R.264).
