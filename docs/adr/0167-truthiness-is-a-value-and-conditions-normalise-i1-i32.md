# 0167. Truthiness is defined by value, and conditions normalise i1/i32

Status: Accepted
Date: 2026-09-27
Audience: compiler maintainers, agents consuming the CLI

## Context

Writing a both-backend corpus for the L9.6 parity work surfaced something basic: programs
that Python accepts and that the *interpreter* mostly ran were not compilable at all in
the AOT backend, and the cases that did compile disagreed with the interpreter.

```py
if count:                 # LLVM: '%_count' defined with type 'i32' but expected 'i1'
while total:              # same
if a and b:               # LLVM: '%t1' defined with type 'i1' but expected 'i32'
if a < b or b > 9:        # same
print(not x)              # same
print(1 if flag else 2)   # same
if 0.0:                   # interpreter: true (wrong), AOT: false
if xs:                    # xs = [1, 2] — interpreter: true, AOT: false
```

Two distinct causes:

- **Representation confusion.** A value in this backend is either an `i32` (integers,
  booleans stored as 0/1) or an `i1` (an `icmp`/`fcmp` result). The condition paths assumed
  `i32` (`icmp ne i32 %cmp, 0`, `br i1 %count`), so *any* condition whose operand happened to
  be a comparison result — which is what `and`/`or` receive — produced IR LLVM rejects.
  Conversely, values that should be `i32` (the result of `not`, `in`, a comparison used as a
  value) leaked a bare `i1` into `store i32`, `call i32 @print`, `ret i32`.
- **Testing the representation instead of the value.** The interpreter tested `cond != 0`
  everywhere, and floats/strings/containers live on the heap, so a handle made `0.0`, `""`
  and `[]` truthy. The AOT compiled `if xs:` to a handle null-test, which is never false for
  a live container — and for a compile-time list global the tested word was not a length
  either, so a non-empty list came out false.

Also found while testing this: rebinding a container variable emitted an unconditional
`rt_free(old)`, and `0` is both "not a handle" (int/bool/None, freshly declared container)
and a valid heap slot index — so `ys = []` freed the object owned by an unrelated `xs`.

## Decision

**1. Truthiness is a property of the value, defined once, in both backends.**
Falsy: `0`, `0.0`, `-0.0`, `False`, `None`, `""`, and containers with no elements. Everything
else — including any other number, string, container or object — is truthy. `if`, `elif`,
`while`, the ternary, `and`/`or`/`not`, comprehension `if` clauses and `match` guards all use
this one rule; documented in `docs/language.md` § Truthiness, exercised by
`integration/truthiness_test.go` (38 cases, each asserted against *Python's* answer, not
against the other backend) and `programs/truthiness.gy`.

**2. Condition operands are normalised, never assumed.** All condition paths go through
`(*irGen).truthOperand`/`truthyValue`, which accept either representation:

- an `i1` produced by a comparison passes straight through (tracked in `irGen.i1Vals`, keyed
  by SSA register, so `if a < b and b < 9:` does not pay a `zext` + re-test per comparison);
- anything else becomes `icmp ne i32 v, 0`;
- a float is `fcmp one double v, 0.0` so `0.0`/`-0.0` are falsy;
- containers and strings are tested by **length**: `rt_list_len`/`rt_dict_len`/`rt_set_len`
  for heap handles, the compile-time length for literals and literal-producing calls.

**3. Values are `i32`, predicates are `i1`, and the boundary is explicit.** Comparisons,
`and`/`or`, `not` and membership now return the interpreter's `i32` 0/1 (`asBoolI32`), so
they are printable, storable and re-testable; only branches/selects/logic instructions see an
`i1` (`asI1`).

**4. The interpreter asks the value behind a handle.** `Evaluator.truthy` consults the heap
object (float value, string contents, container length) instead of testing the handle for
nonzero, and is used at every condition site.

**5. A handle is only released if it is one.** `emitFreeOld`/`emitFree` wrap every
`rt_free` in `if (h != 0)`, and a slot created by the statement being lowering is skipped
outright (`freshSlots`), so declaring a container can never recycle somebody else's slot 0.

## Rationale (agentic)

`if count:` is the most ordinary construct in the language; an agent that cannot compile it
has no way to distinguish "unsupported" from "I wrote something wrong", and a parity harness
that compares only the two backends cannot see a bug both share (the interpreter's
handle-truthiness) or a program the corpus never wrote. Hence the test asserts against
Python's answer, and the conformance corpus now carries a truthiness program so drift here
becomes a red test rather than a mystery.

## Codegen / IR implications

```llvm
; if count:                       (was: br i1 %_count  — invalid)
  %_count.ld = load i32, i32* %_count
  %t1 = icmp ne i32 %_count.ld, 0
  br i1 %t1, label %if.then, label %if.else

; if a < b and b < 9:             (i1 operands used directly, no zext/re-test)
  %t2 = icmp slt i32 %_a.ld, %_b.ld
  %t3 = icmp slt i32 %_b.ld2, 9
  %t4 = and i1 %t2, %t3
  %t5 = zext i1 %t4 to i32        ; `and` yields a value

; while xs:                       (xs a runtime list: length, not handle)
  %_xs.ld = load i32, i32* %_xs
  %t6 = call i32 @rt_list_len(i32 %_xs.ld)
  %t7 = icmp ne i32 %t6, 0
  br i1 %t7, label %while.body, label %while.end

; xs = [] after xs held a handle  (guarded release)
  %f1 = load i32, i32* %_xs
  %t8 = icmp ne i32 %f1, 0
  br i1 %t8, label %free.do, label %free.skip
free.do:
  call void @rt_free(i32 %f1)
  br label %free.skip
free.skip:
```

## Alternatives rejected

- **Coerce at each call site as bugs are reported** — this is what had happened; the same
  class reappeared in five places (`if`, `elif`, `while`, `and`/`or`, `not`, membership).
- **Always return `i1` from comparisons and coerce when storing** — moves the confusion
  instead of removing it: every consumer of a value would need to know the producer.
- **Let LLVM fold the redundant `zext`/`icmp` pairs and skip the tracking map** — at `-O0`
  (the default for fast feedback) nothing folds, and the emitted IR is what users read with
  `--emit-llvm`.
- **Define truthiness of a container as "the handle is non-null"** — it is what the backend
  did, and it is wrong for `[]` and for `[1, 2]` simultaneously.
- **Assert Python's behaviour in the test of only one backend** — parity is the contract; the
  battery runs on both and also verifies every emitted module.

## Consequences

- `i1Vals`/`asI1`/`asBoolI32`/`truthOperand` are the only sanctioned ways to cross the
  predicate/value boundary; new condition-like syntax must route through them.
- Container truthiness now costs a `rt_*_len` call in AOT — correct first, and the
  optimiser pipeline handles hot loops.
- `programs/truthiness.gy` joins the conformance corpus (35/35 cases pass parity).
- Related gaps recorded in `roadmap.md`: Gap K.2 (dict iteration in AOT), Gap K.3
  (`list.pop`, `set()`).
