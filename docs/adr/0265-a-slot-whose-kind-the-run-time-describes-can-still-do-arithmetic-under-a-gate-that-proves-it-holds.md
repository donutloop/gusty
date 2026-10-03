# 0265. A slot whose kind the run time describes can still do arithmetic — under a gate that proves it holds numbers

## Status

Accepted. Ships with the compiled pair door (`@rt_num_arith`, `@rt_lift_num`, `@rt_kind_name`,
`@rt_num_bad` in `pkg/lang/codegen.go`), the operand door and gate in `pkg/lang/heapargs.go`, and the
program-wide kind-tree pass in `pkg/lang/numeric_slot_door.go`. Closes the numeric clause of roadmap
L11.1's last open gap; leaves the membership and loop clauses where they were (Gaps R.95, R.83).

## Context

Roadmap L11.1's row says the tagged value word is the one component that is still a gap, and lists the
uses that had to be paid for one at a time: the read (ADR 0241), the ordering of a slot whose kind the
object carries (ADR 0252), the print of it, the equality of it (ADR 0233's `rt_slot_eq`). The shape that
remained refused is the one where the *answer's* kind is the problem:

```
xs = []
xs.append([7, 8])
print(xs[0][0] + 1)      # CPython 8 · --interp 8 · --aot refused, exit 1
```

`xs` is mutated, so the static shape that recovered a slot type — "this name is still the literal it was
bound to" (`containerLits`, ADR 0241) — has no promise left to keep, and the read walks back to a name
whose literal no longer describes the slots. The refusal was honest (it named the missing half and exited
1, ADR 0166), but it was the last thing standing between this language and the most ordinary program in
the book: a list of lists, and a number taken out of one.

The reason it was never paid for earlier is that arithmetic is the one use whose *result type* has to be
known where the module is written. `xs[0][0] + 1` answers an `int`; make the slot hold `7.5` and the same
source answers a `float`. Static SSA types every instruction, so a single `add` cannot serve both, and an
`fadd` that prints `8` where CPython prints `8` and `15.0` where CPython prints `15.0` is not one
instruction — it is a decision about a value that does not exist until the program runs.

## Decision

**Carry the pair, do the arithmetic in the target, and let the answer bring its own kind — but open the
door only for a program that can be shown to keep numbers in the slots the read reaches.**

1. **The pair travels.** A slot read yields `(payload, tag)`; `payload` is the object's payload word and
   `tag` is the byte the writers left in `@heap_tags` (ADR 0233's value model). Nothing new is invented:
   `taggedContainerRead`, `runtimeSlotPair`, `mixedElemPair` and `mixedDictPair` already produced exactly
   this pair for the print dispatch, and the arithmetic door calls those rather than growing a second
   read.

2. **The operation happens in the runtime.** `@rt_num_arith(op, ap, at, bp, bt, outp, outt, outmsg)`
   takes both pairs, answers with a pair, and returns a status: `0` wrote an answer, `1` is a TypeError,
   `2` is the int-word OverflowError. Inside, `@rt_lift_num` turns either half of the pair into a
   `double` — unboxing a float handle, widening an int, widening a bool, which is a number to Python
   (ADR 0229) — and the operation is `fadd`/`fsub`/`fmul` in the one word that holds both families exactly
   for the magnitudes at stake (ADR 0253's lift, generalised from `/` to the operators whose answer
   changes kind). The caller branches on the status and stores the tag it must print with, so
   `print(xs[0][0] + 1)` prints `8` and `print(xs[0][0] * 2)` prints `15.0` from the same emitted shape.

3. **The raise stays with the caller.** The helper fills a static buffer and returns; the emitted code
   stores `@exn_flag`/`@exn_code`/`@exn_msg` and branches to the open handler or the function's raise
   exit. That is what makes `except TypeError:` and `except OverflowError:` reach it — a runtime helper
   that raised itself would be invisible to the program, which is ADR 0228's rule for built-in traps and
   is kept here rather than worked around. The two sentences are CPython's own:
   `unsupported operand type(s) for +: 'int' and 'str'`, `bad operand type for unary -: 'str'`, both with
   the kind name the *object's* tag supplies.

4. **The gate is the decision, and it is a proof, not a hunch.** `+` and `*` are answered by CPython
   with things that are not numbers whenever a text or a container turns up — `"a" + "b"`, `"a" * 3`,
   `[1] + [2]`, `[1] * 2` — and this backend can build none of those from a slot (Gap R.82 owns text and
   container arithmetic). So a door that opened for a slot that might hold text would *raise where the
   reference returns a value*: a wrong program, and the kind of wrong that no verifier catches.
   `computeNumericSlotChains` therefore walks the program's own store-to-slot sites — `append`, `add`,
   `extend`, `insert`, `update`, `d[k] = v`, every element of every container literal — and demands a
   literal, all-numeric kind tree along the chain the read walks. A container fed by a call, a name, an
   input, a comprehension, or by text is not proven, keeps the refusal it always had, and the row says so.
   The pass is coarse on purpose — one text stored in any container closes `+` and `*` for the whole
   program — because coarse here means refusing more often, and the asymmetry (ADR 0166) has always been
   that a wrongly-refused program is the cheaper error.

5. **`-` and the unary `-` are not gated.** They answer a number or raise, for every kind, in both
   directions — `1 - "a"` and `-"a"` are TypeErrors in the reference, and there is no sentence of CPython's
   in which a subtraction or a negation returns a text. So they take the door whatever the container holds,
   and the runtime's raise is reachable through them today.

6. **The int arm is guarded before the `fptosi`.** Both tags int/bool and a double answer outside
   `[-2147483648, 2147483647]` is a catchable `OverflowError` naming roadmap L12.12, not a truncation.
   This is ADR 0264's lesson (Gap R.133) applied the same day the new door was written: out of the
   compiled `int` word, `fptosi` is poison, and a program that overflows must stop with a sentence rather
   than print a silent negative. The interpreted leg keeps its `int64` and answers CPython's number; the
   split is pinned per leg, not averaged.

7. **The block is emitted only when it is used.** `arithUsed` and a scan of the finished body decide
   whether the module carries the door, and it rides with the heap block because `@rt_lift_num` asks that
   block for `rt_float_of` and `rt_float_new` — a referenced internal function that was never emitted is
   the module `llc` rejects (ADR 0173, ADR 0192).

## The bug this cycle found in itself

The first version formatted both raise sentences with one `snprintf` call and one argument list, and the
negation printed `TypeError: bad operand type for unary -: '-'` — the operator symbol where the operand's
kind belongs, because the binary format's first `%s` is the symbol and the two formats were being fed the
same three varargs. CPython prints `unary -: 'str'`. The block now has two calls under a branch, and
`TestNumericSlotArithRaisesThroughTheEmittedDoor` plus the CLI table hold the sentence. It is recorded
here because the interesting part is not the typo: the module verified, `llc` accepted it, and the number
printed fine. Only reading the reference's own words back caught it.

## Measurement

Both engines against CPython, same source, `--interp` and `-aot` forced explicitly
(`--file <path> --interp`, `--file <path> -aot`; a bare `--file` is the interpreter's default and `-aot`
after the path is parsed as the flag's value — the mistake that made an earlier cycle report a divergence
that was only the leg not being forced).

| program | CPython | `--interp` | `--aot` (new) | `--aot` (before) |
| --- | --- | --- | --- | --- |
| `xs.append([7,8]); print(xs[0][0]+1)` | `8` | `8` | `8` | refusal, exit 1 |
| `xs.append([7.5,8]); print(xs[0][0]*2)` | `15.0` | `15.0` | `15.0` | refusal, exit 1 |
| `xs.append([7,8]); print(-xs[0][0])` | `-7` | `-7` | `-7` | refusal, exit 1 |
| `xs.append([7,8]); print(xs[0][0]+xs[0][1])` | `15` | `15` | `15` | refusal, exit 1 |
| `d["k"]=40; print(d["k"]+2)` | `42` | `42` | `42` | refusal, exit 1 |
| `xs.append("hi"); print(-xs[0])` | TypeError `unary -: 'str'` | `-281474976710659` | TypeError `unary -: 'str'` | TypeError `unary -: '-'` |
| `xs.append("hi"); print(xs[0]+1)` | TypeError `can only concatenate str…` | same | refusal, exit 1 (gate shut, Gap R.82) | refusal, exit 1 |
| `xs.append([7,8]); print(xs[0][0]*1000000000)` | `7000000000` | `7000000000` | OverflowError, exit 3, catchable | refusal, exit 1 |
| `xs.append([7,8]); print(xs[0][0] / 2)` | `3.5` | `3.5` | `3.5` (ADR 0253's own door) | `3.5` |

`programs/numeric_slot_arith.gy` prints `8 5 -7 15 15.0 8.5 42` identically on all three engines and is
registered `oracle: match`. The two probes are `oracle: not_applicable` with a pin per leg, because the
engines disagree there on purpose.

## Alternatives rejected

- **Answer the arithmetic in `double` always, and print with the mixed printer.** That is what `/` does,
  and it is right for true division because `/` is a float whatever arrives. For `+`, `-` and `*` it
  prints `8.0` where CPython prints `8` — the same class of wrong the earlier cycles filed for `round`
  and `floor`, and it loses the distinction the tag exists to keep.
- **Specialise the module on the kinds the program might put in the slot.** Sound and much faster, and it
  is the same machinery as the shape notebook — which is exactly why it is not this: the notebook does not
  go depth-by-depth yet, and a specialization that inferred the wrong kind would be a wrong answer, not a
  refusal. Deferred to L11.1's own completion (the row's "when a container can hold more than one kind,
  the operation is selected on the value's kind at run time" is what a real tagged value word — with a
  real word for the kind — is for).
- **Refuse, as before.** The refusal was correct and named its missing half; it was also the last refusal
  standing between this language and a list of lists, and there was an honest gate available that makes
  the answer exactly CPython's for every program it opens for.
- **Open the door for any slot read and raise on what turns out to be text.** Rejected outright: CPython
  *answers* `"a" + "b"` and `[1] * 2`. A door that raises where the reference returns a value is the one
  failure mode this compiler does not ship, and it is invisible to every check the suite has.
- **Emit the raise as a direct call to `@rt_raise` from inside the helper.** Faster by one branch, and it
  would make `except TypeError:` not reach the trap. ADR 0228 already settled which side a built-in trap
  is on.
- **Put the tag in the payload's high bits and skip the second word.** The tags are a separate `i8`
  array precisely so the collector can mark a slot without the payload's owner decoding it (ADR 0233);
  stealing payload bits would make the collector's traversal a decoder, and the float payload is a heap
  handle whose bits belong to the heap.

## Consequences

- `pkg/lang/numeric_slot_door.go` is the third program-wide pass codegen runs before emitting a module,
  and the first one whose answer is "may this operator be answered at run time at all". Its coarseness is
  now a documented behaviour, not an accident: adding a text to a container can close an arithmetic door
  in an unrelated part of the program. When that bites, the fix is to deepen the notebook (L11.1), not to
  widen the door.
- Two refusals that used to be reachable from this shape remain, and are now the interesting ones: the
  membership test and the loop over a run-time-built container (Gaps R.95, R.83), both of which need the
  haystack's *kind* where this cycle only needed the slot's *tag*.
- Newly measured and filed, not fixed here: `print(-"hi")` and `x = "hi"; print(-x)` answer
  `-281474976710658` interpreted and `0` compiled, on a program CPython stops with
  `TypeError: bad operand type for unary -: 'str'` — a silent exit-0 number on both engines, which is
  Gap R.137 and a worse bug than the one this cycle closed.
