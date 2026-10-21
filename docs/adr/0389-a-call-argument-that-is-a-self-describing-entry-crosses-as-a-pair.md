# A call argument that is a self-describing entry crosses as a (payload, tag) pair

**Roadmap:** Gap R.220 (closed here — a dict or list entry handed to a call answered the payload, not the
value) ·
L11.1 (the tagged value word in the positions that read one word) ·
Gap R.146 (the one-word position this row's argument used to be) ·
Gap R.361 (filed here — a callee that TESTS its own pair-carrying parameter still asks the payload) ·
Gap R.362 (filed here — a dict read inside a body reaches the unexplained `index must be a constant`) ·
ADR 0273 (the pair crosses a call; the rule this row applies to a new argument shape) ·
ADR 0327 (a dict *literal*'s read is answered by `subscriptPairOf`) ·
ADR 0331 (a list literal's element asks the same door) ·
ADR 0353 (an entry's kind is the object's fact, not the key's) ·
ADR 0387 (the signless call asks the entry's tag) ·
ADR 0388 (a condition asks a self-describing read its own kind) ·
ADR 0274 / Gap R.344 (the body the promotion owns, which this door must not enter) ·
ADR 0302 (the new answers are recorded from the reference, not self-certified)

## Status

Accepted. Landed 2026-10-21.

## Context

Every road that *renders* a self-describing entry already asks the object which kind it read. ADR 0353 made
`print(d[None])` print `3.5`, ADR 0387 gave `abs(d[None])` the entry's kind, ADR 0388 let `if d[None]:`
branch on it. One position had none of that: the entry handed to a **call**.

```python
d = {"a": "x"}
def id(v):
    return v

print(id(d["a"]))      # CPython: x — backend: 1, at exit 0
```

`1` is the text's index into `@str_tab`. The same program with each other entry kind printed the payload of
whatever word the read happened to lower into: a float entry printed its `@float_box` handle, a `None` entry
printed `0`, a list entry printed a heap slot number. All at the exit code of success.

The read itself was not the problem. `subscriptPairOf` — the door ADR 0327 built for a dict literal and ADR
0331 extended to a list literal — had answered that read correctly for two releases, and `print(d["a"])`
printed `x` the whole time. What was missing was the second word at the parameter: `pairCallScan` decides
which parameters arrive as two `i32`s, and it asked two questions — does some argument need a word the
ordinary road cannot give it, and does some argument carry a `double`. A read out of a literal container
answered neither, so the parameter stayed one word, `pairArgWords` lowered the argument through `g.value`,
and the kind was quietly left in the dict.

The asymmetry is what made this a row rather than a nuisance. The same value written **in place** refused:

```python
print(id({"a": "x"}["a"]))   # refused: "this position keeps one word for a whole value" (Gap R.146)
```

so the backend had two answers for one program, and the worse one was the one that ran.

## Decision

**The argument is asked of the entry, never of the key, and the emission asks the door — not the scan.**

1. **The scan grew one question.** `pairCallScan` already asked `anyNeedsWord` and `anyCarriesDouble` per
   parameter. It now also asks `anyReadOfLiteralSlots`: does any argument written to this position reach
   into a container the program spelled out and never wrote again, whose slots hold something a payload
   cannot mean on its own? The predicate is `readOfLiteralSlots`, and it is written as the scan's copy of
   the door's own two questions, not as a new rule:

   - `literalSideIsUnwrittenLiteral` mirrors the door's `runtimeContainerSide` — a `*DictLit`/`*ListLit`
     written there, or a `*Name` whose every recorded binding is such a literal and which the built-slot
     table does not claim;
   - `dictSlotsCarryKinds` / `listSlotsCarryKinds` ask whether some **value** is a text, a float, `None`, a
     verdict or a container — the kinds whose payload is a `@str_tab` index, a box handle or a slot without
     its tag. A key's kind is the door's business (`dictKeyCarriesPair`, ADR 0353) and never opens a
     parameter;
   - `pairSlotIsPlainInt` is the negative control made explicit: an entry the literal already describes as
     an integer (`d = {"a": 7}`) has a payload that *is* its number, the fold answers the read, the
     ordinary call road has always handed it across correctly, and it keeps the one word it always had;
   - `keyIsSpelling` mirrors the arm list the door can build a key pair from — the spellings
     `dictKeyCarriesPair` admits plus a name the scan can see the bindings of, which is evidence rather
     than a guess.

2. **The emission asks the door, not the scan.** In `pairArgWords`, an `*Index` argument asks
   `g.subscriptPairOf` — the same function `print`, `bool`, `abs`, `min`/`max`/`sum`, an f-string field and
   the container builders ask. Where the door answers, both words travel and `bindTaggedVar` writes the
   callee's slot beside its tag with the arithmetic origin recorded (ADR 0187, ADR 0267). Where the door
   declines, the road below keeps **its own** refusal, word for word.

   That ordering is the load-bearing part. A gate that decided the answer and an emitter that trusted it is
   how this family ships a payload wearing another kind's bits (ADR 0249's lesson, ADR 0232's "one question,
   asked once"). The scan may open a parameter; it may not decide what crosses it.

3. **The door stands aside for a body the promotion owns.** `pairBodyIsFloatReturnRoad` is the scan's
   syntactic copy of ADR 0274's question — does some `return` answer a double the module already knows
   about? Such a body is emitted `define double`, and there is no word beside a `double` for a pair's tag to
   live in; opening the parameter would turn a program that answers today into one `llc` rejects. That shape
   is **Gap R.344**'s exit-2 list, and this row neither widens it nor claims to fix it. The ladder this
   house runs on is that a paid door trades a refusal for an answer, never an answer for a crash.

4. **What the row does not reach stays owed, and is named.** Three shapes answer wrongly *below* the call
   and one refuses with a sentence that explains nothing. They are byte-for-byte what the parent commit
   produced — this row did not cause them and does not hide them:

   | program | compiled | reference | owner |
   |---|---|---|---|
   | a callee that tests its own parameter (`if v:`) | `1 1 1 0` | `0 0 1 0` | **Gap R.361** (filed here) |
   | a recursive callee that hands the parameter back | `3` | `3.5` | Gap R.146 (the tagged value word) |
   | a ternary whose chosen arm is the parameter | `3` | `2.5` | Gap R.147 |
   | a self-call that reads a module-level dict from the body | exit 1 `index must be a constant` | `0` | **Gap R.362** (filed here) |

   Each is a row of `integration/testdata/cpython-debt.json` with CPython's answer beside the compiled one
   and the roadmap row that owns the fix, and each is exercised by
   `TestWhatTheCallArgumentStillOwesTheOtherRowsOwn` — a row that stopped describing either side fails, and
   so does a row no case reaches.

## Consequences

- `d = {"a": "x"}` / `def id(v): return v` / `print(id(d["a"]))` prints `x`. A float entry prints `3.5`, a
  `None` entry prints `None`, a list entry prints `[1, 2]`, a dict entry prints `{'k': 1}`, an empty text
  prints the empty line, `-0.0` prints `-0.0` — every one of them CPython's answer at exit 0.
- The same door answers the same read written three ways: bound (`d = {"a": "x"}` then `d["a"]`), written in
  place (`{"a": "x"}["a"]`), and out of a container the program wrote again (`d["b"] = "y"`).
- Keys stay spellings. `w = {None: 3.5, True: "t", 0: [4]}` reads `w[None]`, `w[True]`, `w[0]` each as the
  entry the object says, which is ADR 0353's rule restated one boundary further out.
- An integer entry is unaffected: `twice({"a": 3}["a"])` is still `6` on the one-word road, and the IR rows
  fail if it starts asking a pair it does not need.
- The literal-in-place asymmetry is gone: the two spellings can no longer disagree, because they go through
  one door.
- The refusals that remain still refuse, and still name the missing half: a bound entry handed to a call
  (`x = d["a"]` → Gap R.146), and a mixed-kind list element asked to concatenate below the call (Gap R.82).
- The capability registry's `lower.unsupported.mixed-element-pair` sample had to move. It was
  `print(id(xs[1]))` — a program this row pays — and a registry whose sample compiles clean is a claim about
  nothing, which its own guard fails either way. It is now `xs = [1, "a"]` / `print(xs[1] + 1)`, an operand
  that really does keep one word.

## Agentic rationale

Nothing about the machine path changed: same flags, same exit codes, same refusal classes. What changed is
that 24 shapes moved from a wrong answer at exit 0 to CPython's answer, which is the only class of change an
agent consuming this compiler can act on without re-checking it against the reference. Two things keep that
claim honest at the interface level:

- the record leg is the authority for the new answers — 41 sources added to
  `pkg/lang/testdata/interpreter-golden.json` (8223 → 8264) plus the conformance program (→ 8265), every one
  recorded from CPython and cross-checked by the compiled leg, never self-certified (ADR 0302);
- the reference leg is the authority where the compiled answer still differs — the four rows above are in
  `integration/testdata/cpython-debt.json` (6 → 10 rows), each with the roadmap row that owes it, so
  `--oracle` and the matrix report the disagreement rather than hide it.

`integration/programs/dict_entry_call_argument.gy` is the single-program witness: matrix row
`programs/dict_entry_call_argument`, oracle `match`, nine lines whose expectation is what CPython printed for
it. The program deliberately does **not** contain a callee that tests its own parameter — a conformance
program is only on the matrix while every line it prints is the reference's, and Gap R.361 owns that line.

## Alternatives rejected

- **Refuse the position instead.** The one-word road already had a refusal sentence, and closing the
  position with it would have made `print(id(d["a"]))` exit 1. That is ADR 0273's forbidden move — a gate may
  not cost a program that compiles today — and here it would have been doubly wrong, because the program
  compiles today *and answers wrongly*: the refusal would have been an improvement in honesty bought with the
  answer the row exists to deliver.
- **Let the scan decide the words, and have the emitter trust it.** Rejected for the reason ADR 0249 and ADR
  0232 exist. The scan is AST-only and cannot see the emitter's notebooks; a scan that answered for the read
  would be a second opinion about one question, and every previous row in this family shipped either a
  payload wearing another kind's bits or a refusal nobody measured. The emitter asks the door; the scan only
  decides whether a parameter has room for two words.
- **Generalise `anyNeedsWord` to "any subscript".** Cheap, and wrong: it opens parameters for reads the fold
  answers in one word — every all-integer literal — and for reads of a container the run time built, which
  the built-slot arm already owns. The measured form of this is the pair of negative controls in
  `TestTheCallArgumentScanIsAskedOfTheEntryNotTheKey`: an integer entry, an integer list element and a name
  the program rebound all answer `false`, and a scan that over-opens fails them.
- **Include the float-returning bodies and fix `llc` later.** Rejected with the exit-2 evidence in hand.
  `def f(v): return v + 1.0` called with an argument the pair road opens emits `define double` beside a call
  site that passes two `i32`s, and `llc-20` refuses the module. Gap R.344 already pins that shape as an
  exit-2 row; widening its list to cover container entries would have bought no answer and spent the
  contract's compiler-bug code.
- **Fix the callee's `if v:` in the same commit.** It is a different door (`emitCond`/`pairTruth` reading a
  parameter instead of a read), it is wrong identically on the parent build, and ADR 0388's rule is that the
  truth door owns conditions and nothing else. One commit per feature means filing it — **Gap R.361** — with
  the debt row that pins today's answer beside the reference's.
- **Pin the four wrong answers as `want` in the answer table.** A conformance test cannot assert a wrong
  answer: the reference check runs first and the case would fail on its own expectation. Filing them in the
  ledger, exercised by a table that fails if the program stops being measured, is the form that keeps both
  the truth and the debt.

## Codegen / IR implications

- No new runtime helper, no new IR block. The argument takes the pair road that ADR 0273 built: the `define`
  gains its second `i32`, `bindTaggedVar` writes value and tag, and the callee's own pair bookkeeping
  (`pairSlotsForBody`, `numericPairVar`) decides what the body asks.
- The emitted read is the one `subscriptPairOf` emits — `@rt_dict_get_tagged` / `@rt_dict_value_tag` on the
  dict side, `@rt_get_elem` with the tag register on the list side — so a module cannot contain a second,
  disagreeing rendering of `d["a"]`.
- IR-shape rows assert both directions: the entry-handed-to-a-call program carries the pair door and the tag
  read, and the integer-entry control carries **neither** — a scan that starts opening parameters it has no
  business in fails there rather than shipping a slower module nobody notices.
- Every module this row emits passes `llc -filetype=null`; the exit-2 list is unchanged from the parent
  commit, which is the assertion `TestWhatTheCallArgumentRefusalsLeaveTheTrapCodesAlone` exists to keep true.
