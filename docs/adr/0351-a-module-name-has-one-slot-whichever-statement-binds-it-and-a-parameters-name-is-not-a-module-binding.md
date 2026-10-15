# ADR 0351 — A module name has one slot, whichever statement binds it; a parameter's name is not a module binding

Status: accepted
Date: 2026-10-15
Amends: [ADR 0227](0227-a-module-name-a-body-reads-is-state-the-call-runs-against-so-the-name-lives-in-a-global-and-not-a-frame.md), [ADR 0163](0163-a-function-body-owns-its-slots-per-call-so-a-name-some-other-scope-used-is-not-a-reusable-allocation.md)
Depends on: [ADR 0220](0220-a-variables-value-at-a-call-is-what-the-call-runs-against-not-what-the-parser-saw.md), [ADR 0181](0181-the-collector-is-precise-about-roots-because-a-register-that-can-hold-a-handle-is-not-a-root.md), [ADR 0166](0166-exit-2-is-a-compiler-bug-and-not-an-answer-so-a-program-the-reference-runs-may-not-leave-the-toolchain-with-it.md)
Roadmap row: Gap R.49

## The gap, as measured

The row was filed as an exit-2 bug and it had quietly become a wrong answer. Both faces came from the
same two sentences of `pkg/lang/codegen.go`, and both were measured against `python3` on `9b60ed4`
before anything below was written.

Face one — a value that no statement produced, at the code that means the program ran:

```gusty
def g(y):
    return y

y = 2
for y in [10, 20]:
    print(y)
print(y)
```

CPython prints `10`, `20`, `20`. The compiled backend printed `2`, `2`, `2` and exited 0. The emitted
module explains it in five lines:

```llvm
  %_y = alloca i32                       ; the loop's variable
  store i32 2, i32* @gy_mod_y            ; y = 2
  store i32 10, i32* %_y                 ; the loop binds … the alloca
  %l2 = load i32, i32* @gy_mod_y         ; … and every read asks the global
```

Face two — an ordinary program blamed on the compiler:

```gusty
def g():
    return y

y = 1
y += 3
print(y)                                 # CPython: 4
```

```
llc-20: error: …:3984:23: error: use of undefined value '%_y'
  store i32 %t4, i32* %_y
```

Exit **2** — ADR 0166's code for a compiler bug, produced by three statements of ordinary arithmetic.
Renaming either binding made both faces disappear, which is the signature of a name-keyed fault rather
than a shape-keyed one.

## Why the two faces are one defect

A module-level name that some function body reads is given real storage — `@gy_mod_y` — because a
main-frame alloca is gone by the time the body runs (ADR 0220, implemented compiled-side by ADR 0227).
Two questions decide that storage, and each had been answered in exactly one of the two places that had
to agree:

1. **Which names need a global?** `moduleSlotNames` walked every body and marked every name a body
   mentioned — *without asking whether that body binds the name*. A `def`'s parameter is a binding of
   the body, and a body's own assignment shadows the module for the whole body exactly as it does in the
   reference. So `def g(y): return y` was counted as a module read of `y`, and `y` — a name the module
   had bound to a literal and then rebound through a loop, which is what disqualifies a literal from
   being a folded constant — was handed a global it never needed.
2. **Which roads bind a name that has one?** Only the plain assignment asked. `y += 3` and both `for`
   roads (the unrolled-literal one and the container-walk one) wrote `%_y` instead, because they carried
   their own alloca-based idea of a variable. One road wrote a slot no reader consults; another wrote a
   slot nothing had ever allocated.

A name therefore had, in the worst case, two slots and three writers. That is the whole row.

## The decision

**One name, one slot, one decision about it.**

1. **The storage question is asked per scope.** `moduleSlotNames`'s walk now carries a shadow set: a
   body's parameters, its own assignments, its loop variables, its `with … as` name and its match
   captures are all bindings *of that body*, and a name they hold is not a module read. The set nests, so
   a `def` inside a `def` shadows what encloses it as well as what it binds — the scoping the reference
   applies. A name still earns its global when **any** body reads it without binding it, so narrowing the
   gate past parameters into genuine module reads breaks a row (`TestAParameterNameIsNotAModuleBinding`
   asserts both directions on the AST itself, in the ADR 0273 style: the decision, not just its answers).
2. **Every road that binds a module name asks the same helper.** `storeModuleScalar` is now the one
   place that decides where a module-level scalar binding goes; the plain assignment, the augmented
   assignment and both `for` roads call it. It returns whether it emitted anything, so a road keeps its
   own alloca for the ordinary case — a name no body reads still has no global and no second store.
3. **A loop's variable keeps its rooted slot *and* writes the global.** The alloca is not redundant: an
   element can be a heap handle, and the collector follows root entries, not globals (ADR 0181). What was
   missing was the second store, not the first.
4. **A name nothing's body reads still carries nothing.** The gate got narrower, not wider:
   `TestAModuleNameCarriesNoGlobalItsBodyNeverReads` fails if a program whose only body mention of `y` is a
   parameter emits `@gy_mod_y` at all, and `TestEveryBindingOfAModuleNameWritesItsSlot` pins the stores a
   name that *does* have one must actually get.

## What was rejected

- **Allocating `%_y` in the augmented-assignment road (making the alloca exist).** It removes the `llc`
  failure and leaves the wrong answer: the module would then have `%_y` *and* `@gy_mod_y`, written one
  apiece by neighbouring statements. The exit-2 symptom was the loud half of a split, and curing the noise
  is not fixing the split.
- **Making reads consult the alloca when the alloca exists.** That is order-dependent — a name read by a
  body needs the global no matter what the main frame happened to allocate first — and it reintroduces
  exactly the per-body state leak ADR 0163 was written to close.
- **Refusing the shape instead (a module name that is also a parameter).** Rejected outright: the
  reference runs every one of these programs, and ADR 0166's rule is that a refusal is the wrong answer
  to a program the reference runs — a front-end refusal is a diagnostic an agent branches on, not a way to
  make a hard case invisible.
- **Letting the loop's read fall back to whatever the module last stored (a copy at loop entry).** Rejected
  because it would silently differ from Python on the case that matters: a body called from inside the loop
  must see the loop's current binding, which is a store, not a copy.
- **Renaming the emitter's symbol for parameters so a parameter can never collide with a module name.**
  Attractive and cheaper, and rejected: the collision is not what made the store wrong (the store was wrong
  for the *module's* name), and mangling names to dodge a bookkeeping bug leaves the bug in place for the
  next road that writes the slot directly.

## Codegen / IR implications

- `@gy_mod_<name>` becomes the single answer to "where does this module name live", so an IR-shape claim
  can finally be made about it. `TestTheModuleAllocasAreHoistedAndPerScope` walks every emitted definition
  and fails on any `store <ty> …, %_x` with no earlier `%_x = alloca` in the same function — the exact
  `llc` rejection this row was filed under, asserted structurally rather than by example.
- `TestEveryBindingOfAModuleNameWritesItsSlot` pins the *number* of global stores a program must produce
  (three for a three-element loop, two for a container walk, one for `+=`), so a road that quietly drops
  back to alloca-only fails on a count rather than on a printed value.
- Nothing in this row touches the tagged value word, the arena, or the collector's marking: the shadow
  analysis is a read-only pass over the AST, and `storeModuleScalar` emits one store.

## Verification

- Unit (`pkg/lang/module_slot_scope_test.go`): 20 crossings run on the compiled leg and the JIT with
  CPython's answer beside each, and the same 20 asserted to be **on the record** (recorded from the
  reference by `tools/recmerge`, not self-certified — this row newly answers them); the gate asked
  directly as a function over the AST in both directions; the alloca/store invariant over every emitted
  definition; the global-store counts; the negative control that a program whose body never reads a name
  carries no global; and exit 2 forbidden on every row.
- Integration (`integration/module_slot_scope_test.go`): the same sources through the CLI against
  `python3` on `--eval` and `--file` (the JIT, where `llc` runs in-process), through **`--build`** — the
  link-and-run leg, which is the one that really shells out to `llc` and `cc` and therefore the only place
  the exit-2 half can be seen from outside — and through the machine path's `--json` payload, which must
  carry the answer at exit 0 and not an `llc` rejection at exit 2. The corpus program is compared line for
  line against `testdata/module_slot_scope.expected`, which `python3` wrote.
- Corpus: `integration/programs/module_slot_scope.gy` joins the conformance matrix as a `match` row, with
  the names reused across module, parameter and loop target on purpose.

## What this row still does not own

Measured while writing these tables, and filed rather than fixed because they are a different road's
business: **a module name rebound over a literal TEXT keeps printing the literal** — `m = "n"` /
`m = f()` prints `n`, and `label = "n"` / `for label in ["p", "q"]: print(label)` prints `n` three times
while the reference prints `p`, `q`, `q` (roadmap Gap R.273; the root is the module's constant and
interned-string records not being retracted by those roads, not the slot). And a body reading a module
name whose bindings are not scalars still refuses with Gap R.35's sentence, which is honest but is the
refusal this row's own corpus program had to be written around.

Two more were measured while testing the fix and are **filed, not fixed**, because they are a different
road's business and each is a separate defect. Both leave an ordinary program at **exit 2** — `llc`
rejecting a module this toolchain emitted — which ADR 0166 calls a compiler bug:

- **Two nested `def`s that share a name collide on one arena slot** (roadmap **Gap R.274**). `def a(): x = 1 /
  def h(): return x / return h()` beside `def b(): x = 2 / def h(): return x / return h()` is the
  reference's `1 2`; here `llc` answers `redefinition of global '@h_slot'`. Give the two inner functions
  different names and the identical program compiles and prints `1 2` — the control that says the fault is
  keyed on the NAME, and the reason the fix is per-definition identity rather than a rename.
- **A module name defined twice collides in the function table** (roadmap **Gap R.275**). `def h(): return 1`
  above `def h(): return 2` is the reference's `2` — in Python the later binding wins and the first body is
  dead — and here `llc` answers `invalid redefinition of function 'gy_h'`. This is the same "one name, one
  slot" instinct this row applied to variables, applied where it does not belong: a name may be *rebound*,
  and the second definition is a different function with the same spelling.

Both are pinned with today's answer in `TestWhatTheModuleScopeCycleFoundNextDoorIsFiledNotFixed` (unit) and
`TestWhatTheModuleScopeCycleFiledNextDoorThroughTheCLI` (CLI), and neither can be recorded: `tools/recmerge`
refuses to write a record for a source whose compiled leg prints other bytes than the reference's, which is
why these two live in tests and not in the record.
