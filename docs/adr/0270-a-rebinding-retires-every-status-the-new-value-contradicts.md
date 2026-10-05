# 0270. A rebinding retires every status the new value contradicts

## Status

Accepted. Ships with one door — `retireVarStatuses(name, rhs)` in `pkg/lang/heapargs.go`, called from the
assignment path in `pkg/lang/codegen.go` before the binding records its own kinds — retiring the
interned-text pair (`strVals`, `internedVars`) and the class an instance came from (`varClasses`) whenever
the value being stored contradicts them.

Closes roadmap **Gap R.145** ("An interned-text binding survives a container rebinding": `n = "text"` then
`n = [1, 2]` prints `text` compiled at exit 0, where CPython and the interpreter print `[1, 2]`).

Found again, and this time fixed, while landing the signless doors of ADR 0271: `abs` asks the same
records the print dispatch asks, so with the stale record in place it *raised*
`bad operand type for abs(): 'str'` on `x = "text"` / `x = 5` / `print(abs(x))` — a program CPython answers
with `5`. A door that reads an untruthful notebook is worse than the wrong answer it replaces, so the
notebook got fixed first.

## Context

ADR 0172 states the rule, and it has been in the compiler since long before this cycle:

> the variable's **latest** assignment decides how print, truthiness and equality lower, and any other
> assignment clears the status.

What the rule never got was an implementation with a single owner. Each status ended up with its own
forget-helper and its own call sites:

| Status | Helper | Cleared at |
|---|---|---|
| the None singleton | — (`delete(g.noneVars, …)` inline) | the scalar binding |
| the verdict (`boolVars`) | `forgetVarBool` | the scalar and unpacking bindings |
| the tagged pair (`taggedVars`) | `forgetTaggedBinding` (ADR 0267) | ten binding paths |
| the container kinds | inline `= false` at the free | the scalar binding that frees the heap slot |
| **the interned text** (`strVals`, `internedVars`) | **none** | **nothing** |
| **the class of an instance** (`varClasses`) | **none** | **nothing** |

So the last two rows were the measured defect:

```gy
x = "text"
x = [1, 2]
print(x)        # CPython [1, 2] · interpreter [1, 2] · compiled text   (exit 0)
```

and the same shape with a number, a dict, a set, `None`, and the same shape read back through `str(x)` and
through the numeric road (`x * 2` refused at exit 1 because the arithmetic still saw a text). The class
half is quieter — `x = C()` then `x = 5` printed `5`, because print's instance arm is asked only after the
other arms decline — but a method call on that name would have dispatched to `C`'s method.

The reason this is worth a door rather than another `delete` at one more call site is the one ADR 0267
learned when it wrote `forgetTaggedBinding`: once a *new* consumer reads the record, an omission that was
latent becomes a wrong answer. ADR 0267's consumer was the print dispatch on a pair-bound name; this
cycle's is the signless door, which asks `printsAsInternedStr`/`exprIsString` about a name and would raise
CPython's `abs` sentence on a program whose answer is `5`.

## Decision

**One door, called at the binding, that clears exactly what the new value contradicts.**

1. `retireVarStatuses(name, rhs)` is called from the assignment path the moment the target is a `*Name` —
   before the union record, the escape-analysis shortcut, the container registration and the string fold
   below it. Every one of those paths *sets* what the new binding is; the door's job is only to take away
   what it is not. Ordering matters: clearing after the fold would delete the record the fold had just
   written.
2. It asks the **print dispatch's own predicate**, not a fresh one: `bindingIsText` is `stringVal` ∨
   `exprIsString` ∨ `printsAsInternedStr` ∨ a call the module knows answers text (ADR 0229's rule that the
   print path and the operation path ask the same question, applied one step earlier, at the binding). Two
   answers to "is this a text?" is how `print(x)` and `-x` have disagreed before.
3. A text binding therefore keeps its record, which is what `len(x)`, `x.upper()` and the folded
   concatenation read; a container/number/`None`/class binding takes it away. The class record is retired
   by anything that is not a `*Call` — only a construction can bind an instance.
4. **The container kinds are left to the existing door.** Their clearing is entangled with the GC: the
   scalar path frees the old heap slot (`emitFreeOld`, `gcClearRoot`) *by reading* `listVars`/
   `runtimeDicts`/`runtimeSets`. A helper that cleared those first would skip the free and turn a wrong
   answer into a leak, so `bindTaggedVar` and the scalar free keep that job (Gap R.142's fix, ADR 0181's
   rooting rule). Recorded here so the next reader does not "tidy" it into the same helper.
5. **The tag keeps its own helper** (`forgetTaggedBinding`, ADR 0267) for the same reason — it is called
   from paths that are not assignments at all (loop bindings, comprehension targets).

## Measurement

Reference leg CPython 3.12.3; compiled leg Ubuntu LLVM 20.1.2 (`llc-20`, `opt-20`). The *before* column was
re-measured on a binary built from the pre-cycle commit in a `git worktree`, not remembered.

| Program (both bindings at module top level) | Before — compiled | Now — both engines | CPython |
|---|---|---|---|
| `x = "text"` / `x = [1, 2]` / `print(x)` | `text` | `[1, 2]` | `[1, 2]` |
| `x = "text"` / `x = {"a": 1}` / `print(x)` | `text` | `{'a': 1}` | `{'a': 1}` |
| `x = "text"` / `x = 5` / `print(x)` | `text` | `5` | `5` |
| `x = "text"` / `x = 5` / `print(str(x))` | `text` | `5` | `5` |
| `x = "text"` / `x = 5` / `print(x * 2)` | exit 1 refusal | `10` | `10` |
| `x = "text"` / `x = 5` / `print(-x)` | **exit 3 `bad operand type for unary -: 'str'`** | `-5` | `-5` |
| `x = "text"` / `x = None` / `print(x)` | `text` | `None` | `None` |
| `x = None` / `x = "text"` / `print(x)` | `text` | `text` | `text` |
| `x = "a"` / `x = "bb"` / `print(len(x))` | `2` (unchanged) | `2` | `2` |
| `x = 5` / `x = "abc"` / `print(x.upper())` | `ABC` (unchanged) | `ABC` | `ABC` |
| `class C` / `x = C()` / `x = 5` / `print(x)` | `5` | `5` | `5` |

Note the two rows in that table that were *traps* before: the negation of a name whose stale record says
text raised CPython's own sentence at exit 3 — a raise the reference never performs. That is the shape of
the risk this commit removes, and it is why the door is placed at the binding rather than at the reader.

Counts, from one green run of `go test -tags=llvm20 ./...`: `pkg/lang/rebind_status_test.go` 5 tests — 19
both-engine parity rows, the gate asked directly (six bindings × the two records, built on the
`&irGen{}`-style the kind gates in `mixed_list_test.go` use), the class-record row, four "the new binding
keeps what it needs" rows and the `bindingIsText` table. `integration/pair_binding_test.go`:
`TestAContainerRebindingRetiresTheTextBindingToo` was a filed-not-fixed table pinning the compiled leg's
`text`; it is a three-engine parity table now, eight rows, and the harness failed the build until it moved
— which is the ratchet working (a paid debt must break something).

## Agentic rationale

* **A name's statuses are a machine-readable fact, so they have to be a truthful one.** `--emit-llvm`, the
  print dispatch, the numeric door and the oracle all read the same records. An agent that asks `--json`
  why `print(x)` says `text` after a rebinding gets an answer derived from a notebook the compiler knew
  was stale.
* **The gate is askable.** `retireVarStatuses` and `bindingIsText` are tested as functions, not only
  through output, in the style ADR 0265/0267 established: a loosening fails a row instead of producing a
  plausible print.
* **No machine surface moved.** No flag, schema, exit class or message changed. Two shapes moved from exit
  1/3 to exit 0 because they were never supposed to fail; the refusal that `x * 2` used to raise is gone
  rather than reworded, and `docs/operations.md` needed no new row.

## Alternatives rejected

* **Clear the record at each reader** (`print`, the numeric road, the signless door each asking "is this
  record older than the last binding?"). Three copies of a rule that exists to be asked once, and the
  readers cannot see the binding: only the emitter can. This is ADR 0267's lesson about a rule asked twice.
* **Fix it in `scanStringBindings`,** the whole-program pre-pass that first marks `internedVars`. The scan
  sees every binding and cannot tell which one is last at a given use; making it conservative (unmark any
  name bound to more than one kind) turns this wrong answer into a *decline* for every multi-bound name,
  which costs the text methods and `len` to programs that answer today. Measured on the branch:
  `x = "text"` / `x = 5` / `print(x)` regressed to a refusal, and `x.upper()` went with it.
* **Clear the container kinds in the same door.** Tempting symmetry, and it breaks the GC: the scalar
  binding frees the name's old heap slot by reading `listVars`/`runtimeDicts`/`runtimeSets`, and
  `gcClearRoot` marks the root dead by the same record. Clearing first leaks the object. Left to the door
  that owns the free.
* **Close Gap Q.1 here instead.** Q.1 is the *general* "a variable rebound to another kind keeps the old
  kind" row, including the statuses this commit leaves to their own helpers; R.145 is the named, measured
  pair. Q.1 stays open with its own scope.
* **Leave the door out and make `abs` decline a name whose records disagree.** That was the first draft of
  this cycle: a whole-program "kinds ever bound to this name" scan that made the door decline. It passes
  every abs test and leaves `print(x)` answering `text` — the wrong answer the row was filed for, kept so
  that a different door can be conservative. Deleted, and its measurement is the second paragraph of
  Context.

## Consequences

* Gap R.145 closes. Gap Q.1 — the general status table — stays open, and this commit narrows it to the
  statuses that still clear themselves in five different places (`boolVars`, `noneVars`, the container
  kinds, the tagged pair).
* The signless doors (`-x`, `abs(x)`, ADR 0271) may read the records as truth. That is the dependency the
  next ADR cites, and the reason the two commits are ordered this way.
* `retireVarStatuses` is the place a future status goes when it is added: register it in the binding, and
  retire it here. A status that cannot be retired here is a status whose binding must also be moved into
  one door — which is what Gap Q.1 is for.
* The corpus gained no program: the shapes are covered in the two new test tables, and the probe that
  pinned the compiled leg's `text` moved out of the filed-not-fixed tables rather than being deleted.
