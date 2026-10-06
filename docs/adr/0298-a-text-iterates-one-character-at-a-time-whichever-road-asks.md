# A text iterates one character at a time, whichever road asks

## Decision

A text is an **iterable**, and every road that iterates one asks the same question the `for` statement
already asked (roadmap `Gap R.185`, owner L11.1 / L11.5; ADR 0187's one-representation rule applied to
iteration):

| program | reference | `--interp` before | `--aot` |
|---|---|---|---|
| `[c for c in "abc"]` | `['a', 'b', 'c']` | `[]` (exit 0) | refuses (exit 1) |
| `[c for c in "abc" if c != "b"]` | `['a', 'c']` | `[]` (exit 0) | refuses |
| `max("abc")` / `min("abc")` | `c` / `a` | `abc` (exit 0) | refuses |
| `for c in "abc": print(c)` | `a`,`b`,`c` | ✅ already correct | refuses |

Both fixed roads now build the element list by ranging over the object's `sval` **per rune**, exactly as the
statement road does, so `max("aé")` is `é` and not the last byte.

## The agentic rationale

The interpreter answered `[]` at **exit 0** for a comprehension over a text. Not a refusal, not a trap — an
empty list, the shape a program happily iterates over and never enters. An agent that wrote
`[c for c in s]` and got `[]` had no signal at all: exit code clean, output well-formed, program wrong.
`max("abc")` → `abc` was the same class with a plausible face.

The compiled leg's refusal is not a defect being celebrated: it is exit 1 with a sentence naming what it
cannot lower, which is the contract's honest failure. This cycle fixed the half where a wrong number was
being emitted, and recorded the compiled half as owed (`L11.1`) rather than making the interpreter match the
refusal.

## Codegen / IR implications

- **The interpreter's `obj` has two element stores** — `elems` for containers, `sval` for texts — and the
  two roads that iterate took `elems` unconditionally, then checked `ok` on the heap map. A text *is* in the
  heap, so the `ok` succeeded, `elems` was empty, and the road concluded "zero items". Nothing failed; that
  is why it survived: an empty result is indistinguishable from an empty iterable.
- **`min`/`max` had a dict guard and a list/set arm, and everything else fell to "a bare scalar is a
  one-element collection."** A text landed there and the whole string came back as the candidate list of
  one. The `else` of a kind dispatch is where wrong answers live; this one now has a text arm before it.
- **Three roads, one question** (`for`, comprehension, iterable builtin). The rule from ADR 0279/0280 about
  *kinds* applies verbatim to iteration: if the roads ask different questions, the program gets two answers
  to one question, and the tests above fail the disagreement rather than each road separately.
- **Code points, not bytes.** Iterating per `rune` matches the reference for `"aé"`. The measurement side
  (`len`, `s[i]`) is still bytewise and is `Gap N.2`'s / L11.5's row; this row does not claim it.

## Alternatives rejected

- **Make `--interp` refuse too, matching `--aot`.** Rejected: that would "fix" a wrong number by deleting a
  correct answer, and the ladder forbids it — the reference and this interpreter now agree, and taking
  `--interp`'s answer away to achieve agreement is the worst available outcome. The compiled refusal stays
  because the compiled road genuinely cannot lower it.
- **Iterate a text by byte** (the cheapest change, since `sval` is a Go string). Rejected: `max("aé")` would
  answer a fragment of a code point. Rune-for-rune costs one `range` and matches the `for` road beside it.
- **Teach `elems` to hold characters at text-interning time** so every consumer sees elements. Rejected:
  `elems` is the container store that the container roads mutate (`append`, `pop`, `dictPut`), and giving a
  text a mutable element list creates a second source of truth next to `sval`. The text's characters *are*
  `sval`; each road derives them.
- **Fold the comprehension case at parse time for a literal text.** Rejected: same four sweep rows, and
  `for c in s` over a name would still be the only road that worked.

## Related

`Gap R.185` (this row), `Gap N.2` / L11.5 (code-point strings: the measurement half), `Gap R.76`/ADR (a
comprehension over a set or dict answering machine words), ADR 0279/0280 (one question per expression),
ADR 0297 (the previous cycle's `not`-vs-`if` disagreement — same shape, different question), ADR 0166
(exit codes), ADR 0298 (`Gap R.183`/`R.184`'s iteration sibling).
