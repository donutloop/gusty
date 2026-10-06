# A container has the methods its reference's containers have

## Decision

`extend`, `insert`, `index`, `remove`, `clear` on a list and `update`, `pop`, `setdefault`, `clear` on a
dict **exist**, on both the statement and the expression road, and answer what the reference answers
(roadmap `Gap R.188`, the concrete half of `Gap R.63` / L12.11; ADR 0301). Before this cycle every one
of them answered `no such list method` / `no such dict method` on **both** engines:

```
xs = [1]
xs.extend([2, 3])   # CPython [1, 2, 3]      · both engines: no such list method
xs.insert(0, 9)     # CPython [9, 1]         · both engines: no such list method
xs.index(2)         # CPython 2              · both engines: no such list method
xs.clear()          # CPython []             · both engines: no such list method
d.update({"b": 2})  # CPython {'a': 1, ...}  · both engines: no such dict method
d.pop("a")          # CPython 1              · both engines: no such dict method
d.setdefault("c",3) # CPython 3 (and writes) · both engines: no such dict method
d.clear()           # CPython {}             · both engines: no such dict method
```

A **missing** answer, not a wrong one — the ladder's second class, and the one a pin cannot catch: there
was no output to compare, and a refusal on a program the reference runs to completion is exactly what
`Gap R.63` was filed for.

Along the way `Gap R.189` was measured and taken as far as the backend allows: a `KeyError` carries the
**key's repr** (`KeyError: 'a'`, `KeyError: 9`), which the interpreted leg now prints and the compiled
leg cannot, because its raise is a compile-time constant string.

## The agentic rationale

A program that reads like Python has to run like Python, including the methods a reader does not think
twice about writing. `d.setdefault(k, []).append(v)` is not an exotic construct; it is how anybody groups
rows, and it did not exist. The failure mode was also the wrong shape for a machine: `no such list method
extend` is exit 1 with a sentence that tells the author their program has a bug, when the language is the
thing that lacks a feature — a refusal that blames the wrong party (ADR 0166's exit-class contract,
`Gap R.38`'s rule that a refusal describe the program the reader is holding).

## Codegen / IR implications

- **A dict is two parallel slices, so removal needs a door.** `elems` and `dvals` must stay the same
  length; a `pop` that shrinks one and not the other misaligns every later key against the wrong value,
  and the symptom shows up three lines and one container away. `dictPut` was already the single write
  door, guarded by a test that counts appends; the new `dictRemove` sits beside it under the same guard,
  so the invariant has one owner rather than two that can drift.
- **`index` answers by value equality, not identity** — `[1].index(True)` is `0` — because it must agree
  with `in`, with `count`, and with the dict-key rule ADR 0234 settled. Roads that ask "is this the same
  element" separately are where a bool stops being a number for some operations and not others.
- **`insert` clamps; it does not validate.** `xs.insert(9, 9)` on a one-element list appends, and
  `xs.insert(-9, 0)` prepends. An implementation that rejects an out-of-range index looks more careful and
  is wrong: the clamping is what makes building a list by repeated `insert(n, ...)` work.
- **The mutators answer the void, per ADR 0300.** `extend`, `insert`, `remove`, `clear`, `update` return
  `e.noneVal`, which is why no new mutation road reopened `Gap R.187`. `pop` and `setdefault` are
  expressions and answer with a value. The distinction is the same table, read the same way.
- **The raises are the reference's sentences**, because a caught exception is compared by programs and by
  the ledger (ADR 0215): `ValueError: 5 is not in list`, `ValueError: list.remove(x): x not in list`,
  `KeyError: 'z'`, `KeyError: popitem(): dictionary is empty`. Each was transcribed from a live CPython
  rather than composed.
- **`popitem` refuses, and says why.** It answers a **pair**, and this language has no tuple value until
  L11.3 (`Gap R.126` family). Answering a list would make `print` render `[k, v]` where the reference
  renders `(k, v)` — a wrong answer in a container's clothing — so the method exists and refuses naming
  the missing representation. A method that exists is not obliged to answer.
- **The compiled leg still refuses these over a *name*.** It folds a container method only over a literal
  written at the call; the slots a variable holds belong to the runtime. That is `Gap R.63`/L12.11's
  remaining half, recorded as an `OraclePin{aot, Missing}` rather than matched by making the interpreter
  refuse (ADR 0298's rule, restated a fourth time).

## Alternatives rejected

- **Answer `popitem` with a two-element list.** Rejected: `print` would show `[1, 2]` for what the
  reference shows as `(1, 2)`, and the row would then have to be reopened by someone who noticed. It
  refuses until L11.3 gives the pair a value.
- **Raise `IndexError` from `insert` for an out-of-range index.** Rejected: the reference clamps, and the
  clamping is load-bearing for a real idiom.
- **Make `index` answer `-1` when absent.** Rejected: that is `str.find`'s contract, not `list.index`'s,
  and a `-1` is a legal index — a program would silently read from the end of the list.
- **Give the compiled leg run-time container-method calls in this commit.** Rejected as bundling: it needs
  the tagged word (L11.1) to know what a slot holds, and the interpreter's correctness is not contingent on
  it. The refusal is honest and recorded.
- **Leave `KeyError: key not found`.** Rejected for the interpreter: a `KeyError`'s argument *is* the key,
  and `except KeyError as e: print(e)` is a real program. Kept for the compiled leg only because it is a
  module constant, with the row marked PARTIAL so the debt cannot be mistaken for closed.

## Related

`Gap R.188` (this row), `Gap R.63` / `L12.11` (receiver-keyed dispatch: the compiled half), `Gap R.189`
(a `KeyError` names the key — PARTIAL, owed to L11.1), ADR 0300 (in-place mutation answers the void, which
every mutator here obeys), ADR 0215 (a catchable raise keeps CPython's sentence), ADR 0234 (the dict-key
rule: why `index` compares by value), ADR 0291 (a void hands back `None`), ADR 0298 (refusal symmetry
declined), ADR 0166 (exit classes; a refusal that blames the program for the language's gap).
