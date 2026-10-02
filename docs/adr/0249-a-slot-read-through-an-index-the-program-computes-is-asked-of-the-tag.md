# ADR 0249 — a slot read through an index the program computes is asked of the tag

Date: 2026-10-02
Status: Accepted
Roadmap: L11.1 (the tagged value word, numeric family), Gap R.88 (closed here),
Gap R.82 (the ordering half, still open), Gap R.89 / Gap R.90 (measured by the same sweep, left open),
ADR 0243 (a literal-indexed slot is that number), ADR 0233 (a float slot holds a box handle, not a number),
ADR 0166 (the exit-code contract: a module `llc` rejects is the compiler's bug)

## Context

ADR 0243 taught the compiled backend that an element the literal still describes **is** that number for
arithmetic: `xs = [1, "a"]; print(xs[0] + 1)` answers `2`. The promise is a compiler-time one — the
literal is in front of it — and where the promise runs out, the answer ran out too. The next thing a
program does is compute the index:

    xs = [10, 4]
    i = 0
    print(xs[i] / 4)      # CPython 2.5 · --interp 2.5 · --aot exit 2

Not a refusal — an **exit 2**. The float arm had already decided the expression was floating, then asked
for the operand, got nothing, and emitted

    %t2 = fdiv double , %t1

which `llc-20` rejects. ADR 0166 counts exit 2 on a program CPython answers as the compiler's own bug,
so an ordinary three-line program was failing as a compiler defect. The same family, measured on three
engines before anything was written:

| program | CPython | `--interp` | `--aot` |
|---|---|---|---|
| `xs=[10,4]; i=0; print(xs[i]/4)` | `2.5` | `2.5` | **exit 2** (`fdiv double , %t1`) |
| `xs=[1.5,"a"]; i=0; print(xs[i]+1)` | `2.5` | `2.5` | refused: *needs a single static kind* |
| `xs=[1.5,"a"]; i=0; print(-xs[i])` | `-1.5` | `-1.5` | refused |
| `xs=[1.5,"a"]; i=1; print(-xs[i])` | TypeError `bad operand type for unary -: 'str'` | TypeError | refused |
| `xs=[1,"a"]; k=1; print(1 > xs[k])` | TypeError `'>' … 'int' and 'str'` | TypeError | TypeError naming **'str' and 'int'** (operands the wrong way round) |
| `xs=[1,"a"]; i=0; print(-xs[i])` | `-1` | `-1` | refused |

Two of those are wrong answers rather than refusals, which is the worse class: the reversed comparison
message and — found while wiring the negation — the interpreter's own unary minus.

## Decision

**A numeric use of a slot whose kind the object carries is lowered to the slot's (payload, tag) pair and
dispatched on the tag — but only where the compiler can settle the *result's* kind.**

1. **One read, one shape.** The read is `runtimeSlotPair`: the payload and the tag the writer left beside
   it, the same pair ADR 0241 already uses to print a slot and ADR 0247 to compare one. Nothing new is
   stored; this is the third use of the same two words.

2. **The tag decides the arithmetic.** A float slot unboxes through `rt_float_of` (never: read the box
   handle as a number, which is the miscompile ADR 0233 exists to keep out). An int or bool slot converts
   with `sitofp`. A slot holding text, `None` or a container **raises**: one branch per kind the container
   can actually report, the last one as the unconditional `else`, so every path out of the read either
   produces a `double` or raises, and the merge `phi` never has a predecessor that stores nothing.

3. **The raise is CPython's, per kind and per operator.** `unsupportedNumberOp` spells the sentences —
   and they are not one sentence: `"a" + 1` is *can only concatenate str (not "int") to str*, `"a" - 1` is
   *unsupported operand type(s) for -: 'str' and 'int'*, an ordering is *'&gt;' not supported between
   instances of 'str' and 'int'*, and `-xs[i]` is *bad operand type for unary -: 'str'*. Two consequences
   fell out of writing them down:
   - **the two type names come in source order**, so the door has to know which side the read was on —
     `1 > xs[i]` names `'int' and 'str'`, and a message assembled from (slot, other) rather than
     (left, right) has it backwards;
   - **negation is its own operator**, so it is its own door. Lowering `-xs[i]` as `0 - xs[i]` would have
     raised the binary sentence for a program that never wrote a binary minus.

4. **The gate is the result's kind, not the operand's.** A float-only container is admitted for
   `+ - * / // % **`; true division is admitted whatever the operands because it always answers a float;
   an ordering is admitted whatever it lifts because it answers `0`/`1`. What is declined:
   - a container whose slots are **ints on one side and floats on the other** (`xs = [1, 2.5]`), where
     `xs[i] + 1` is `2` or `2.0` depending on the index — answering it from the float arms prints `2.0`
     for CPython's `2`, which is a wrong answer dressed as a near miss. That question is the tagged value
     word Gap R.82 still owes;
   - a container **no literal describes** (`xs.append(1.5)`, or a container handed to a function): the
     compiler would be guessing, and a guess is an answer that depends on the program's data;
   - **`*` and `%` over a container that can hold text**, because there CPython does not raise at all —
     it repeats (`"ab" * 2`) and formats (`"a" % 2`), which this backend does not have (Gap R.33, R.31);
   - **two slot reads of a container that can hold text**, where `xs[i] + xs[j]` is concatenation for one
     pair of indices and a TypeError for another, and a door that only emits raises would answer the first
     wrongly.

5. **An operand that cannot be lifted is never emitted.** `floatValue` returning nothing used to be
   ignored by the print and format sites, which is how `fsub double 0.0, ` and `rt_fmt_double(double )`
   reached `llc` — a second exit-2 hole of the same shape, in the print path rather than the arithmetic
   one. Now the lift records what it could not lift and the caller returns a front-end refusal that names
   the operand and the debt. Exit 1, `codegen:` prefix, no assembler complaint.

6. **The bounds check is untouched.** The read still goes through `rt_get_elem`/`rt_tag_of`, so an
   out-of-range slot is still an `IndexError` rather than a `phi` reading a word out of the object —
   pinned on both engines (`TestTaggedNumericUseKeepsTheBoundsCheck`,
   `TestTaggedNumericKeepsTheBoundsCheck`).

## Consequences

- The float path is now the only path that can ask a slot's tag what it means, so it is also the path
  arithmetic and negation arrive on. `isFloatExprName` says so for the operators the door answers, which
  is what lets `print` choose the float formatter for `xs[i] + 1` and print `2.5` rather than `2`.
- A refusal pin is allowed to break when the work lands, and one did: `mixed_list_test.go` claimed
  `print(1 if xs[1] > 2 else 0)` over `[1, "a"]` must be refused at compile time. CPython raises for that
  program and both backends now raise it too, so the row moved to the trap table rather than being kept
  as a flattering test.
- `mixedReadErr`'s sentence was updated to say what the tag now carries — printing, binding, equality,
  membership, length **and a numeric use whose result kind the compiler can settle** — because a refusal
  that claims less than the truth is its own defect (the Gap R.38 rule).

## Measured, not fixed here

Two defects came out of the same sweep and are recorded rather than silently repaired, each with its own
roadmap row:

- **Gap R.89 — unary minus never consults a tag.** `print(-"a")` prints `-281474976710658` in the
  interpreter (the interned index, negated) and `0` in the compiled backend; `print(-[1])` prints the same
  garbage and, for a list *literal*, emits `%t1 = sub i32 0, @.lst1` — `llc` rejects it, so **exit 2**.
  CPython raises `TypeError: bad operand type for unary -: 'str'` / `'list'`. The door added here fixes
  this for the one shape it owns (a float-family container read through an index); everything else about
  unary minus is still the interpreter's and the untagged path's.
- **Gap R.90 — an out-of-range subscript names the wrong container.** Both backends say
  `IndexError: index out of range`; CPython says `IndexError: list index out of range`. A one-word
  divergence, but the sentence is part of the language's contract and a program that matches on it will
  not match.

## Alternatives rejected

- **Read the payload and let the arithmetic decide.** That is the miscompile ADR 0233 refuses: the payload
  of a float slot is a `@float_box` handle, so `xs[i] + 1` would add a heap address to 1 and print a
  plausible-looking number. It is also exactly what the interpreter's unary minus still does.
- **Answer the "mixed" family by lifting everything to float.** `xs = [1, 2.5]; print(xs[i] + 1)` would
  print `2.0` where CPython prints `2`. Printing a float where an int was asked is not a near miss, it is
  a different value, and the next program divides it.
- **Guess the family from the first element, or from what the container "looks like".** An answer that
  depends on the program's data is not a compile-time answer; it is a runtime answer with the runtime
  removed.
- **Emit the empty operand and let `llc` complain.** Exit 2 on a program CPython answers is the compiler's
  bug by ADR 0166, and the diagnostic an assembler gives is not one a programmer can act on.
- **Fold negation into the binary minus path.** Cheaper by twenty lines, and it would have shipped a
  TypeError sentence for an operator the source never contained.
