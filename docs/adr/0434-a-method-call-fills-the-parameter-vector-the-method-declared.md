# ADR 0434 — a method call fills the parameter vector the method declared: one word per declared parameter, with the defaults and the names read from the declaration

## Status

Accepted, 2026-10-27. Closes **roadmap Gap R.472**, filed by **ADR 0432**'s generator-method round as the shape
whose buffer slots could not be filled because the *call* never handed the body a word. Files **Gap R.479** (the
doors that read a parameter's KIND — the text census, the verdict census, the heap-container registration — are
keyed on the arguments the call *wrote*, so a parameter filled only from its default arrives with the right word
and the wrong kind) and hands one shape back to **Gap R.474** (a container default the fold turned into a module
global is the same literal-in-an-operand-list rejection, reached a second way). Depends on **ADR 0166** (a wrong
answer at the exit code of success is the artifact class this contract ranks worst, and a refusal must be a refusal
in words), **ADR 0029** (no path emits IR it cannot name), **ADR 0224** (a call site asks the walk that minted the
symbol — `resolveMethodDef` beside `resolveMethod`), **ADR 0232** (one question, one reading: the vector is laid out
in one function and the five roads ask it), **ADR 0302** (both witness legs, never two engines), **ADR 0421**-style
reading of a declaration rather than a spelling, and **ADR 0181**/**ADR 0227** (a binder is stack-scoped to one
call site, as every per-body registration in this compiler is). Supersedes nothing.

## Context

```python
class C():
    def m(self, a, b=2):
        return b

c = C()
print(c.m(5))
```

prints, on the build this row opens:

```
0
```

at exit **0**. The reference prints `2`. And the keyword spelling is worse rather than better:

```python
class D():
    def m(self, a=1, b=2):
        return a * 10 + b

print(D().m(b=7, a=3))
```

prints `73` where the reference prints `37` — the two values travelled to the callee in the order the *source wrote
them*, so the digits arrived swapped inside a number the program itself supplied. `class E():` /
`    def m(self, a):` / `        return a` called as `E().m(1, 2, 3)` printed `1` at exit 0 where the reference
raises `TypeError: E.m() takes 2 positional arguments but 4 were given`: the extra words were emitted and never
read. Five programs, five exit-0 answers, none of them the program's.

The cause is one idiom in five places. Every road that emits a method call built its operand list from the
*call site*:

```go
argRegs := make([]string, len(c.Args))
for i, arg := range c.Args {
        av, err := g.value(b, arg)
        …
        argRegs[i] = ", i32 " + av
}
```

— the static-receiver dispatch and the `super()` dispatch in `pkg/lang/codegen_method.go`, the run-time class-id
switch in the same file, the `mod.fn(...)` road in the same file, and `callInstantiate`'s constructor call in
`pkg/lang/codegen_instance.go`. Meanwhile the callee is emitted by `emitClassMethod` (`pkg/lang/codegen_class.go`)
as

```
define i32 @gy_C_m(i32 %self, i32 %p1, i32 %p2, …)
```

with one word per entry of `fd.Params`, and its body binds `fd.Params[i].Name` to `%p<i>` for `i ≥ 1`. The callee
reads its parameters **by position**; the caller was counting **the arguments the source wrote**. An omitted
parameter's word is therefore whatever the register allocator left in that slot, and a `name=` keyword is a
positional with a label on it.

What makes the whole class of bug silent is the assembler's tolerance, and it is worth writing down precisely
because it is the reason no layer between here and the object file warns anyone: an under-filled operand list is
**not** malformed IR. `llc-20` accepts the module for the program above, and so does `opt -passes=verify`
(`gustyc --verify-llvm` prints `module verified` for it). There is no exit 2 to wait for, no diagnostic to read,
and no test that fails until somebody asks the program the right question.

The module-`def` road has not had this problem. `callUserFunc` (`pkg/lang/codegen_func.go`) has walked
`provided`/`pos` over `fd.Params` since **ADR 0166**: one word per *declared* parameter, defaults filled, keywords
bound by name, and the shapes it cannot lay out refused in words with sentences like `too many arguments for %s`,
`unknown keyword argument %q for %s`, `multiple values for argument %q of %s`, `positional argument after keyword
argument for %s` and `missing argument %q for %s`. The method roads are separate roads, and no one had asked them.
That asymmetry — one question answered in one door and guessed at in five — is the shape **ADR 0232** exists to
protest.

## Decision

**A method call's operand vector is the callee's parameter list, and it is read from the callee's declaration.**

1. **One function answers the question.** `pkg/lang/codegen_args.go` NEW holds `callArgBinder` and its
   `callArgs` walk — the same shape `callUserFunc` walks, written once so the five roads and the def road cannot
   drift: positional arguments fill slots in order after the caller's own `skip` leading slots (1 for a method,
   whose slot 0 is `self` and arrives in the receiver word the caller wrote itself; 0 for an imported module
   function), `name=` keywords bind against `fd.Params[i].Name`, and every slot the call left empty is filled from
   `fd.Params[i].Default`. `methodCallArgs` and `plainCallArgs` are the two entries; `operandWords` renders the
   vector as the operand tail so no road assembles a call line by hand again.
2. **A default travels the identical word a written argument would have travelled.** The default expression is
   handed to the *same* `g.value` the road hands its own operands — not to a separate constant-fold, not to the
   module-`def` road's float box. Whatever a road does with a written text it does with a text default; whatever it
   does with a written float it does with a float default. A default the road could not have written is a default
   this road cannot fill either, which is a refusal rather than a second guess. This is what keeps the row narrow:
   no new value representation is introduced, only a new place the existing one is reached from.
3. **The refusal sentences are the def road's, reused verbatim.** `too many arguments for C.m`,
   `missing argument "b" for C.m`, `unknown keyword argument "k" for C.m`,
   `multiple values for argument "b" of C.m`, `positional argument after keyword argument for C.m`. One mis-shaped
   call says the same thing about itself whichever door it came to; a new paraphrase would be a second table of the
   same rule (**ADR 0232**), and `self=` is refused for free by the same walk because slot 0 is the caller's and is
   not in the name space a keyword may bind.
4. **The declaration is read by the walk that minted the symbol.** `classInfo` gained `methodDefs`, filled by
   `registerClass` beside `methods`, and `resolveMethodDef` walks the same base chain in the same order as
   `resolveMethod` (**ADR 0224**'s rule: a call site asks the thing that minted the symbol, so definition and call
   site cannot disagree about which declaration a name denotes). An inherited method's defaults therefore come from
   the class that *declares* it, and an override's from the subclass — measured both ways in the answered family.
5. **The run-time class-id switch builds every arm's vector before the switch.** The dispatch hands one call
   expression to several `define`s, so the operand words are per-arm (each arm's own method declares the
   parameters, and two arms with two different defaults is an answered row, not a hypothetical). They are emitted
   before `switch` is written, from one binder shared by the arms: an operand emitted inside one arm and named in
   another is a use of a register whose block did not run, which the verifier *would* reject — **Gap R.318**'s rule
   that no path emits IR it cannot name, caught here before it was ever emitted.
6. **An unresolvable declaration keeps the old words.** With no `fd` to read an arity from, the caller emits what
   it emitted before this row: filling a vector for a signature the compiler cannot see is the guess this file
   exists to stop, and a fallback that guesses is how a fixed shape comes back as a rephrased bug.
7. **The refusals go where the def road's refusals go.** They are compile-time errors (exit **1**, `--json`
   `phase: codegen`), not traps: the roads that previously emitted `unreachable` + a runtime `unsupported:` trap for
   an unresolvable callee are untouched, and an arity error is not a capability, because it is not a shape the
   backend declines by design — it is a call that does not match its callee.

## Consequences

* **Twenty programs came over from this backend's answer to something else's** between a `git worktree` build of the
  parent (`bf03c37`, **ADR 0432**) and this commit, across **25** deduplicated sources — **20 moved, 5 held**
  byte-identical, the held five being the verdict default and the four shapes that were already refused or already
  wrong for another row's reasons. **17 arrived at the reference's bytes exactly**: eleven value programs — the
  written-out default (`0` → `2`), defaults on either side of a written argument (`9257725` → `111`), the keyword
  swap (`73` → `37`), a keyword beside a positional (`608912130` → `16`), a constructor's default (`0` → `2`), an
  inherited default (`2` → `4`), an override's own default (`1 2` → `4 5`), the two dynamic arms (`11 11` → `4 6` —
  different numbers per arm, which is the evidence the vector is per-arm), `super()` reaching the base's default
  (`20` → `40`), a generator method's omitted default (`7 0` → `7 3`) and a buffer summed through a default
  (`7` → `10`) — the corpus program, and **five** shapes that moved from a wrong answer at exit **0** to a refusal at
  exit **1** at the exit code the reference's own `TypeError` exits with (too many arguments, a required parameter
  left empty, an unknown keyword, `self=` at a method, and the constructor whose argument the program never passes).
  **3 moved and did not arrive**, each named for the row that owns it: the defaulted text
  (`hi 4` → `hi 0`, still the intern position — **Gap R.479**), the float default (`8` → `4`, both wrong against
  `5.0` — the method's missing double return word, **Gap R.35**/**L11.6**) and the container default (a fabricated
  `4` at exit 0 → the assembler's `global variable reference must have pointer type` at exit **2**, **Gap R.474**).
* **`llc` and the verifier will never tell me this.** The under-filled call is accepted by both. A row of this
  class can only be caught by asking the program, which is why every answered row in this file is run against a
  live `python3` inside the case rather than against a remembered string, and why the record leg is an assertion
  rather than a comment.
* **A default's KIND is not carried by the vector, and that is a new row.** `strParamOf` (**ADR 0366**),
  `boolParamOf` and `declareHeapParams` decide a parameter's kind from the arguments the call *wrote*; a parameter
  that no call writes is not in their evidence. So `def m(self, s="hi")` + `return s` prints `0` at the method's door
  while the same body as a module `def` prints `hi` on this build, `def greet(self, who="world")` +
  `print(f"hi {who}")` prints `hi 0` — the `@str_tab` position — and `def m(self, xs=[1, 2, 3])` + `return len(xs)`
  refuses with `len of a non-string variable`. Filed as **Gap R.479**, with today's bytes pinned beside the
  reference's in both test files. This is the same "second door behind the first" shape **ADR 0432** recorded: the
  vector now fills the slot, and the doors that read a slot's kind are still reading the call site.
* **One claim in an existing owed table was the retired engine's, not the reference's.**
  `pkg/lang/method_text_test.go` pinned `class C:` / `    def __init__(self, v):` / `        self.v = "hi"` /
  `…` / `print(C().get())` as "the reference prints `hi`, this backend prints `0`". It does not: CPython raises
  `TypeError: C.__init__() missing 1 required positional argument: 'v'` and exits **1** — the value `0` was this
  backend's, and the `hi` was what the retired engine's record held for a call it allowed with the parameter
  unbound. The row moved to that file's refusal table, where the guard is that it *stays* a refusal, and the
  compiled leg now refuses at exit 1 beside the reference's exit 1. Where the record and the reference disagree,
  the reference is right and the record is the debt (**ADR 0302**); a pinned `reference` column has to have been
  asked.
* **A default marks a parameter that may be omitted and says nothing about the parameters around it.**
  `class C:` / `    def m(self, a, b=1, c):` / `        return a + b + c` / `z = C().m(1, 2, 3)` answers `6` — the
  default fills one slot, the positional list keeps counting — and the record states it (repr `6`, type `int`).
  CPython cannot witness that row, because its parser rejects the `def` (`non-default argument follows default
  argument`); the record is the leg, and the case says so rather than pretending to a reference it cannot have.

## Alternatives rejected

* **Fill the vector at the callee instead** — have `emitClassMethod` write each omitted parameter's default into
  its own slot. Rejected: the callee cannot see which of its parameters a given call omitted, so this would
  evaluate a default on every call — a text default interning on every call, a container default allocating on
  every call — and a body whose default reads a name would read it from the callee's scope instead of the call's.
  The argument vector is a call-site fact.
* **Carry argc into the method ABI and let the callee fill defaults**, the way `callUserFunc`'s entry road does.
  Rejected: it changes the calling convention of every method in every module — the define, the three dispatch
  roads, the dynamic switch, the closure road and every lit-style IR pin in the suite — to move a rule that lives
  at the call site. The pair road's tag word beside a parameter is precedent that this ABI grows one word at a
  time when a *caller* needs it, not when a callee wants a service.
* **Keep emitting one word per written argument and pad the tail with `i32 0`.** Rejected: it is the same
  zero the row exists to remove, written politely, and it would make the keyword swap *worse* — the defaults would
  then be unreachable by construction.
* **Reuse `callUserFunc` itself** rather than a binder. Rejected: that function is the module-`def` road, and its
  walk is entangled with the pair door (`pairArgWords`, `tagWords`), the float-double road, `applyStrFill` and the
  container-answer gates, none of which a method's flat `i32` parameter list participates in. Extracting the
  arity walk and leaving the doors where they belong is the split; forcing the method roads through the def road
  would either refuse every method the pair gates refuse or require the gates be made caller-shaped.
* **Answer the too-many-arguments shape by dropping the extra words**, as the road did before: rejected — the
  reference raises there, and this contract answers a program the reference refuses with a refusal in words, never
  with the program's own digits rearranged.
* **Refuse a float default (or a container default) instead of lowering it** through the existing operand road:
  rejected while the same road still answers the written-argument spelling. Both are filed (**Gap R.35**,
  **Gap R.474**/**Gap R.479**) with the door named, and a refusal invented here would be a new capability the def
  road does not have, for a shape that differs from a written argument only in where the expression was written.
* **Widen the kind censuses in the same commit** so a defaulted text parameter prints its text: rejected as two
  features in one commit (**AGENTS.md**, one commit per feature), and because the census is *evidence collected
  from call sites*, changing it to also trust a default expression is a decision about `strParamOf`'s soundness
  that belongs to **ADR 0366**'s row, not to this one.

## Machine path

* The refusals are ordinary codegen diagnostics, so the agent path is the one the def road already had:
  `gustyc --json --eval '…'` answers
  `{"backend":"aot","error":"jit: codegen: missing argument \"b\" for C.m","exit":1,"ok":false,"phase":"codegen"}`.
  Every one of the five arity sentences is asserted in `integration/method_args_test.go` at exit **1** with the
  reference's exit code beside it, and exit **2** in a refusal row fails the case.
* `--capabilities` grows no entry. These are not design refusals; they are calls that do not match their
  callees, and the registry holds neither the def road's `too many arguments` nor its `missing argument` today.
* `integration/programs/method_arg_defaults.gy` NEW is the matrix program (row
  `programs/method_arg_defaults`, `asserted`, oracle `match`): 20 lines byte-identical to CPython, covering the
  omitted default, defaults on both sides, keywords out of order, a constructor, an inherited default, an override's
  own default, `super()` reaching the base's, a dynamic dispatch with a different default per arm, and a summed
  generator buffer as the control that a generator's slots come from the same vector.
* Tests: `pkg/lang/method_args_test.go` NEW (**11** answered programs each asserted on the compiled leg AND against
  a live `python3` in the same case, **1** record-leg row for the shape the reference's own parser refuses, **9**
  reading rows that ask `callArgs` DIRECTLY over hand-built `[]*Param`/`[]Expr` — which expression's register lands
  in slot `i`, plus the five refusals — **5** filed-not-fixed rows with today's bytes beside the reference's, and
  **5** refusal rows), `integration/method_args_test.go` NEW (**15** CLI programs each re-derived from a live
  `python3` inside the case, **1** record audit over the whole family, **6** exit-code rows, **1** assembler-rejection
  pin for **Gap R.474**, **5** CLI owed rows, **1** corpus/matrix check).
  `pkg/lang/testdata/interpreter-golden.json` **+16** sources recorded from the reference by `tools/recmerge` with
  the compiled leg cross-checked (**9835 → 9851**, none written by hand, none refused).
* The two **ADR 0432** pins that held this row open — the ladder row in
  `pkg/lang/generator_method_test.go::TestWhatAGeneratorMethodStillCannotReachIsFiledNotFixed` and its CLI twin in
  `integration/generator_method_test.go::TestWhatTheGeneratorMethodStillOwesAtTheCLI` — are DELETED rather than
  edited, and stand in the answered families (`generatorMethodFamily()`, `methodArgFamily()`).
