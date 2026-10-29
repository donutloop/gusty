# ADR 0456 — membership asked of a container the SOURCE spells: the haystack goes to the door that builds it, not to the road that folds it

- **Status**: Accepted
- **Date**: 2026-11-02
- **Roadmap**: **Gap R.523** done — filed 2026-11-01 by **ADR 0453**, from the sweep that closed **Gap R.522**,
  and widened the same day by **ADR 0454**'s sweep. Two residues this door declines are filed beside it:
  **Gap R.530** (a literal element whose answer IS a container) and **Gap R.531** (a literal element no door
  names an answer for, an attribute call).
- **Builds measured**: parent `c6a458f` (**ADR 0455**'s count update) and this commit, every byte from the
  gate's `-tags=llvm20`.

## Context

One literal, three roads, two answers. This is the program that filed the row:

```
$ gustyc --eval 'x = 5
print([x])'          →  [5]
print(len([x]))'     →  1
print(1 in [x])'     →  exit 1: codegen: list literal elements must be integers, not int
```

`print` and `len` ask the **container door**, which builds a heap object whose slots carry a kind beside every
payload. Membership asked the **number road** — `g.value(b, n.R)` — whose answer for a literal is a folded
`@.lstN` global of `{i32, [N x i32]}`: a length beside an array of `i32` words. A name, a call, an arithmetic
expression has no word to write into one of those, so `emitList` refused the whole program, with a sentence
about an element kind the literal was not asking about — `not int`, for a list of `int`s — which is **ADR
0166**'s second clause ("a message must name the missing thing") sitting inside the first ("a program the
reference runs must not be refused").

The refusals were the smaller half. The folded road that *did* run compares payloads alone and asks nothing of
either side's kind, and the answers it produced at the exit code that means the program ran were:

| program | this compiler, before | CPython |
|---|---|---|
| `print(None in [0])` | `True` | `False` |
| `print(1.0 in [1])` | **exit 2** — `ordering a number against a list[int] …` | `True` |
| `print(1.5 in [1, 2])` | **exit 2** — same | `False` |
| `print([1] in [1, 2])` | **exit 2** — `llc: global variable reference must have pointer type`, from `%t3 = icmp eq i32 @.lst1, 1` | `False` |
| `print(1 in [1.0])` | `False` | `True` |
| `x = 5` / `print(1 in [x])` | **exit 1** — refusal | `False` |

Two of those are exit 2 — the exit class the contract reserves for the compiler's own mistake — for programs
the reference prints in one line.

The reason the fold could answer `None in [0]` at all is the same reason it could not answer `1.0 in [1]`: it
lowers a needle to the word it occupies and compares that word with a slot's word. The void's word is `0`; a
double's word is the handle of a `@float_box`, which is why the numeric cross-kind arm had to live in
`rt_payload_eq` (**ADR 0233**) and why the fold reaching `1.0 in [1]` was refused by the ordering door above
it rather than answered.

## Decision

**1. One door, asked where the other membership doors are asked.** `membershipOfLiteralContainer` sits in the
`in`/`not in` block at the head of `valueBinOp`, beside `mixedMembership` and above every operand lowering —
the position **ADR 0365** established for `membershipOfBuiltSequence` (`2 in [1] + [3]`) and for every door
whose operands cannot be lowered before the question is asked, because the ordinary lowering of the operand IS
the thing that refuses. It is the same shape one spelling earlier: that door takes the haystack an OPERATOR
builds, this one takes the haystack the program WRITES.

**2. The haystack is built by the door that already builds it.** `containerOperand` — the question `print([x])`
asks — decides tagged-or-flat and fills the slots. This door mints no builder of its own, and the test says so
in the module's bytes: the program that PRINTS `[x]` and the program that asks MEMBERSHIP of `[x]` emit the same
`@rt_alloc(`/`@rt_set_elem(`/`@rt_tag_elem(` counts. **ADR 0232**'s one question, asserted rather than argued.

**3. The question goes to the object: `rt_contains_tagged(hay, payload, kind)`.** The needle arrives as a
`(payload, kind)` pair from `taggedOperand`, never as a payload alone — the rule **ADR 0241** set for a read
haystack and **ADR 0365** for a built one, applied to a spelled one. That is what makes the second half of the
table above come out right: the pair is what lets `rt_payload_eq` do its cross-numeric work (`1.0 in [1]` is
`True` because the tags differ and the numbers agree), and the same pair is what stops the void's `0` from
being the number `0`.

**4. The folded road keeps exactly what it can serve, and that is two halves.** It still owns a haystack whose
elements it can write into a global at emit time **and** whose needle it can name — a whole number or a verdict
compared `icmp eq i32`. `foldNamesNeedle` reads the same kind table the slot builder reads (`elemKindTag`), so
the fold and the door cannot disagree about which needles are which. Declining on the first half is what keeps
`x in [1, 2, 3]` the zero-allocation unroll it always was, asserted by a row that fails if `@main` grows an
`@rt_alloc(` for a literal the fold serves. Taking the second half away from the fold is what fixed
`None in [0]` and `1.0 in [1]`: a needle whose kind the fold cannot name is not a needle the fold may answer.

**5. A decline falls through to the refusal that already names its owner.** Where `containerOperand` declines
(a shape no builder covers) or no pair can be named for the needle, the door returns "not mine" and the ordinary
roads below answer — including the refusals for **Gap R.530** and **Gap R.531**, which are pinned with their own
words and leave this file when those rows pay. A door that minted its own vaguer refusal for a shape it declined
would hide the row that owns it.

**6. Order of effects is the reference's.** The haystack's elements are evaluated by the builder, so
`print(5 in [probe()])` where `probe` prints prints first. A door that declined after building leaves the object
dead and the effect performed — the rule `membershipOfBuiltSequence` already keeps for the same case.

## Consequences

- The membership question about a literal has one answer at every position it can stand in: `print`, a
  condition, a `while` test, a `for` iterable, `len` beside it. The conformance program
  `integration/programs/membership_literal_haystack.gy` walks those positions, because "the road below answered
  differently" is how this class of defect hides.
- **43 record entries of this row's own** (and **73** across the cycle, 10386 → **10459**) were written from the
  reference by `tools/recmerge`, the compiled leg cross-checked. This row's: 32 from the new door's own tables, 8
  from the control table in `pkg/lang/membership_name_test.go` (whose two `Gap R.523` refusal rows moved into the
  answered family; that table is written to fail when a pinned refusal starts compiling, so a paid row cannot
  quietly stay), 1 for `print(0 in ["a", 2])`, the mixed-literal twin that `pkg/lang/generator_buffer_kind_test.go`
  had pinned as **Gap R.478**'s wrong answer and that this door answers, and 2 for whole corpus programs. The other
  30 came from the guard this cycle adds and the row it files (**Gap R.532**): `TestEveryAssertedCorpusProgramIsOnTheRecord`
  asks every asserted program under `integration/programs/` for its record, and 24 of them had none — including
  `integration/programs/unpack_container_claims.gy`, which **ADR 0455** shipped in a documentation-only commit with
  no record at all. A row the ledger declares `not_applicable` is exempt by the artifact's own declaration: the
  reference cannot run a gusty-only program, so the record has no answer to hold for it.
  The owes tables the generator row came from now hold only the buffer's
  own arm: `pkg/lang/generator_buffer_kind_test.go::TestWhatAYieldedKindStillCannotReachIsFiledNotFixed` and
  `integration/generator_buffer_kind_test.go::TestWhatTheYieldedKindStillOwesAtTheCLI` still pin
  `print(0 in c.m())` at `True` against the reference's `False`, which is **Gap R.478**'s remaining half — and the
  two rows beside each other are what prove the defect is the generator's door and not membership's.
- The float-needle refusals this row retires were the ordering door's (`Gap R.37`'s message), reached because
  the membership question never got to be asked. They are not a new capability of the ordering door: `2.5 <
  [1, 2]` still raises the reference's `TypeError`, and `2.5 == [1]` still answers `False`.
- The residue is named, not answered: a literal element whose answer is itself a container (**Gap R.530**) and
  an element no door names an answer for (**Gap R.531**) still reach `emitList`'s refusal, pinned in
  `TestWhatTheLiteralHaystackDoorStillDeclines` and at the CLI.

## IR implications

- A served haystack: no `@rt_contains`, no `@rt_alloc(`, no `@rt_set_elem(`, no `@rt_tag_elem(` in `@main` — the
  answer is an `icmp eq i32` chain folded at emit time.
- A built haystack: the three builder calls, exactly as many as the printer emits for the same literal, then one
  `@rt_contains_tagged(i32 %hay, i32 %payload, i32 %kind)` and an `icmp ne i32 … 0` widened with `zext`.
- The untagged `@rt_contains(` is absent from a program this door answers: it is the payload-only scan whose
  answer the door replaces, and its presence in `@main` is a test failure.
- `not in` is the same call with `icmp eq … 0`, so the two spellings cannot drift apart.

## Alternatives rejected

- **Widen `emitList` to accept non-constant elements.** That changes what the folded global means for every
  road that reads it (`isConstListLit` gates indexing, printing, equality), and it makes the answer to "is this
  literal constant?" depend on whether the elements happen to be lowerable — the exact kind of fact **ADR 0343**
  outlaws. The folded road would then be a fast path that silently becomes a different data structure.
- **Register an unannotated name's kind in the compiler's kind maps so the fold accepts it.** This is what the
  row's own wording invited ("an element that is a NAME holding a number still does not reach the builder"), and
  it is **ADR 0453**'s rejected move in a new place: a name-keyed summary is a report about the assignments a
  pass has seen, not a promise about the value the read receives. It would answer `1 in [x]` by guessing.
- **Let the fold answer the needles it cannot name, with a tag check bolted on.** That is a second equality
  beside `rt_payload_eq`, which **ADR 0232** and **ADR 0233** exist to prevent; `1.0 in [1]` got a cross-kind
  arm exactly once, in the runtime helper, and the fold must not acquire its own copy.
- **Hoist the whole membership block above the ordering door.** One door's problem is not every door's move; the
  block contains roads whose operands the ordering door already answers correctly (`2.5 == [1]`), and moving them
  would re-decide those.
- **Refuse the float-needle shapes instead of answering them.** They were the pre-existing state, and two of
  them were exit 2. A refusal is the honest artifact only where no answer exists; here the object answers.

## Machine path

Nothing new to discover: the door adds no flag, no diagnostic class and no schema field. What an agent can read
is unchanged and now true of more programs — `--emit-llvm` shows the tagged scan for a spelled haystack and
nothing but a fold for a constant one, `--verify` passes the modules that used to die in `llc`, and the record
leg (`pkg/lang/testdata/interpreter-golden.json`) holds the reference's answer for all 40 sources this row
added, so none of the verdicts above is self-certified by the compiled leg (**ADR 0302**).
