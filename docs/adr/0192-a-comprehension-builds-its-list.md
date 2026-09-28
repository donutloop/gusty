# A comprehension builds its list; the fold is an optimisation, not the meaning

## Context

`[f(x) for x in range(5)]` worked in the interpreter and refused in the compiled backend with
`comprehension element must be constant`. That message is the signature of an implementation
order turned inside out: the AOT comprehension lowering *was* a compile-time folder — it bound the
loop variable in a constant map, folded the element expression to an `int64` per item, and emitted a
global `@.lstN`. Every comprehension whose element was not an integer constant had no meaning at
all, including the most ordinary list-building idiom in the language. `docs/language.md` described
comprehensions without ever saying they were constant-only, so the gap was invisible from both ends.

This was the last finding from the boring-program sweep (ADR 0190) that the ledger had pinned and
nobody had owned. Closing it required the thing the folder was avoiding: a comprehension that runs.

## Decision

**1. The meaning of a comprehension is a loop that appends; the fold is an optimisation on top.**
Three lowerings now coexist, tried in this order:

- **Fold** — iterable is a literal/`range` with constant bounds *and* the element and filter fold.
  Unchanged, and it must keep priority: `sum`/`min`/`max`/`len` over a comprehension read the
  compile-time element set, so sending a foldable comprehension to the runtime path makes those
  builtins refuse (a regression I introduced and reverted inside this cycle).
- **Unrolled** — iterable enumerable at compile time, element not foldable: one straight-line
  block per item, storing the item into the loop variable's slot, evaluating the element,
  appending. This is the same strategy `for` over a literal already uses, so the backend gains
  nothing new to verify and the shape matches its neighbours.
- **Runtime loop** — iterable is a container with a runtime length: a real loop (named preheader,
  condition, body, done blocks; `rt_list_len` + `rt_get_elem` per step).

The probe that decides "is the fold possible" runs the *same* fold the constant path will run, so
the two cannot disagree about what is foldable. A probe that lies would build a list at compile
time and a list at runtime and call whichever failed an error.

**2. Binding the loop variable is the feature.** Every one of these lowerings writes the item into
the loop variable's *slot* (`%_x`) rather than substituting a constant, which is what lets the
element contain `sq(x)` — a call that reads the variable — at all. That is also why the loop
variable needs the same kind-tracking the `for` statement records (`internedVars` for a container
of strings): a filter reading `n == "a"` has to know `n` is an index into `@str_tab`.

**3. A handle must be rooted where it is born.** `ys = [f(x) for x in ...]` stores a heap handle,
and the assignment that stores it is where the GC root has to be registered. Routing it through the
ordinary scalar store compiled, ran, and **segfaulted** under the link-and-run tests — the collector
did not see the live handle and recycled the list out from under the program. The binding now takes
the slot + `gcReg` + `gcStoreHandle` path (ADR 0181's rule, applied to a construct that had never
produced a heap object before). Had I tested only through the JIT I would have seen this later: the
segfault showed up in the `--build` parity tests first, which is an argument for the two-path test
rule rather than against it.

**4. Refuse where the fold would have lied.** Three shapes refuse, each with a message that names
the reason and the roadmap item:

- `sum`/`min`/`max` over a comprehension with runtime elements: there is no compile-time element
  set to fold, and folding the empty one **answers 0 for a list that has elements in it**. I built
  that bug, saw the 0 in a test table, and turned it into a refusal; a runtime reduction is the
  open item (`probe_comp_runtime_reduce`).
- A filter comparing elements with a string literal (`[n for n in names if n == "a"]`). This is
  not a new limitation: it is ADR 0192's most important discovery. `for n in names: if n == "a":`
  emits `icmp eq i32 %_n, @.str3` — an index into `@str_tab` compared against the *address* of a
  string global — and **llc rejects the module**. That is exit 2 through `--build` semantics: a
  compiler bug, not a refusal, and it predates this change. The comprehension refuses (exit 1)
  instead of inheriting the rejection, and both shapes are now pinned:
  `probe_str_loop_eq` (the `for` case, whose pin asserts the llc error text) and
  `probe_comp_str_filter` (the comprehension case).
- An iterable the escape analysis kept as a compile-time constant (`xs = [1, 2, 3]` never
  materialised): there is no slot to load, and emitting the load is another llc rejection. The
  message says how to materialise it (`append`, or iterate with `for`) and points at L11.2, whose
  tagged value word makes every container a runtime object and removes the whole category
  (`probe_comp_folded_iter`).

**5. A heap-building construct requests the heap runtime.** The prelude is emitted on demand, and a
comprehension can be the only allocation in a program; without the flag the module called an
undefined `@rt_alloc`. Smaller than it sounds, invisible until a program with no other containers
was compiled.

## Codegen/IR implications

- New block-label family `comp.pre/cond/body/done/item/skip`, all from the shared counter, so two
  comprehensions in one function cannot collide.
- The induction phi forward-references the body's next-register (the runtime helpers do the same
  and llc accepts it), and that register is named off the temp counter — **not** `%s<N>`, because a
  name starting with `%s` reads as a string value to the print and call lowerings. This backend has
  already been bitten by that naming rule twice, and a register-naming convention carrying semantic
  weight belongs in an ADR.
- `print([...])` goes through `rt_print_list_mixed` via `containerOperand`, which now accepts a
  `*Comp`: the constant path returns a folded global whose layout is a length plus an array, so it
  is materialised into the heap first. ADR 0188's "a container in a value position is a rendering
  question" rule, one construct later.
- Assigning a comprehension marks the variable as a container (so `len`, indexing, `for`, truthiness
  and `print` all see a list) and inherits the element kind for string elements.

## Alternatives rejected

- **Keep folding and refuse everything else.** The status quo, and it made `docs/language.md` a
  lie: the language has comprehensions, and the compiled backend had a subset whose boundary was
  "contains only integer arithmetic".
- **Lower every comprehension to the runtime loop.** Simplest to reason about, and wrong for this
  backend today: it would take `sum([x for x in range(5)])` from a constant to a loop, breaking the
  fold consumers that read `compEls`. The three-way ordering costs a comment and keeps both.
- **Synthesise a closure call per element.** The honest design once functions are values (L11.7's
  other half) and comprehension filters can call user functions uniformly through the fnptr path;
  until then, "evaluate the element expression with the variable bound" is the same semantics
  without machinery that does not exist.
- **Let the 0 stand for `sum` of a runtime comprehension.** Rejected in the moment it was
  discovered: a refusal costs one program; a wrong answer costs the corpus's trust, and this row
  would have entered the matrix as a passing case.

## Consequences

- `comprehension_calls.gy` is a parity row (promoted from `probe_comprehension_call`, whose pins
  said the AOT leg must fail — deleting the pin is how a paid debt is recorded). Corpus: 66 rows,
  48 parity, 18 probes; oracle 32 `match` / 24 `debt` / 10 `not_applicable`, 0 drift.
- Three new debts, each owned: `L11.8` for the interned string comparison (with the llc-rejection
  contract violation it exposes), `L11.7` for the runtime reduction, `L11.2` for the folded-away
  iterable. The `for`-loop string-comparison bug is the one to fix first: it is a crash, it is
  older than this cycle, and the comprehension refusal exists only to keep it from spreading.
- Set and dict comprehensions still take the fold path only; the runtime lowerings are list-kind.
  That is recorded in `docs/language.md` rather than discovered by a user.
