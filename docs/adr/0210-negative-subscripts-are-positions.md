# ADR 0210 — a subscript is a position or a key, and only positions count from the end

Status: accepted (roadmap L11.4; closes the two `probe_negative_*` debts)

## Context

`xs[-1]` trapped with *index out of range* on both backends where CPython answers the last
element, `"abc"[-1]` failed compilation, and `print([1, 2, 3][-1])` ended the compiler's life
rather than the program's:

```
panic: runtime error: index out of range [-1]
```

The last one is the interesting symptom. `irGen.value` had a `case *ListLit` that read
`obj.Elems[key]` with no bounds test at all — the constant folder assumed any key that reached
it had been checked somewhere else, and nothing had. Under the exit-code contract (ADR 0168) a
Go panic is a compiler bug, never a source error, so this shape was already owed; the two
wrong-answer shapes were owed by the oracle ledger (ADR 0186), which had them pinned as
`probe_negative_index` and `probe_negative_literal` and had been politely reporting them as
`debt` for dozens of cycles.

The fix looks like one line — `if i < 0 { i += len }` — and the reason it is an ADR is that the
line is wrong if you write it in the obvious place.

## Decision

**One normalisation, applied to positions, never to keys.**

- `normPosIndex(idx, length)` (interpreter) and `(*irGen).normalizeIndex` (codegen) implement
  `i < 0 ⇒ i + len` followed by the bounds test. Read and write go through them; `pop`,
  slicing and `index` already had the same rule locally and keep their local shape.
- In the compiled backend the normalisation is emitted, not folded: `rt_list_len` is called,
  the wrap is a `select`, and the bounds check runs on the *normalised* index, so `xs[-1] = v`
  works for a list whose length the compiler never sees and an out-of-range index still takes
  the `raiseTo(IndexError)` path.
- **Dicts and sets are exempt.** Their subscript is a key: `d = {-1: "minus"}` must answer
  `"minus"`, not the last entry. A helper that "normalises all subscripts" would have broken
  this, and it is pinned on three engines to keep it from being un-broken by a future
  simplification.
- The constant paths (list literal, string literal, comprehension list, list-producing call,
  imported module list) normalise in Go and bounds-check before indexing, which is what removes
  the panic.

## The rule that came out of it

Two things in this cycle generalise:

1. **A refusal message must be tested against the shell it promises.** While writing the
   subscript tests I exercised the advice in last cycle's run-time-string refusal — *"index a
   string with a constant (s[0])"* — and found it was false: `s = "abc"; print(s[0])` is
   refused by the AOT backend, because a string *variable* has no run-time string value yet
   (L11.5). Only a string *literal* may be subscripted with a constant. The message now says
   so. A refusal whose workaround does not work is worse than no workaround, because the reader
   spends their time on our mistake and concludes the whole message is unreliable. The rule to
   apply again: type the workaround into a scratch file before shipping the sentence.
2. **"Both backends agree" was never the contract.** Both backends agreed on `xs[-1]` erroring,
   for weeks, while CPython disagreed with both of them. The parity checks that were green here
   were comparing two implementations of the same misunderstanding. This is why the corpus runs
   three legs and why the two programs moved from `debt` rows to ordinary ledger-free parity
   cases: the oracle is the one that gets a vote.

## Codegen / IR implications

- The read path emits `call i32 @rt_list_len`, `icmp slt`, `add`, `select`, then the existing
  `icmp sge`/`or`/`raiseTo` sequence and passes the normalised register to `rt_get_elem`.
- The write path replaced a hand-inlined bounds check with the same helper, so read and write
  cannot drift apart on which index is checked.
- `checkIndexRead` remains for callers that only check; new normalising callers use
  `normalizeIndex`, which also sets `g.heapUsed` (ADR 0209's rule that a helper is emitted
  because it is referenced, plus the flag that lets the fast path stay fast).

## Alternatives rejected

- **Normalise in the analyzer, once, on the AST.** Rejected: the length lives in a heap object
  at run time, so a constant-folded normalisation is only correct for literals — and literals
  were already the easy half.
- **Normalise every subscript, including dict keys.** Rejected because `d[-1]` is a key: the
  exemption is not a special case, it is the sign that the two operations were never the same
  operation. (`{}` already taught this lesson — see the Containers section of
  `docs/language.md`.)
- **Fix the panic with a bounds check alone.** Rejected: it converts a crash into a wrong
  answer (`"index out of range"` for a shape Python answers), and the ledger would have called
  that progress.
- **Leave `docs/language.md`'s "negative indices ✅ DONE" heading.** Rejected: the docs already
  carried a paragraph disclaiming it ("do not read it as a claim about indexing"), which is the
  written form of a spec that describes two things in one heading. The heading now says what is
  true of both.
