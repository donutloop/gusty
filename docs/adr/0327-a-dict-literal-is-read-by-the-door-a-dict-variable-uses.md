# ADR 0327: a dict literal is read by the door a dict variable's entries are read by

- **Status**: Accepted
- **Date**: 2026-10-13
- **Deciders**: Agent (cycle), Language designer (roadmap)
- **Roadmap references**: L11.1 (the tagged value word — this is a subscript *gate* on top of doors L11.1 has
  already built), Gap R.215 (this row, ⏳ `OPEN` → ✅ `DONE`), Gap R.216 (a repeated key in a literal, measured
  and paid in the same commit), Gap R.217–R.219 (measured while probing the door, filed not fixed), Gap R.37 /
  ADR 0326 (the raise the missing-key case reaches), Gap R.146 / ADR 0273 (the one-word positions this row
  leaves as refusals), Gap R.189 (a `KeyError` whose key has no literal to quote), Gap I.2 (the "a compiled dict
  literal holds constant integer keys only" sentence this row retires for the read)
- **Supersedes / extends**: extends ADR 0187 (the word that writes a payload writes its tag beside it) from the
  write side to the *read* side of a literal; applies ADR 0232's per-slot tags to a container the compiler is
  looking at rather than one it is holding; reuses ADR 0241/0251's `taggedSlotPair`, ADR 0303's one
  tag-reading printer, ADR 0326's raise; continues ADR 0166 (exit-code contract) and ADR 0154 (`llc`/`opt` as
  the exit-2 witness).

## Context

Three programs, one construct:

```
print({"a": 1}["a"])        # the reference prints 1;               gustyc printed codegen: index must be a constant (exit 1)
print({"a": 1}["b"])        # the reference raises KeyError: 'b';   gustyc refused the same way        (exit 1)
d = {"a": 1}
print(d["b"])               # KeyError: 'b' at exit 3                                              ✅
```

The literal — the *most* constant base the language can write — was the only base the language could not read
with a key that was not an integer. Not because the answer did not exist: the read a dict **variable** performs
builds the dict, hands the key over as a (payload, tag) pair, asks `rt_dict_has_tagged`, and raises
`KeyError` with the key's repr from the program's own arm. Every one of those parts was in the tree and in
production use, from ADR 0232 (per-slot tags), ADR 0241/0251 (`taggedSlotPair`) and ADR 0326 (the raise).

What was missing was a **gate**. The subscript road begins with:

```go
if il, ok := n.Idx.(*IntLit); ok { key = il.Value } else {
    if nm, ok := n.Obj.(*Name); ok { /* registered container? continue */ }
    else { return "", fmt.Errorf("index must be a constant") }
}
```

A base that was not a registered name and a key that was not an integer literal never reached the
`switch obj := n.Obj.(type)` below, whose `case *DictLit:` arm could only ever be handed an integer key — and
whose `dictLiteralKeys`/`dictLiteralVals` helpers admitted nothing but integer constants anyway. So the
compile-time fold was not a fast path in front of a general road; it was the *only* road, and a dict literal
with a text key, a float value, a `None` value or a nested container had no answer at all. The sentence
`codegen: a compiled dict literal holds constant integer keys only; CPython supports string and other keys`
was the backend describing its own gate as if it were a property of the language.

The second half of the row is the half that decides the design. Once the literal is allowed to reach the
object, `{1: 2}[True]`, `{True: 9}[True]`, `{1: "a"}[True]`, `{"a": [1, 2]}["a"]` and
`print({1: None}[1])` all answer themselves — because the answer is not a payload, it is a **(payload, tag)
pair**, and the door that produces it is the one `print`, `str()`, `repr()`, an f-string field, a binding, a
`len`, a membership test and a loop already know how to consume. Writing a second "read a literal dict"
helper would have produced exactly the kind of drift this repository keeps paying for: two answers to one
question, one of them quietly wrong about a float.

## Alternatives Considered

1. **Widen the fold to text keys** — compare the key against `dictLiteralKeys`/`dictLiteralVals` extended to
   strings, floats, bools and `None`, and keep everything compile-time. Rejected: the fold returns *one word*,
   and the whole lesson of ADR 0232 onward is that one word is not a value. `{"a": "x"}["a"]` would hand the
   print road an `@str_tab` index and `print` would write a number; `{1: 1.5}[1]` would hand it a `@float_box`
   handle. Each of those would need its own tag-carrying patch in every consumer, which is the many-doors shape
   ADR 0325's lessons keep condemning.
2. **Widen `dictLiteralKeys`/`dictLiteralVals` themselves** so the whole literal family — subscript,
   `.get`, `in`, `len` — reads non-integer constants. Rejected: those two helpers are also the *storage*
   predicate for the static `{count, [N x i32]}` global (`emitDict`), and widening them silently moves dict
   literals off the static layout, which is a different decision with its own witnesses (it changes
   `sum`/`min`/`max` folding, the printer's kind choice, and the GC's root story).
3. **Materialise the literal into a synthetic variable and reuse the variable road** (register
   `runtimeDicts["_lit3"]`, emit an alloca, store the handle, then run the name road). Rejected: it drags in
   the whole name-keyed bookkeeping (`dictKeyStr`, `mixedDicts`, `dictValStr`, definite-assignment, the collector's
   root set, the escape analysis's dead-list pass) for a value that lives in one expression, and every one of
   those maps is a place where a synthetic name can be mistaken for a program's variable. `taggedSlotPair`
   already takes a *handle*, not a name — the bookkeeping is the cost of a variable, and this value has none.
4. **Answer the one-word positions anyway** (`return {"a": "x"}["a"]`, `g({"a": "x"}["a"])`), since the answer
   is "usually" right. Rejected on measurement: the compiled leg printed `1` for `x` and `0` for `None` at
   **exit 0** — the reference's value replaced by the integer underneath it. The contract's worst class, and
   precisely what Gap R.146 exists to pay for. These positions now refuse with the missing half named, joining
   the pair road's existing one-word refusals.
5. **Fix the fold's duplicate-key rule and call it Gap R.216 later.** Not rejected — done, in this commit, because
   it was measured here and it is two lines: `{1: 2, 1: 3}[1]` is `3` in the reference and `2` from a fold that
   scanned forwards while the dict is built by insertion. A defect measured while landing a door is paid with
   the door, not filed and left.

## Decision

**A dict literal is read by `taggedSlotPair`, the one door that answers "which (payload, tag) pair does this
subscript denote", and the compile-time fold keeps exactly the shape it can read.** In
`pkg/lang/dict_literal_read.go`:

- `dictLiteralFoldAnswers(dl, key)` is the fold's own gate: an **integer** key *and* every key *and* every
  value of the literal an integer constant. Both halves matter — `{0: 2}["a"]` sits over an all-integer
  literal, and a fold asked to look up a text key in an integer table answers `2` where the reference raises
  `KeyError: 'a'`. A fold may only be asked the questions it can read.
- `literalDictPair` builds the entry with `heapDictFrom` (the binding road's builder, tags and all), lowers the
  key with `taggedOperand`, raises a missing key with `checkKeyReadTagged`, and reads the answer's kind with
  `rt_dict_value_tag` beside its payload. It constructs an `*Index` and calls `taggedSlotPair` rather than
  duplicating the three calls: one door, two bases.
- `runtimeContainerSide`/`runtimeSlotPairDeep` admit a `*DictLit` base, which is what hands the read to
  every consumer that asks the pair door — `renderPair` (`str`/`repr`/f-strings), the print dispatch, the
  binding road — without each of them growing a literal-dict case.
- `dictLiteralValueIsString` answers the printer's "is the answer a text?" **per entry**, because the
  dict-wide `dictValStr` record that serves a variable records "some value of this dict is text" and a literal
  has no name for that record to live under.
- `constantKeyOf`/`constKey.sameAs` implement the reference's key-identity rule for the constant key kinds:
  `1`, `1.0` and `True` are one key, and a text never is. Two integers compare as integers (exactly, at any
  width); only a key *written* as a float reaches the float comparison.
- The fold scans its keys **backwards** (Gap R.216): a repeated key is one key written twice, and the later
  entry wins the value.
- The one-word positions — a `return`, a call argument — refuse with `literalDictOneWordErr`, naming the
  missing half and the row that owns it (Gap R.146), at exit 1 and never exit 2.

## Consequences

- **Positive**:
  - 29 answer rows and 7 raise rows move from exit 1 to the reference's answer: text/float/`None`/bool keys,
    text/float/`None`/bool/container entries, an entry read three deep, a bound entry, `len`/`in`/`for` over an
    entry, `str`/`repr` of an entry, arithmetic and concatenation over an entry.
  - `print({"a": 1}["b"])` exits **3** with `KeyError: 'b'` and the program's own `except KeyError:` runs; the
    same is true of `{"a": 1}[1]`, `{1: 2}["a"]` and `{0: 2}["a"]` — the last being the case a widened fold
    would have answered `2`.
  - 39 record entries (6130 → 6169) through `tools/recmerge`, including the whole conformance program; the
    recorder's refusal to write a source the two legs disagree on is what certifies them.
  - `programs/dict_literal_key.gy` joins the corpus (191 → 192 programs, 180 → 181 matrix rows, 142 → 143
    parity-asserted, 127 → 128 `match`), printing CPython's output line for line.
  - Two sentences leave the vocabulary for these shapes: `index must be a constant` (for a dict-literal base)
    and `a compiled dict literal holds constant integer keys/values only` (for a read). The static *storage*
    gate is untouched, which is the difference between this cycle and rejected alternative 2.
  - `{1: 2, 1: 3}[1]` answers `3` (Gap R.216 paid the same afternoon it was measured).
- **Negative**:
  - `def f(): return {"a": "x"}["a"]` and `g({"a": "x"}["a"])` refuse at exit 1. They measured as `1` at exit 0
    with a naive routing, and Gap R.146 — "a pair handed across a call" — is the row that owns making them
    answer. The refusal names that row and the pair, and `TestTheOneWordPositions…` (unit + CLI) requires both.
  - A dict literal's read materialises the dict on the heap where the fold does not: one `rt_alloc` per read.
    It is the same cost the print position already pays for `print({"a": 1})`, and the fold's shape is asserted
    to carry none of it (`TestTheLiteralDictReadRoutesThroughTheObjectsDoor`'s negative control).
  - `print({(1, 2): 3}[(1, 2)])` still refuses — a tuple is not a value in this language yet (L11.3), and the key
    has no tag to travel with, which is Gap R.212's residue seen from this door.
  - Three defects measured on the way and filed rather than bundled: **Gap R.217** (a dict read by a container
    key raises `KeyError: key not found` where the reference raises `TypeError: unhashable type: 'list'` — the
    variable road says the same thing, so the literal inherits an existing debt), **Gap R.218** (`print(["a","b"][0])`
    prints `0`, `print([1.5, 2][0])` prints `1` — the *list* literal's read still answers the payload where this
    cycle gave the *dict* literal the pair), and **Gap R.219** (a function is registered as returning text on the
    evidence of one arm, so `def f(x): if x: return "x" / return 0` prints `x` twice).
- **Neutral**:
  - `KeyError: key not found` for a key with no literal to quote (`k = "z"; print({"a": 1}[k])`) is Gap R.189's
    existing residue, and the literal road behaves exactly like the variable road, which is what "one door" is
    supposed to mean.

## Verification

- Unit (`pkg/lang/dict_literal_key_test.go`) — every row asks the **compiled leg** (`compiledOut`: `Compile`
  then `lli`, so a refusal or an unexpected exit fails the case) and the traps ask the **JIT** (`jitTrap`, which
  fails a program that answers where the reference raises and reads the class and the sentence back off the
  runtime's own words):
  - `TestADictLiteralAnswersTheEntryTheKeyNames` — 29 rows: keys of every kind, entries that are text, float,
    `None`, `bool`, a list, a dict, a set; an entry read three deep; a bound entry; `len`/`in`/`for` over an
    entry; `str`/`repr` of an entry; arithmetic and concatenation on an entry; the fold's repeated-key rule.
  - `TestAMissingKeyOfADictLiteralRaisesWhatTheReferenceRaises` — 7 rows, class **and** message, including
    `{0: 2}["a"]` (the case a widened fold would have answered `2`) and a key the fold could not read.
  - `TestTheLiteralDictsKeyErrorIsCatchable` — the handler runs; the entry is a value the program can handle.
  - `TestTheFoldStillAnswersWhatItCanRead` + `TestTheFoldKeepsTheLastValueOfARepeatedKeyWhenTheFoldReadsIt` —
    the fold's shapes, and the fold's module asserted **not** to carry the object doors.
  - `TestTheLiteralDictReadRoutesThroughTheObjectsDoor` — the same three door names asserted **present** where
    the fold cannot answer, so the negative control above is a control and not an empty assertion.
  - `TestADictLiteralReadIsNotPrintedByPrintf` — the program's own `@main` body is read (the runtime helpers
    own their own `%d`): a text entry goes to the text printer, a container entry to the tag-reading printer,
    and neither to a numeric format.
  - `TestTheLiteralRoadIsNeverWorseThanTheVariableRoad` — 11 rows: where both roads answer they answer alike
    and the reference's way; the literal road may answer where the variable road refuses; and the literal road
    may **refuse** only where the variable road is not answering the reference either — the shape that keeps
    Gap R.220 visible instead of smoothing it over.
  - `TestTheOneWordPositionsRefuseTheLiteralDictRead` (4 rows) and `TestAPlainIntEntryStillTakesTheOneWordRoads`
    (2 rows) and `TestAnUntypableDictLiteralKeyRefuses` — the refusals name the pair, the one word and their own
    gap numbers, and the plain-int rows keep the refusals from being a blanket.
- Integration (`integration/dict_literal_key_test.go`): 13 CLI rows run against the reference (`oracleTrap`) on
  the CLI engines and on the `--aot` pipeline, plus `compiledRun` as the record leg, each required to print the
  reference's bytes and never to say `codegen:`; 4 missing-key rows assert exit 3, the `KeyError: <key>` sentence
  and a printed traceback; 2 catchable rows assert exit 0 with the handler's line; 2 refusal rows are required to
  stay at exit 1 with the sentence, so paying Gap R.146 has to break a test rather than a memory; and
  `TestTheDictLiteralKeyProgramIsAnAssertedMatrixRow` keeps the new program asserted and `match` in the matrix.
- Corpus: `integration/programs/dict_literal_key.gy` (28 lines) is `match` on both legs and asserted — its
  stdout compared line for line against CPython 3.12.
- Artifact guards: `TestTheArtifactTableCountsAreTheFilesOwn`,
  `TestTheRecordedSourceCountInTheAgentFacingDocsIsTheFilesOwn`,
  `TestTheRoadmapSnapshotIsMeasuredNotRemembered`, `TestTheRoadmapsCitationsResolve`.

## Measured, not remembered

| Claim | Number | Where it lives |
|-------|--------|----------------|
| Answer rows moved from refusal to the reference's answer | 29 unit + 13 CLI | `pkg/lang/dict_literal_key_test.go`, `integration/dict_literal_key_test.go` |
| Missing-key raises (class + sentence, exit 3, catchable) | 7 unit + 4 CLI + 2 + 2 catchable | same |
| One-word positions that stay refusals | 4 unit + 2 CLI (+ 2 plain-int rows that must still answer) | same |
| Record entries added | 39 (6130 → 6169) | `pkg/lang/testdata/interpreter-golden.json` |
| Conformance programs / matrix rows | 192 / 181 (143 parity-asserted, 128 `match`) | `integration/programs`, `integration/conformance-matrix.json` |
| Drift-ledger rows paid or added | 0 / 0 (332 + 21 unchanged) | `pkg/lang/testdata/interpreter-golden-drift.json`, `integration/testdata/interpreter-golden-drift.json` |
| Rows closed by this cycle | 1 (Gap R.215) + 1 paid inside it (Gap R.216) | `roadmap.md` |
| Rows opened by this cycle | 4 (Gap R.217, R.218, R.219, R.220) | `roadmap.md` |
| Runtime helpers added | 0 — `rt_dict_has_tagged`, `rt_dict_get_tagged`, `rt_dict_value_tag`, `rt_print_mixed_value`, `heapDictFrom`, `taggedOperand` all pre-date this commit | `pkg/lang/codegen.go`, `pkg/lang/heapargs.go` |
