# A slice of a container answers the list — the handle is not the list, and a copied payload copies its tag

## Decision

A container slice (`[1, 2, 3][1:]`, `["a", "b"][::2]`) is answered on **both** engines by the same runtime
helper (`rt_slice`), and three separate defects on that one road are closed together (ADR 0187's pairing
rule, ADR 0234's literal question, ADR 0166's exit codes; roadmap `Gap R.179`, owner L11.1):

1. **A container literal's global address is not a heap handle.** `[1, 2, 3][1:]` lowered to
   `call i32 @rt_slice(i32 @.lst1, …)`. llc rejected it — *global variable reference must have pointer
   type* — which ADR 0166 counts as **exit 2**, the compiler's own bug. The fix is the comprehension road's
   existing answer, not a new one: ask the literal records, and when the operand is a literal, build a real
   heap object first (`heapListFrom`) and slice *that*. Both spellings are claimed — the literal node the
   source wrote, and a name the records hold a literal for.
2. **A heap handle printed through printf's `%d` is the bug ADR 0188 already removed for literals.** The
   print road tests per-node shapes (`*Name` + `listVars`, `*Comp`, `sorted(...)`) and had no slice arm, so
   it took the numeric road: `print([1, 2, 3][1:])` said `1`. A slice of a container is a container, asked
   from the shared `isContainerExpr` the equality and membership roads already consult — the arm went in the
   predicate, not copied into each caller.
3. **`rt_slice` copied payloads and never copied tags** — ADR 0187's pairing rule ("the operation that
   writes a slot's payload writes its tag") violated by a runtime builder that had simply never followed it.
   `print(["a", "b"][1:])` rendered the interned text's INDEX: `[1]` where the reference prints `['b']`.

## The agentic rationale

Every shape was a number at exit 0 that CPython does not produce, or exit 2 on a program the reference runs,
and **the interpreter answered all of them correctly** — so the two-backend parity matrix is structurally
blind to the whole row and only the oracle leg can see it. That is the argument for the oracle leg, restated
in a new domain: an agent scripting this compiler would have received `1` from `--aot` with a clean exit code
and no signal at all.

The refusal-vs-bug line is unchanged: `print(len([1, 2, 3][1:]))` still refuses (`len` wants an inline
literal). That is a *different* road and a different row — closing the slice did not quietly turn a refusal
into a guess, and did not turn the working `len` cases into failures.

## Codegen / IR implications

- **A container literal is a compile-time global, and any runtime helper that walks `@heap` needs a real
  object.** Cycle 19 found this for the arithmetic roads; the slice road was the same bug with a different
  helper. The general rule now recorded in `listLiteralOf`: expressions that lower to `@.lstN` must be
  materialised before any `rt_*` call takes them.
- **Print dispatch is per-node-shape, which means every new node needs an arm.** `*Slice` was invisible to
  it. The arm sits *before* the numeric road, like the `sorted`/`Comp` arms beside it, and asks the shared
  `isContainerExpr` rather than a private copy of the question.
- **The tag copy is two `getelementptr`s into `@heap_tags` inside a raw module string** — where llc, not the
  LLVM verifier inside Go, is the first thing to complain. An IR comment containing a backtick or a `;`
  mishap broke the Go raw string and produced a *Go* syntax error before any IR was emitted; IR comments go
  through the same review as IR instructions.
- **`isContainerExpr` gained `case *Slice`, which also feeds equality and membership.** That is the point of
  putting it there, but it is also why the kept-answer rows exist: `xs[0] == [1, 2]`, `2 in xs[0]` and
  `for row in xs[0]` read the same predicate.

## Alternatives rejected

- **Fold a container slice at compile time** (the compiler can read `[1,2,3][1:]`). Rejected: it duplicates
  ADR 0234's literal problem instead of answering it — the result would be another global address, and any
  later `rt_*` consumer re-triggers the same exit 2. One representation, one road.
- **Print slices through the container printer only when the base is a name.** Rejected: `[1,2,3][1:]` — the
  exact shape that crashed — has no name.
- **Leave `rt_slice`'s tag omission to the future tagged value word (L11.1).** Rejected: it prints a wrong
  number at exit 0 *today*, and the pairing rule already exists — this builder had just never been asked to
  follow it. Deferring a wrong number because a redesign will eventually make it impossible is how wrong
  numbers survive.
- **Make `print` ask `g.value` and inspect the resulting IR text.** Rejected: the question is about the
  program, not the string we rendered; the shared predicate already answers it and can answer it again for
  the equality road.

## Related

`Gap R.179` (this row), ADR 0187 (payload–tag pairing), ADR 0188 (a handle is not a value), ADR 0234 (a
folded literal stays a literal), ADR 0166 (exit codes), ADR 0192 (L11.1's tagged value word, which owns the
rest of this family), `Gap R.175` (the same global-address-where-a-handle-belongs class, arithmetic roads).
