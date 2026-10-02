# ADR 0248 — an ordering of two texts asks `strcmp`; equality keeps the index

Date: 2026-10-02
Status: Accepted
Roadmap: L11.1 (the tagged value word's comparison family), Gap R.84 (closed here),
Gap R.85/R.86/R.87 (measured by the same sweep, left open), ADR 0173 (the intern table),
ADR 0224/ADR 0229 (the print/operation paths ask one predicate), ADR 0166 (the exit-code contract)

## Context

A text value in the compiled backend is an index into `@str_tab`. Interning is content-addressed, so
two equal texts are the same index and `x == "hi"` is an `icmp` — that is ADR 0173's purchase, and
nothing here changes it. An **ordering** is a different question wearing the same word: an index
records the order the program mentioned each spelling, not the order of the characters.

    print(1 if "b" > "a" else 0)   # CPython 1 · --interp 1 · --aot 0

`"b"` is the first text that program mentions, so it interned to 0 and `"a"` to 1, and the comparison
of two texts asked which text the program wrote first. Every `<`, `<=`, `>`, `>=` between texts had
that answer. Two variables, `later = a > b`, a loop over `["pear", "apple"]`, a function parameter —
all arrival order.

The embarrassment is that the right answer was already in the module. `rt_sort` takes a mode argument
and, for mode 1, compares two elements with `strcmp` rather than by payload, with a comment saying
that sorting strings by payload "would sort them by arrival and look almost right until it did not".
The sorter knew. The comparison operators were never given the same question, so a program could sort
a list of texts correctly and then compare two of its elements wrongly.

The shape of the defect is worth recording precisely, because it hid in three places at once:

  - `value`'s comparison arm emitted `icmp sgt i32 %t1, %t2` on the two indices (wrong answer);
  - the condition arm reuses `cmpI1Op` to keep an `i1` for a branch, and had the same compare (wrong
    answer, in `if a > "a":`);
  - the string-operand guard, which *refused* the same question in a binding —
    `later = a > b` died with `operator ">" on a string is not supported in the AOT backend; ... a
    compiled string is an interned table index, so arithmetic and ordering on it have no meaning`.

So one comparison was a refusal in a `let` and a wrong answer in a `print`. The message was honest
about the representation and wrong about the consequence: ordering an interned index does have
meaning, once something reads the bytes behind it.

## Decision

**One helper answers an ordering between two texts, and it reads the text.**

  - `@rt_str_order(i32 %a, i32 %b)` is added to the heap runtime block beside `rt_elem_gt`: both
    operands are indices into `@str_tab`, both are loaded to their `i8*`, `strcmp` answers, and the
    sign of the result is returned as -1, 0, 1.
  - `textOrderOperands` is the door's question, asked with the predicates the print and operation
    paths already agree on (ADR 0224, ADR 0229): `stringVal`, `exprIsString`, `printsAsInternedStr` —
    an operand that *renders* as text is an operand that *orders* as text, or the two doors disagree
    and one of them is quietly wrong.
  - `emitTextOrderOperands` emits the call and the predicate against 0, returning both the i32 0/1 a
    value position needs and the `i1` a condition can branch on. It takes the two registers the
    operands already live in rather than lowering them itself, because an operand can carry a print or
    a call and must not be lowered twice (ADR 0189's rule for a statically-decided comparison).
  - All three sites consult it: the value arm (before the `icmp` family is emitted), the condition
    arm (before `cmpI1Op`), and the string-operand guard (before it refuses, so an ordering is answered
    and the guard is left to what it is actually for — arithmetic on a text).
  - **Equality does not move.** Two interned texts are equal exactly when their indices are, and
    routing `==` through `strcmp` would pay a call to learn what the representation guarantees. The
    asymmetry is pinned by `TestTextEqualityIsStillAnIndexComparison`, so a later cycle cannot tidy it
    away in the name of symmetry.

## Consequences

  - An ordering of two texts is answered by both backends, in both positions, and the answer no longer
    depends on the order the source lines happen to mention the spellings. `pkg/lang/text_order_test.go`
    (16 rows) and `integration/text_order_test.go` (20 programs, checked against `python3`) pin it;
    the rows are written so that arrival order and text order disagree, because a table written with
    `"a"` first would have passed the old code.
  - `rt_str_order` sits beside `rt_elem_gt`'s mode-1 arm rather than replacing it. They are one
    question with two callers; a later cycle that gives `rt_sort` the sign helper will collapse them,
    and the comment on each names the other.
  - An ordering of a text against a number, or against `None`, is still answered rather than raised —
    which the measurement found and **Gap R.85** now records, because the ordering door's work made the
    wrongness visible. That is Gap R.37's class (a compile-time decision with no way to become a
    language event), not a new one.
  - Two more measured defects came out of the same sweep and are recorded without being fixed:
    **Gap R.86** (a container orders against a container by its heap handle, so `xs < ys` depends on
    allocation order) and **Gap R.87** (two *list literals* compared for order emit
    `icmp slt i32 @.lst1, @.lst2`, `llc` rejects the module, and the CLI exits 2 — a compiler bug under
    ADR 0166, filed apart from R.86 because it is loud where R.86 is quiet).
  - `pkg/lang/slot_ordering_test.go` is added as the ordering counterpart of `slot_equality_test.go`:
    the rows the compiler can answer (text-only containers, dict entries, ints and floats, both
    positions) as parity rows, and the rows it cannot (a container that mixes kinds, ordered) pinned as
    refusals with the interpreter's answer recorded beside them — Gap R.82's remaining half.

## Alternatives rejected

  - **Refuse an ordering of two texts, as the guard already did for a bound one.** Honest, and it is
    what the two failing paths did *differently*, which is the whole complaint: the program asked one
    question and got a refusal in a `let` and a verdict in a `print`. The answer costs one call into a
    helper the module already had the parts for.
  - **Order by index and fix the intern table to sort on insertion.** Interning is content-addressed
    and insertion-ordered for reasons ADR 0173 chose deliberately; an ordering that reads the table's
    order would still be wrong for any two texts whose arrival order differs from their text order,
    which is the normal case, and it would tie the answer to the source text.
  - **Route everything — equality included — through `rt_str_order` for symmetry.** It pays a call for
    a fact the representation guarantees, and it makes ADR 0173's central claim (index equality *is*
    content equality) untestable. The pin that keeps them apart is cheaper than the symmetry.
  - **Compare the payloads with `icmp` when both operands are interned text and one index is a
    constant known at compile time.** Then the answer depends on what the optimiser can see, which is
    how "almost right" got this far.
  - **Fold literal-vs-literal orderings at compile time and leave the rest refused.** Half the rows
    would be right for the wrong reason, and the fold has to agree with the runtime helper on
    byte-for-byte ordering anyway — at which point the helper is the feature.

## Machine path

  - `--emit-llvm` / `--emit-ast --json` name the helper: `call i32 @rt_str_order(i32 %t1, i32 %t2)` is
    the value position, and the predicate against 0 follows it. An agent reading the module sees that
    text ordering is content-based without running the program, and `TestTextOrderingAsksStrcmp`
    asserts the call sits in every emitted module that orders text — in the value position, in a
    condition, and across slot reads — with no compile-time global in an operand.
  - Refusals for orderings this door will not answer keep the `codegen:` prefix and exit class 1;
    Gap R.85's cases are measured, not yet pinned, and the roadmap rows carry the programs verbatim.
