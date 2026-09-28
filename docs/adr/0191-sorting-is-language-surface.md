# Sorting is language surface: one comparator, tags move with payloads

## Context

`xs.sort()`, `xs.reverse()` and `sorted(xs)` on a variable existed in neither backend. The
interpreter raised `no such list method sort`; the compiled path answered **`string method sort on
non-constant string`** — the call had fallen through to string-method dispatch, so the compiler told
the user their list was a string. `sorted([3,1,2])` on a literal was the only spelling that worked,
because a constant folder sorted an array of `IntLit`s; `sorted(["b","a"])` refused with "list
elements must be integer literals", and `sorted(xs)` on a variable with "codegen folds only an
inline list literal". The sweep in ADR 0190 found all of this in a ten-minute pass over twelve
tutorial programs.

Sorting looks like the least interesting feature in the world, which is why it had been skipped:
there is a `sorted` builtin, it is obviously implementable, and no design question seems to be at
issue. Four real decisions fell out of it anyway, and this ADR records them because each one is a
rule that will be re-litigated by the next container method (`min`, `max`, `index`, `count`, and
the `key=` that needs L11.7's functions).

## Decision

**1. One comparator, two orders, in both backends.** `rt_elem_gt(a, b, mode)` answers the ordering
question for a container slot: `mode 0` compares payloads as signed numbers; `mode 1` treats the
payload as an index into `@str_tab`, loads the text, and `strcmp`s it. The second mode is the whole
feature: **a stored string is an interned index, and the index records the order strings first
appeared in the program.** Sorting strings by payload sorts them by arrival — which agrees with
alphabetical order often enough that a spot check passes and a program breaks. The interpreter has
the same shape in `Evaluator.compareElems`, including the float branch, because the two backends
must not have two ideas about what "less than" means for a value.

**2. Sorting is insertion sort, and it is stable on purpose.** Quadratic, in a representation whose
element array is 256 deep — the bound comes from the runtime's own shape, not from hope. Stability
is the reason: `sorted(key=)` and `list.sort(key=)` are specified in Python as
decorate–sort–undecorate over a *stable* sort. An unstable sort today would make tomorrow's `key=`
subtly wrong, and that class of bug is invisible in tests that sort distinct numbers. `sortElems`
(interpreter) and `rt_sort` (compiled) are the same algorithm for the same reason.

**3. An exchange moves the whole slot — payload *and* tag.** ADR 0187's pairing rule says the
operation that writes a slot's payload writes its tag; sorting is a series of writes to two slots at
once, so a swap that moves payloads and leaves tags behind mislabels the list it just sorted. The
first draft of `rt_sort` did exactly that, and the reason it is not a latent bug is that a test
asserts the `@heap_tags` stores are inside the swap block. `rt_reverse` swaps tags from the start;
`rt_list_copy` copies them.

**4. A method mutates and returns None; the builtin copies.** `xs.sort()` / `xs.reverse()` lower to
a call on the variable's own handle and yield the None value; `sorted(xs)` lowers to
`rt_list_copy` + `rt_sort`, so `xs` keeps its order. That distinction is observable (the corpus
program prints both), and it is where the GC rules of ADR 0181 bite: the copy is a new root, so it
is allocated through the tracked path (`%h<N>` off `heapSeq`) and stored into a variable registered
as a container. A handle stored in a plain int variable is the ADR 0188 bug one layer down —
`ys = sorted(xs); print(ys)` printed the slot number, and it took the assignment-path fix (track
`sorted`/`reversed`/`list` results as list variables, inheriting element-kind from the source) to
make the program mean what it says.

**5. A mixed-kind list is refused, and the refusal says what Python does.** Python raises
`TypeError: '<' not supported between instances of 'str' and 'int'`; it does not invent an order.
The interpreter raises the same; the compiler refuses with a message naming both the rule and
Python's answer. Ordering a str/int mix by payload would be *another* arrival-order answer. Same
for `sort(key=...)`: it needs first-class functions (L11.7's other half), and the refusal owns that
reason instead of pretending the argument was a count.

## Codegen/IR implications

- New runtime helpers: `rt_elem_gt`, `rt_sort`, `rt_reverse`, `rt_list_copy` — all `define
  internal`, emitted with the prelude. Their existence in the module is now a property of the
  runtime, not of the program, which broke two tests that counted call sites module-wide; those
  counts are now scoped to user code (`countOutsideRuntimePrelude`), because a module-wide count
  changes whenever a helper is added and proves nothing about the program.
- `print(sorted(xs))` needed an explicit branch: the lowering hands back a heap handle, and the
  print path otherwise `printf`s it — the invalid-IR shape ADR 0188 removed for literals.
  `reversed("abc")` still takes the string path, so the guard is "the argument is a container".
- One register-naming rule is load-bearing: handle registers are `%h<N>` from `heapSeq`. A handle
  named `%t<N>` reads as an ordinary temp to the print and call lowerings, which then render it as
  a value. That is a naming convention carrying semantic weight, and it is written down here
  because it is the kind of thing that only makes sense after it has bitten you.

## A tooling note, because it cost the most time

`GUSTY_KEEP_LLVM=1` now keeps the JIT scratch directory and prints its path. Every one of the bugs
above was an *llc rejected this module* failure, and the failing text was deleted with the temp dir.
A compiler whose IR failures cannot be inspected is a compiler you cannot debug, and for an agent
driving this toolchain it is the difference between a structured diagnosis and a guessed one.
(`--emit-llvm <file>` still diverges from the JIT path — it refuses shapes the JIT compiles, e.g.
`print(sorted([10, 2, 33]))` with `unsupported attr expression`. Recorded as a machine-path gap
rather than fixed here, since it is a second codegen entry point, not a second behaviour.)

## Alternatives rejected

- **Constant-fold everything, keep no runtime sort.** What the code did before, and it can only
  order values it can see at compile time — so `sorted(xs)` on a variable has no answer, and a
  tutorial program stops compiling.
- **Sort by payload for strings and note the caveat.** Rejected for the reason above: arrival order
  and alphabetical order coincide often enough to survive review, and the corpus row would have
  pinned the wrong answer as correct.
- **A general qsort with a callback into gusty-level comparison.** The honest long-term design once
  values carry tags (L11.2) and functions are values (L11.7) — it is what `key=` will need. Building
  it now means indirect calls through the very machinery that does not exist yet; insertion sort is
  the version that is correct today and whose *interface* (comparator on a slot pair, mode) survives
  that upgrade.
- **Let `xs.sort()` return the list to enable chaining.** Python returns `None`; making it return the
  list would be a language divergence invented to save a line, and it would make
  `print(xs.sort())` print a list where Python prints `None`.
- **Silently sort mixed lists by tag then payload.** Python raises. Both backends now refuse.

## Consequences

- `sorting.gy` and `sorting_literals.gy` are parity rows in the matrix (they were the pinned probes
  `probe_sort_methods` and `probe_sorted`, which the ledger no longer needs — deleting the pin is
  how a paid debt is recorded). Corpus 63 rows / 48 parity, oracle 32 `match` / 21 `debt` /
  10 `not_applicable`, 0 drift.
- `sorted(key=)`, `min/max(key=)` and the `functools`-style callables stay in L11.7, and now have a
  stable, comparator-shaped base to sit on. `xs.insert`, `xs.index`, `xs.remove`, `xs.extend`,
  `xs.clear` are the same dispatch table, and each will need the same pairing rule.
- The dict-key/set-dedup payload-only comparison remains as recorded in ADR 0189's consequences:
  safe while heterogeneous keys are refused, and it must take the tag when they are not.
