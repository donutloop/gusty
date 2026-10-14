# ADR 0340: the ordering builtins read `key=` and `reverse=` by name — a callable key is called by the ordinary call road

Status: accepted (2026-10-14)
Amends: [ADR 0191](0191-sorting-is-language-surface.md) — the row that made ordering surface and promised a future `key=` would decorate–sort–undecorate over the existing stable comparator
Extends: [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md) (one measured defect was `llc` rejecting `%_abs` at exit 2), [ADR 0233](0233-every-construct-is-implemented-refused-or-absent-never-answer-wrong.md) (the key is either called, raised for, or refused by name — never silently ignored), [ADR 0318](0318-a-fold-that-orders-containers-walks-their-elements.md) (ordering asks the elements; here it asks the key's answer for every element), [ADR 0339](0339-a-for-loop-asks-its-iterable-what-it-is-a-container-it-cannot-name-is-refused-never-read-as-a-count.md) (the loop asks its iterable before choosing a road, and now asks the ordering builtin's key before classifying that iterable), [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md) (every newly answered source is recorded from the reference and checked against the compiled leg)
Supersedes: nothing
Related: roadmap L11.7 (functions are values that compile; this row pays the ordering builtins' callable argument), Gap R.249 (closes), **Gap R.254 and Gap R.255 (newly measured here and filed here)**, Gap R.247 (the non-list argument the key cannot be carried into), Gap R.248 (the float/unsupported comparator this row leaves refused), Gap R.146 and L11.1 (the value word for run-time elements), Gap R.253 (the container pair this row deliberately does not widen)
Owner: `pkg/lang/call_key.go`, `pkg/lang/call_key_test.go`, `pkg/lang/codegen.go` (`case "sorted"`, `case "min", "max"`, `sortedRuntime`, `minMaxCallReturnsText`), `pkg/lang/for_iterable.go`, `cmd/gustyc/main.go`, `integration/call_key_test.go`, `integration/programs/key_argument.gy`

## Context

The ordering builtins had the right comparator and the wrong option parser. They read their second
argument **by position** and treated it as this language's documented reverse flag. The reference reads
options **by name**. Five programs measured before the change:

```python
print(sorted([3, 1, 2], key=abs))                 # CPython [1, 2, 3]      this backend: codegen: sorted(reverse=...) needs a constant flag
print(sorted([3, 1, -2], key=abs, reverse=True))  # CPython [3, -2, 1]     this backend: [-2, 1, 3] at exit 0
print(sorted([3, 1, 2], key=3))                   # CPython TypeError      this backend: [3, 2, 1] at exit 0
print(min(3, 1, -2, key=abs))                     # CPython 1              this backend: llc rejects %_abs, exit 2
print(min([3, 1, 2], key=abs))                    # CPython 1              this backend: "not a container among values"
```

The bad states were not just refusals. `key=3` was read as a truthy reverse flag and printed a descending
list at the exit code for success. `key=abs` in `min`'s argument list was read as a candidate and made the
emitted module refer to a non-existent `%_abs`, so the compiler, not the program, earned exit 2. The `for`
loop had a third face: `for v in sorted([3, 1, 2], key=abs)` refused even though the expression road and the
loop's materialisation road both already had stable ordering machinery.

The fix had one obvious trap. `key=` is not a second candidate, and it is not a reverse flag. It is a
callable. A language whose function values are not compiled could either keep refusing — leaving the most
common ordering options out of the language — or invent a second calling mechanism under the builtin. The
measured shape said to use the existing one: if the compiler can call `f(7)` in a program, it can call
`f(element)` from an ordering.

## Decision

The ordering builtins ask their options by name before any answer is emitted.

```go
pos, key, reverse, badKw := g.callOptions(c)
```

* `key=` and `reverse=` are options and never positional candidates.
* Any other keyword is the reference's runtime `TypeError`, emitted as an ordinary raise so
  `except TypeError:` catches it: `'cmp' is an invalid keyword argument for sort()` for `sorted`, and
  `'<name>' is an invalid keyword argument for min()` / `max()` for the fold.
* `key=None` is the reference's identity key: the option is removed and the ordinary unkeyed road answers.
  For the loop road this normalisation happens **before** the loop classifies its iterable, because the loop's
  fold asks about the builtin's argument list, not about its lowered answer.
* `min` and `max` do not take `reverse=`; the reference says so and so does this backend.
* A positional second argument to `sorted`, with no key present, remains the documented gusty repeat-flag
  deviation. Once `key=` is present, only the iterable is accepted positionally; the reference itself rejects
  a positional key and the refusal says so rather than pretending the flag still applies.

The key itself is asked one question: what is it?

* A declared `def` whose parameters can receive one argument is callable. The key call is synthesised as
  `Call{Fn: key, Args: [element]}` and lowered through `g.value`, the same road that answers `f(7)`, `abs(7)`,
  `len("abc")`, arity mistakes and raises inside the callee.
* A builtin whose one-argument call the backend already answers is callable too — `abs`, `len`, `ord`, `int`
  for numeric keys, `str` and `repr` for text keys.
* A non-callable literal, bound container, bool or `None` value is **not** a compile refusal. It is the
  reference's TypeError: `'int' object is not callable`, `'list' object is not callable`, and so on.
* A lambda, a call expression used as the key, a run-time-built element list, a float-returning key and a
  builtin with no usable one-argument answer remain refusals that name the missing half.

The ordering itself uses the comparator the unkeyed sorter already used (`@rt_elem_gt`). It does not call
`@rt_sort`; it builds a fresh heap list, appends every lowered element with its element tag, computes a stable
rank in emitted IR, then places the element with `@rt_put_elem`.

```text
rank_i = #{ j : key_j comes before key_i } + #{ j < i : key_j equals key_i }
```

Ascending counts keys smaller than the current key (`gt(key_i, key_j)`). Descending counts keys larger than
the current key (`gt(key_j, key_i)`). The tie half counts only earlier equal keys, which is what keeps equal
keys in the order the program wrote. `min` and `max` use the same comparator in an unrolled pick; ties keep
the first candidate, exactly as the reference does. An empty keyed `min`/`max` raises the reference's
`ValueError: min() iterable argument is empty` / `max() iterable argument is empty`.

The `for` road from ADR 0339 now asks the same option door before it refuses a keyed iterable. If the key is
callable, the loop walks the keyed answer; if the key is not callable, the loop's refusal and the builtin's
refusal name the same missing half.

## What this row does not open

The row pays the **key option**, not the whole of function values. Three measured refusals are left standing
and filed:

* `xs = [3, 1, 2]` / `print(sorted(xs, key=abs))` refuses. The elements exist only at run time; a keyed sort
  would have to carry an element to a call and bring back a key value. Without `key=`, the unkeyed runtime
  sort answers that same program (ADR 0339). Filed as **Gap R.254**.
* `lambda` keys, call-expression keys such as `key=f(1)`, and builtin keys whose one-argument call this backend
  cannot answer (`print`, `hash`, `round`, `float`, `sum`, and nested ordering builtins) refuse rather than
  evaluate the expression and pretend its answer is callable. A float-returning key is refused beside Gap R.248.
  Filed as **Gap R.255**.
* The ordinary call road's existing text-parameter limitation is not widened here. A user key that receives a
  text element can still meet that road's honest refusal; a builtin text key such as `len` answers literal text
  elements without inventing a new callback path.

The container pair (`isContainerExpr` / `staticContainerKind` / `containerHandleOf`) is not widened. ADR 0339's
measurement already showed that widening three roads without the operand road turned a working membership
answer into a refusal, and Gap R.253 owns that paired repair.

## IR and runtime shape

A keyed literal ordering emits, per element, one lowering of the element and one ordinary call to the key:

```llvm
%v0 = ... element 0 through g.value ...
%k0 = call i32 @f(i32 %v0)      ; or the builtin's existing call shape
```

It then builds one heap list:

```llvm
%h1 = call i32 @rt_alloc(i32 1)
call void @rt_append_tagged(i32 %h1, i32 %v0, i32 %tag0)
...
%cmp = call i32 @rt_elem_gt(i32 %ki, i32 %kj, i32 mode)
...
call void @rt_put_elem(i32 %h1, i32 %rank, i32 %v)
```

`@rt_put_elem` is used rather than `@rt_set_elem` because the latter bumps length: the first attempt printed
`[1, 2, 3, 0, 0, 0]` with six slots counted from six rank placements over three elements. The placement road
writes the slot; the append road owns the length.

A keyed `min`/`max` does not allocate a list. It lowers candidates and keys, then unrolls one `@rt_elem_gt`
plus two `select`s per candidate. It sets `heapUsed` because `@rt_elem_gt` lives in the heap runtime block.
While implementing that row the root-globals gate had the mirror-image asymmetry ADR 0173's rule warns about:
the body pulled the heap runtime text in while the data half was gated only on `heapUsed`, leaving `@gc.kinds`
undefined for keyed `min`. The gate now reads `needHeap` as well as `heapUsed` and `rooted`; no debt row is
left because the measured undefined-symbol program now answers.

## Agentic rationale

* `--lang` now has an `ordering:` line: the option names, the ordinary call road, and CPython's TypeError for
  a non-callable key. An agent does not have to read the compiler source to know which ordering options compile.
* A refusal is one sentence, at exit 1, naming the option and owner row: `roadmap L11.7` for the lambda/unsupported
  builtin/call-expression door, `roadmap L11.1, L11.7` for a run-time container's elements, and `roadmap L11.1,
  Gap R.248` for a key answer that has no comparator from one word.
* A raise is emitted at the program's point, not a compile-time refusal, so `--json` sees a normal exit-3
  exception and the program's own `except TypeError:` runs. The machine path for `sorted([3,1,2], key=3)` is
  the same path as for a bad argument to an ordinary call.
* The structural witness is not just stdout: keyed modules call `@rt_elem_gt`, `@rt_alloc`, `@rt_append_tagged`
  and `@rt_put_elem`, and **do not call** the unkeyed `@rt_sort`. If the keyed road starts delegating to the
  unkeyed sorter and inventing an answer, the shape test fails.

## Alternatives rejected

*Read the second argument as before unless it parses as an option.* Rejected — that is the measured bug. The
reference gives `key=` and `reverse=` meaning by name; a positional parser silently changed `key=3` from
TypeError into a descending list.

*Call the key through a new ordering helper or a function-pointer table.* Rejected — this backend has no
function values, and a second calling road would duplicate arity, default parameters, raises and returns while
disagreeing with the ordinary call road. `key=f` is exactly `f(element)`; the existing call lowering answers it.

*Refuse non-callable keys.* Rejected — CPython runs the ordering to the point of calling the key and stops with
`TypeError: '<type>' object is not callable`. A compile refusal there both loses a program and makes the
exception uncatchable.

*Evaluate call-expression keys and infer their result kind.* Rejected during measurement: `print(1)` must print
before TypeError, and a key such as `f(1)` has effects that cannot be skipped by a compile-time raise. Until the
value word can carry and call the answer, the honest answer is a refusal that names that missing half.

*Teach the container pair and let `sorted(..., key=...)` share it.* Rejected with ADR 0339's measurement. The
pair has more roads than `in`, `==` and `len`; widening it without `containerOperand` turns working answers
into refusals. Gap R.253 owns the pair.

*Let keyed `min` allocate and sort a list, then take the first.* Simpler, but wrong for the answer's kind and
effects: a selection from candidates should lower those candidates once and compare their keys, not build a
container the program never asked for. Rejected.

## Witnesses

`pkg/lang/call_key_test.go` runs 36 answered table rows on the compiled leg, the JIT and the record leg, 14
reference traps by class and message, 9 still-owed refusals with their sentences pinned, and 18 unchanged-road
rows (the unkeyed builtins, the documented positional repeat flag, shadowed user `sorted`, loops over a sorted
list variable). It also asks the option table directly and asserts the ordinary-call-road IR shape.

`integration/call_key_test.go` runs the same family through the shipped CLI against pinned CPython: 24 answered
programs, 13 traps at exit 3 with the reference's exact sentence, 4 catchable rows, 5 still-refused shapes at
exit 1, and 10 unkeyed negative-control rows, plus two rows for the gusty-only positional repeat flag.
`integration/programs/key_argument.gy` is the 203rd conformance program: 98 source lines, 43 reference-printed
lines, `match` in `integration/conformance-matrix.json`.

The record leg grew from 6591 to 6665 sources for this family (ADR 0302: each newly answered source is recorded
from the reference only after the compiled leg agrees; refusals are not recordable and remain in the test
tables).
