# ADR 0470 — the length of a slice of a text is asked of the slice road, and counted in code points

**Status:** Accepted
**Date:** 2026-10-31
**Decides:** roadmap **Gap R.240** (the length of a slice of a text refuses: `s = "abcd"` / `print(len(s[1:3]))` stopped the build with `codegen: len requires an inline list/dict/set literal` at **exit 1** where the reference answers `2`, and `print(len(s[::-1]))`, `print(len("abcd"[1:3]))` refused alike)
**Files:** **Gap R.567** (`len` cannot reach into the list a CALL answered — `len(s.split(" ")[1])`, `len(sorted(["b", "a"])[0])` — and its refusal blames a literal the program never wrote)
**Keeps open:** **Gap R.243** (a step that is a VALUE: `len(s[::k])` keeps the slice road's own `cannot fold string slice`), **Gap R.402** (a module text a function BODY reads carries no kind, so `len` and `print` refuse it alike), **Gap R.33** (the length road still declines shapes of its own), **L11.1** (the tagged word that would make a slot measurable without a notebook)
**Related:** ADR 0336 (the slice-bounds kind gate and the fold that owns the characters — both walked here, not copied), ADR 0337 (the step-zero raise, inherited by this door), ADR 0229 (the value-bounds road `rt_str_slice` and its sentinels), ADR 0225 (a text's length is its CODE POINTS: `rt_str_nchars`, never `rt_str_len`), ADR 0466 / ADR 0468 (the one base gate whose four sentences this door now inherits for free), ADR 0232 (one question, asked once, in one place), ADR 0273 (a gate may not cost a program that compiles), ADR 0249 (a refusal names the road that declined), ADR 0166 / ADR 0211 (exit 2 is the compiler's bug, a trap is exit 3, a refusal is exit 1), ADR 0337 (a counter-row beside every absence-assertion), ADR 0302 (the two witness legs), ADR 0365 / Gap R.314 (the `(payload, tag)` pair a building road owes its caller — what **Gap R.567** is waiting for)

## The rule

**`len` of a slice of a text asks the slice road which text the bounds select, and measures that text with the
helper a text variable's length already asks.** Code points, never bytes. The bounds and the step are asked
*before* the count, because they belong to the slice road and not to `len`; the raises they produce are this door's
answers too, and a `len` that counted first would print a number for a program the reference aborts.

```
s = "abcd"
print(len(s[1:3]))        # 2       print(len(s[10:20]))   # 0
print(len(s[::-1]))       # 4       print(len(s[3:1]))     # 0
print(len("abcd"[1:3:2])) # 1       print(len(s[i:j]))     # 2   (i = 1, j = 3 — the rt_str_slice road)
print(len("café"[2:]))    # 2       — 2 characters, 3 bytes
```

## Context — three doors answered one fact, the fourth refused to go and ask it

| program | reference | this backend before ADR 0470 |
|---|---|---|
| `print(len(s))` over `s = "abcd"` | `4` | `4` — the fold reads the bound text and counts its runes |
| `print(s[1:3])` | `bc` | `bc` — the fold computes the sliced characters, so it *holds* the count |
| `print(len(xs[0:2]))` of a list copy | `2` | `2` — the `*Slice` arm ADR 0335 gave the container-length door |
| `print(len(s[1:3]))` | `2` | **exit 1** — `len requires an inline list/dict/set literal` |
| `print(len(s[::-1]))` | `4` | **exit 1** — same sentence |
| `print(len("abcd"[1:3]))` | `2` | **exit 1** — same sentence |

The refusal is the second half of the defect. It names an inline container literal in a program whose only literal
is a text, so a reader — human or agent — goes looking for a list that is not there. That is **ADR 0249**'s rule,
and it is why this row's fix had to end with every refusal on the family naming the road that declined.

## The decision, and the tempting alternative

`builtinLen` had arms for a `*Name`, the four literals, a comprehension, a call and a slot read; a `*Slice` fell
through all of them to the container refusal. The tempting fix was a new arm in `builtinLen` that re-derives the
answer from the node it is handed: fold the text itself (the runes are one `stringVal` away), and for value bounds
call `rt_str_slice` and count. Roughly thirty lines, no change to the slice road.

It was rejected, and the reason is a word: **second**. A copy of `valueSlice`'s text branch would be a second place
in this toolchain that decides what a slice of a text is — which bound clamps, which step raises, which stride the
fold can honour, which sentinel an absent bound is spelled with. **ADR 0232** is the house rule for exactly that
shape, and the failure mode is not hypothetical: the branch has three doors stacked in a specific order (base →
bounds → step → fold/helper), and the order is what keeps `len(s["a":"b"])` raising instead of printing `2`. A copied
branch keeps the order only as long as nobody edits the original.

So the branch was **moved instead**:

* `sliceRoadCallsItAText(n *Slice) bool` — the base question `valueSlice` asks, exported, so the length door asks
  the same predicate rather than reading the base's spelling;
* `valueTextSlice(b, n) (reg string, raised bool, err error)` — the branch itself, statement for statement, returning
  `raised` so a caller knows a raise now sits where the read was and must not be followed by an instruction in the
  same block;
* `lenOfTextSlice(b, arg)` — `len`'s arm: base gate → the predicate → the branch → `rt_str_nchars`.

`valueSlice` now calls its own extracted branch, so there is one text-slice road and two callers of it. `raised` is
not decoration: an earlier draft let `len` emit its measurement after the raise's terminator, which `llc` rejects —
the verifier catching at exit 2 what a row should have caught at review.

**Why the count is asked of the object instead of folded.** The folded shape's answer is a constant this compiler
could compute, and the first draft did exactly that. It was dropped for the same reason the branch was extracted:
`len(s)` on a bound text, `len(s[1:3])` on folded bounds and `len(s[i:j])` on value bounds would then be measured by
two rules — one rune-count in Go, one `rt_str_nchars` in the module — and the row's own parity table (one program
written with constant bounds, with value bounds, and with the slice bound to a name first) is precisely where two
rules drift. Asking the object means the unit is decided once, in the helper **ADR 0225** fixed: a `é` is one
character whichever road produced the text. The cost is one call in a module that would have folded it away, and
`opt` is welcome to it.

**Why the raises belong to this row.** `len` did not acquire a bounds complaint, a step complaint and a base
complaint — it inherited four, for free, from gates it never calls directly:

| program | the reference | this door now |
|---|---|---|
| `print(len(s["a":"b"]))` | `TypeError: slice indices must be integers or None or have an __index__ method` | exit 3, catchable — **ADR 0336**'s gate |
| `print(len(s[1.5:2]))`, `print(len(s[[1]:2]))` | the same | exit 3, catchable |
| `print(len(s[::0]))`, `print(len(s[1:3:0]))` | `ValueError: slice step cannot be zero` | exit 3, catchable — **ADR 0337**'s raise |
| `print(len({1, 2}[0:1]))` | `TypeError: 'set' object is not subscriptable` | exit 3 — **ADR 0466**'s base gate |
| `print(len({"a": 1}[0:1]))` | `KeyError: slice(0, 1, None)` | exit 3 — the dict hashes the slice |
| `print(len(1[0:2]))`, `len(None[0:1])`, `len(True[0:1])` | `'int'`/`'NoneType'`/`'bool' object is not subscriptable` | exit 3 — **ADR 0468**'s arm |

A door that measured first answered five of those with a number at **exit 0**. That is the trade **ADR 0166** and
**ADR 0211** forbid — a wrong value at the code that means the program ran is worse than the refusal this row
replaced — and it is the reason the raise rows are half the witness set rather than a footnote.

## Codegen and IR implications

* The answered module calls `@rt_str_nchars` in the program's own functions, and NOT `@rt_str_len`: the byte helper
  is the wrong unit and its presence is a failing row. `def f(): return "abc"` / `if f():` is the counter-row that
  keeps that prohibition honest, because that program's @main really does call the byte helper (**ADR 0337**'s
  negative-control rule).
* The raising shapes emit NO measurement call at all — asserted, with the answered program as the counter-row, so
  the absence is a fact about the arm rather than about a helper nobody emits.
* Nothing else about the module moves: no new global, no new helper, no new `@str_tab` entry beyond the one the
  print road already interned for `print(s[1:3])`, and no change to the list-copy road (`len(xs[0:2])` still asks
  `@rt_slice` and the count field), which the untouched tables pin on both legs.
* Machine path: no new diagnostic code, no schema change. The refusal the arm declines INTO is unchanged
  (`lower.unsupported.len-needs-inline-container` in `pkg/lang/capability.go`), and the shapes newly answered were
  never in the capability table because they were never advertised as supported — the registry lists refusals, and
  this commit removes one rather than adding one.

## Alternatives rejected

* **A second counting rule in `builtinLen`** (fold the runes in Go, ask the table for value bounds): rejected above —
  two owners for one question, diverging at exactly the parity rows.
* **A `len`-specific gate order** (count, then let the slice road complain if it must): rejected; it turns four
  exceptions into four wrong numbers at exit 0.
* **Fixing the message instead of the door** (make the fall-through refusal name a text slice): rejected as a
  dressing-over — the answer existed twice in the compiler already, so a better-flavoured refusal would have
  preserved a wrong answer at the front door and paid for prose.
* **Answering the value-step shape here** (`len(s[::k])`): rejected as scope-creep into **Gap R.243**'s stride road;
  what this commit does for that shape is improve its sentence — the reader now gets the slice road's own `cannot
  fold string slice` instead of being sent to find a container literal — and the extraction means `len` answers it
  for free, with no change to this file, the day the stride lands.
* **Reusing `heapContainerLenFn`'s `*Slice` arm** (the one ADR 0335 wrote for list copies): rejected because that
  arm's answer is a container count read from a heap header, and a text is not a heap container — its word is an
  interned index, which is exactly the confusion **ADR 0229** exists to prevent.

## Witnesses

`pkg/lang/text_slice_len_test.go`: 31 answered rows run on the compiled leg, the JIT and the record; 11 raise rows,
each asked of `jitTrap` AND of the record, class AND message compared; 5 handler rows checked byte for byte against
the record (including that the caught message carries no `Traceback` caption — **ADR 0406**); 4 parity rows; 20
untouched neighbours (a list copy's length, the containers' count fields, a text variable, a text method's answer,
the print road's own answer); the door asked of the AST node the parser built, each row carrying the notebooks the
binding road would have written for it, with the nil-generator and no-records soundness rows; and the filed table.
`integration/len_text_slice_test.go`: 37 CLI answers with CPython run inside the same case, 12 CLI raises with the
reference's last traceback line read from a live `python3` rather than quoted, 5 handler rows, the IR rows at CLI
altitude with three counter-rows, the corpus program's line-for-line witness and its record witness, and 5 filed
refusals — none of which may reach **exit 2**, all of which must name the road that declined.
`integration/programs/len_text_slice.gy` joined the corpus as a `match` row and MEASURES the length, replacing the
sentence this file used to quote as a limitation of the corpus (`programs/slice_bounds.gy` asserting
`s[1:3] == "bc"` because that row could not measure it).

**69 record entries** came in from the reference with the compiled leg cross-checked (**10862 → 10931**), none
self-certifying: `tools/recmerge` refuses an entry the compiled leg will not agree to, so a shape that answers here
and not there cannot be recorded at all (**ADR 0302**).

The two rows that pinned this shape in the filed tables — `TestWhatTheBoundDoorStillCannotReachIsFiledNotFixed` in
`pkg/lang/slice_bounds_kind_test.go` and its CLI twin in `integration/slice_bounds_test.go` — moved out by deletion,
and the emptied slots say where they went: a filed row is closed by that failure, not by editing a sentence.
