# ADR 0432 — a generator METHOD is a generator: the reading is the body's own question, taken at the class's door, and the doors that receive its answer ask one predicate

## Status

Accepted, 2026-10-27. Closes **roadmap Gap R.455**, the row **ADR 0429** filed from the battery that closed
**Gap R.452** and which **ADR 0430** and **ADR 0431** left standing. Files **Gap R.467** (a dynamically dispatched
SPLIT yield census, refused in words by this commit), **Gap R.468** (a yielded text, float and `None` keep no kind on
the way out of the buffer), **Gap R.469** (a `return` after the yields answers the void and the caller walks nothing),
**Gap R.470** (a `yield` that lives only in a `try`/`except`/`finally` handler is not read as making a generator),
**Gap R.471** (a dynamically dispatched container-returning method walks a counter — no `yield` anywhere, byte-identical
on the parent), **Gap R.472** (a method parameter the call omits answers `0`, not its default), **Gap R.473** (the truth
of a generator's answer is the heap word the buffer arrived in), **Gap R.474** (a container literal handed to a method
— or stored in an `__init__` — has its global emitted inside the `define` and `llc` rejects the module, byte-identical
on the parent's yieldless twin), **Gap R.475** (a generator body that walks its own parameter, refused in words by this
commit rather than answered empty) and **Gap R.476** (a method that FORWARDS a generator call answers a word that is
not the buffer). Depends on **ADR 0140**/**ADR 0081** (the eager-buffer generator the record holds), **ADR 0181** (the
frame rooting a buffer must survive a safe point), **ADR 0224** (a per-method fact is keyed by the mangled symbol),
**ADR 0232** (one question, one reading), **ADR 0302** (both witness legs, never two engines) and **ADR 0166** (a
refusal may name what it refuses and may not blame the program for where its body was written). Supersedes nothing.

## Context

```python
class C():
    def m(self):
        yield 1

c = C()
print(c.m() is None)     # the reference: False
                         # this backend: exit 1 — codegen: yield outside a generator function
```

beside

```python
def g():
    yield 1

for v in g():
    print(v)             # `1`, on this backend, today
```

The second program is the control that made the first a defect rather than a missing feature. The identical body
compiled when the front end could see that its function was a generator. The generator reading is
`containsYield`, and it was reached from the whole-program walk over `prog.Stmts` — the same boundary as
**Gap R.35**'s module-container scan and **ADR 0343**'s rule that a whole-program fact is decided before any IR
exists. That walk has never entered a `ClassDef`. So a `def` written inside a class body was a method by POSITION
alone: nothing ever asked its body the question, the flag stayed unset, and the `yield` statement road — which decides
`is this inside a generator?` by that flag — refused. The sentence blamed the program for the position its body was
written in, which is the one thing an honest refusal is not allowed to do (**ADR 0166**): no program can be written
that makes `yield` in a method legal, and the same five characters at module scope were legal.

The second half of the row was waiting behind the first. Making a method answer a buffer is not a `yield`-road fix:
it makes the ANSWER of a method call be a container, and four positions ask "is this a list?" of an expression — the
print door, the membership test, a binding, and the `for` road. Each of them asked it through its own arm, and every
arm matched `call.Fn.(*Name)`, the module spelling. A method call is spelled `recv.m(...)`, its callee is an `*Attr`,
and its symbol is minted by `registerClass` and handed out by `resolveMethod`. Left alone, the `for` road's generator
arm did not fire, the loop fell through to the numeric-range road, and `for v in c.m():` printed `0 1` for the
reference's `1 2` at the exit code of success — the artifact, not a limitation.

## Decision

**Take the reading at the class's door, and ask it once.** `registerClass` already walks `cd.Body` to mint every
method symbol; it now asks `methodIsGenerator(fd)` — which is `!fd.Async && containsYield(fd.Body)`, the SAME
predicate `funcDef` asks a module `def` (**ADR 0232**'s rule that a fact may not have two answers) — and records the
verdict in `g.genFuncs` under the **mangled** symbol (`gy_C_m`), because that is the key `resolveMethod` hands a call
site and the key `strFuncs` already uses (**Gap R.42** / **ADR 0224**). `async def` is excluded by position, not by a
second opinion: `effects.go` stops an async generator before codegen on both doors.

**Give the buffer to the frame that owns the body.** `emitClassMethod` allocates the runtime list with `rt_alloc` at
the top of the callee's entry block, stores it to a stack slot and registers that slot with `gcReg` before any body
statement runs (**ADR 0181**: an unrooted handle is one collection away from appending into a freed slot — the module
road's rooting, copied rather than re-derived), and answers the handle at BOTH exits a method body has: the ordinary
`ret` and the unwind edge, because a generator that trapped mid-body hands back what it collected, which is the `def`
road's arm (**ADR 0140** / **Gap D**) rather than this row's invention. The handle is per-body: `genHandle` is saved
and restored, so a `class` registered while an enclosing generator was in flight cannot inherit a buffer's register
name, and a method that does not yield inherits none.

**One predicate for every position that receives the answer.** `callIsGeneratorCall` answers the question for any call
the program can write — a `*Name` whose body the module walk registered, an `*Attr` whose receiver's class the
compiler can see (resolved through `resolveMethod`, so an inherited generator is read at the subclass's door too), and
the dynamic dispatch, where the answer is "a buffer" only when EVERY class that declares the name yields. The two
static call roads and the dispatch label the answer register in `listOperands`; the binding road
(`stmtAssign`'s `*Call` case) claims the name through `settleContainerKind`; `forIterCallIsGenerator` asks the same
predicate instead of matching a `*Name`; and the print door and the `in` door read the label the call road left. The
reading and the receiving are therefore one fact asked three times, and the IR-shape test asserts the buffer's
allocation, its two appends and its two `ret`s — and asserts them ABSENT from a method that does not yield, which is
the negative control that keeps this from being a road every method body walks through.

**Refuse the two shapes this row cannot answer, rather than answer them.** A dynamically dispatched call whose YIELD
CENSUS is SPLIT — some declaring classes yield, some do not — would join a buffer and a plain value into the one `i32`
the class-id `switch` produces, and the road receiving it chooses its helper from a kind the line does not state; the
door now refuses naming the register, the shape and the missing tag (**Gap R.467**,
`lower.unsupported.split-generator-method-dispatch`). The control that makes that a decision and not an oversight is
in the same test file: the identical program with no `yield` anywhere answered `0` at exit 0 on this row's parent and
still does (**Gap R.471**) — the join has never had a kind word, generator or not. The second refusal is a generator
body that walks one of its own PARAMETERS with a `for`: the loop classifies a Name iterable out of the tables the
module walk fills and a method parameter is in none of them (the keyed-call pass that gives a `def` its parameter's
kind is not reached by a method body), so the walk would run zero iterations and hand back the EMPTY buffer — `[]` for
`[1, 2]`, at the exit code of success — where the module twin answers correctly. It refuses
(`lower.unsupported.generator-method-parameter-walk`, **Gap R.475**), and the row is explicit that its parent refused
the whole program for the unrelated `yield`-reading reason, so the refusal is this commit's decision while the hole is
its predecessor's.

## What moved

Measured beside a `git worktree` build of the parent (`d729564`, **ADR 0431**) over **22** deduplicated sources — the
answered families, both owed tables, and the twin pairs:

- **18** moved, and every one of them moved OFF the `yield outside a generator function` refusal. **14** of those print
  the reference's bytes: the row's own `print(c.m() is None)` → `False`, the walk, the binding, the summed fold, the
  membership pair, the truth of a buffer that holds something, a loop in the body, both arms of a condition, a `while`,
  a field the body writes, a `match` arm, a `yield` inside an `except` handler, `yield from` a method's own answer,
  `self.` and `super()` and an inherited generator, and a dynamically dispatched call every arm of which yields.
- **4** held, byte for byte: the two programs **ADR 0429**/**ADR 0431** answered (`print(c.m() is None, c.m())`, and a
  method that prints then returns the void), `len` of a generator answer (the fold's standing refusal, identical to the
  module door's), and `bool` of a module generator (the `lower.unsupported.container-truth` refusal **Gap R.131** owns).
- **1** moved from that refusal to THIS row's refusal (**Gap R.475**), and **3** moved from it to an answer that is not
  the reference's, each pinned as the row that owns the fix: **Gap R.468** (a yielded text, float and `None` — and the
  module twin prints the same four bytes, byte for byte, which is what files it at the append door rather than here),
  **Gap R.472** (an omitted method argument answers `0`), **Gap R.473** (a generator's truth read from the handle word).
- **Nothing that ran on the parent now refuses.** `tools/recmerge` wrote **28** entries from the reference with the
  compiled leg cross-checked (**9786 → 9814**, none refused), and the two void-method pins that stood in the way are
  DELETED rather than edited, and stand inside the answered families.

## Alternatives rejected

- **Repair the module scan so it walks into `ClassDef` bodies.** Rejected: it keeps one fact in a walk that runs over
  statements and another in the door that registers a method's every other per-body fact (`strFuncs`, `methodVoid`,
  `methodCarry`, the field tables), and the two would drift exactly as **Gap R.42**'s symbol did. The reading belongs
  where the symbol is minted, under the key the dispatch speaks.
- **Answer the split dynamic census by claiming the majority kind, or by testing the register at run time.** Rejected:
  the first is **ADR 0166**'s artifact with extra confidence, and the second asks a word which kind it is, which is the
  question this backend has only ever answered with a tag the producer wrote (**Gap R.146**'s family). A refusal with a
  code and a suggestion is the honest half until **Gap R.471**'s tag exists.
- **Answer a parameter-walking generator body with the empty buffer and file the wrong answer.** Rejected for the same
  reason, and pinned in both directions instead: the refusal is the row's own, and the yieldless twin's silent
  `1`-then-stops is pinned beside it as the hole's true size.
- **Make a generator method lazy (a real iterator) while in there.** Rejected as out of scope and named: the eager
  buffer is what the record holds for a module generator and what `print(g())` has answered since **ADR 0081**;
  `next()`, a partially consumed buffer and a lazy `yield` are **L12.1**'s rows, and this commit deliberately leaves
  the two doors answering the same eager list.
- **Fill the buffer at the call site (caller-allocates), as `heapargs` does for a container argument.** Rejected: the
  body's appends happen across safe points, so the handle has to be rooted in the frame that owns them, and the module
  road's rooting is already the answer (**ADR 0181**). A caller-allocated buffer also cannot exist for a call the
  dispatch picks at run time.
- **Route a method generator through the closure/lifted-call machinery (`closure.go`).** Rejected: that path exists for
  a body the defunctionalisation pass moved, and it keys its own facts off the source name, so a generator method would
  have needed a third copy of the reading.

## Agentic interface

Two capability refusals are new and both are machine-readable, per **ADR 0346** / **L11.8**:
`lower.unsupported.split-generator-method-dispatch` (**Gap R.467**) and
`lower.unsupported.generator-method-parameter-walk` (**Gap R.475**). Each carries its stable code, the suggestion that
replaces it (call the generator on a receiver the compiler can see / walk a container the body can see or write the body
as a module `def`), and the roadmap rows that own the missing half, so a script branches on `capability.code` and never
on prose. Both ride the `--json` failure object, are declared in `--schema`'s enum, and are printed by
`gustyc --capabilities`; the inventory in `docs/operations.md` is generated from the registry by
`go run ./tools/captable`, and the registry's own sample programs are compiled by
`TestTheCapabilityRegistryCoversWhatItClaimsToKnow`, so a sentence edited without its marker is a failing test.
`docs/language.md`'s *Generators & lists* section now states the method form, the five doors that answer it, and the two
that refuse — which is the third place the same fact lives, and it is written from the tests that hold the bytes.

## Tests

`pkg/lang/generator_method_test.go` NEW — **26** answered programs on the compiled leg AND the record leg, with a
MISSING record failing the case (**ADR 0302**: an answer cannot be certified by the backend that earned it, nor deleted
by deleting a record); **10** reading rows asking `methodIsGenerator` DIRECTLY in both directions beside `containsYield`,
with their own controls — a yield only inside an `if`, a `while`, a `for`, a `match` arm or after an unconditional
`return`, a `yield from`, a plain body, a fall-through body, and the `async def` the checker owns — each also required to
agree with `containsYield` so the class door and the `def` door cannot answer one body twice; **4** IR rows over the
emitted method: the buffer allocated by `rt_alloc`, `rt_append` asked once per yield, the handle answered at BOTH exits
(exactly two `ret i32 %ghN`), and the whole shape asserted ABSENT from a method that does not yield; a never-worse ladder
that asks the same body at the method door and the module door and refuses a pair where the module arm answers and the
method arm does not; and **13** owed rows, each pinned with today's bytes AND the dossier that says whether it is
byte-identical on this row's parent. `integration/generator_method_test.go` NEW — **24** CLI programs whose expected
bytes are re-derived from a live `python3` inside the case, **1** record audit, **2** machine-readable JSON refusals
(code, phase, suggestion, rows), **3** exit-2 pins for the `llc` rejection **Gap R.474** owns, **8** owed rows with the
reference's bytes beside this backend's, and a corpus-program check. `integration/programs/generator_method.gy` NEW —
23 lines, `match` on all three legs of the conformance matrix, byte-identical to CPython. The two pins that stood in the
way (`pkg/lang/void_method_test.go`, `integration/void_method_test.go`) are DELETED rather than edited and reappear as
answered rows, and the gate was `make testparallel`.
