# ADR 0250 — an ordering of slots asks the tag which pair it was given

Date: 2026-10-02 · Status: Accepted · Roadmap: L11.1 (Gap R.82), ADR 0247, ADR 0248, ADR 0249, ADR 0166

## Context

ADR 0247 gave a slot read's *equality* the `(payload, tag)` pair the printer already carried, and ADR 0248
taught an ordering of two texts to read the text (`@rt_str_order`) instead of the interned index. What neither
could answer was the ordering whose operands are slots whose kind lives in the object:

```
xs = [1, "a"]
print(1 if xs[1] > "a" else 0)   # CPython 0 · --interp 0 · --aot refused  ("needs a single static kind")
xs = [1, "a"]
i = 0
print(1 if xs[i] > "z" else 0)   # CPython TypeError · --interp TypeError · --aot refused
```

Such a program is not one comparison but three. `xs[1] > "a"` is two texts (answered by `strcmp`), and
`xs[0] > "a"` is a number against a text, which CPython refuses with a `TypeError` that names the left
operand's type first. Which of the three it is cannot be decided in the compiler: the container's literal holds
both kinds, and only the tag written beside the payload knows what this slot reported today.

Two wrong answers were available, and both had been taken before by a neighbouring path. One was to compare
the payloads and print whichever verdict came out — the trap ADR 0248 found for texts and Gap R.85 records for
kinds. The other was to lower the read anyway and emit an instruction with a missing operand, which is exit 2:
LLVM rejecting a module the compiler wrote, ADR 0166's own bug class.

## Decision

An ordering with a slot read on one side, and text among the kinds either side could report, is lowered to a
three-arm choice whose tests are the two sides' tags.

- **Two numbers** — each side is lifted to a `double` (a float slot unboxes out of its `@float_box` object, an
  int or bool slot converts) and compared with `fcmp olt/ole/ogt/oge`. An `icmp` on two payloads cannot hear
  `1 < 1.5`, and an ordering of mixed int/float slots is a question about numbers.
- **Two texts** — `@rt_str_order`, the helper ADR 0248 put behind the static ordering. Equality stays an index
  comparison because interning is content-addressed (ADR 0173); ordering is the one thing that must read text.
- **A number and a text** — `@raiseTo` with CPython's own sentence, `'<' not supported between instances of
  'int' and 'str'`. Which side's name is printed first is settled by the tags when both sides could be either
  family, because CPython always names the *left* operand's type first.

The arms are emitted only when they can happen. A side the compiler already read answers "are you text?" and
"are you a number?" with a settled yes or no, an arm whose test is settled `false` is dropped, an arm whose test
is settled `true` ends the chain, and the merge `phi` lists exactly the blocks the chain can reach. A block that
no branch enters but the `phi` names is the module `llc` rejects — the exit class this whole line of work exists
to stay out of (ADR 0166). Where the tags would have to be guessed — a container a loop built, a slot that could
hold a container, numbers that could be ints or floats at once, so that CPython's sentence would name the wrong
type — the door steps aside and the front end refuses by naming the shape.

The gate reads two sources: the literal that built the container (`containerLits`, which gives the element kinds
and therefore the type names the raise arm prints) and the mutation-tracked kind maps (`kindMapsFor`, which knows
about `append` and item assignment since ADR 0210). Both are needed: the literal alone would let a grown
container order as though it still held what it was written with.

## Consequences

- `xs[1] > "a"`, `xs[i] < 5`, `5 > xs[0]`, `xs[0] < 1.5`, a mixed slot in an `if`, in a `while` condition and in
  an `and` compound all answer CPython's answer on the compiled path, and the traps raise CPython's sentence
  rather than being refused at compile time. `pkg/lang/slot_ordering_test.go` pins the parity, the traps on both
  engines, the refusals, and the bounds check; `integration/slot_ordering_test.go` pins the same tables against
  `python3` at the command line, where exit 2 on any row is a failure of the file.
- One refusal pin had to break: `TestSlotEqualityRefusesWhatItCannotProve/an ordering comparison of a mixed slot`
  pinned as owed exactly the program this door answers. It moved to the parity table, which is the only way a
  refusal pin can be trusted — a pin that never breaks is not measuring anything.
- The read is unchanged, so the `IndexError` a subscript always raised still fires first, in the block the
  comparison starts in: `TestSlotOrderingKeepsTheBoundsCheck` fails if the tagged read loses the check.
- The three arms cost real IR (about thirty instructions per comparison). That is the price of a run-time
  question, and it is bounded: settled sides fold their tests away, so a comparison the compiler can settle
  still compiles to the static door beside it.

## Measured, not fixed here

Three defects surfaced while measuring this door, all pre-existing, each with its own row and none silently
absorbed: **Gap R.91** (a loop variable over a container that mixes kinds prints a verdict for a `TypeError`),
**Gap R.92** (an ordering of dict value slots answers by the interned index), and **Gap R.93** (an ordering of a
slot in a container a loop built prints a verdict for a `TypeError`). Containers ordering against containers stay
with **Gap R.86**/**Gap R.87**'s elementwise helper, and the ordering across kinds the compiler *can* see stays
**Gap R.85**'s.

## Alternatives rejected

- **Compare the payloads and let the verdict land.** 500x worse than a refusal: `print(1 if xs[1] > "a" else 0)`
  would print `0` or `1` for a program CPython kills, and a printed number is a fact the program did not earn.
  Gap R.85 counts exactly this class as a wrong answer.
- **Reuse `@rt_payload_eq`.** Equality has one question (same kind, same value); ordering has three, and two of
  them need to look at the bytes and the boxes. One helper answering both would end up a switch over tag pairs
  with an ordering hidden inside an equality.
- **Reuse `@taggedDoubleFromSlot` for the numeric arm.** It carries raise arms for non-numeric tags and computes
  the *other* operand's type name from its argument — for `xs[i] > "z"` that name is a text, so the shared lift
  declined the whole door. The numeric arm needs an unboxed value, not a sentence.
- **Emit every arm and let LLVM prune the dead ones.** A `phi` names its predecessors whether or not a branch
  reaches them, so an unreachable arm is a verifier failure, not an optimisation opportunity: the first build of
  this door died with *PHI node entries do not match predecessors*.
- **Trap with `unreachable` for a tag the literal ruled out.** A block cannot be written underneath the one the
  caller is still filling in — the trap landed inside the comparison's own merge block and made the tail hold two
  terminators. The numeric arm is entered only when the tag already said "number", so the int/bool conversion is
  total and needs no fourth answer.
- **Answer containers elementwise here too.** That is a second helper walking two objects' slots — `
  rt_container_order`, which is Gap R.86's own row, with `min`/`max`/`sorted` waiting on it. Ordering a slot
  against a container is refused rather than guessed.
