# A key the language cannot hash names its type at every dict door

## Status

Accepted — 2026-07-14

## Context

ADR 0324 taught a container-valued *needle* to raise `TypeError: unhashable type: 'list'` when it was
tested against a container held in a slot, and ADR 0327 gave a dict literal the same tagged door a dict
variable uses. Between them, the rule looked finished. It was not: it reached the deep roads and missed
the three ordinary ones. Measured against CPython at the start of this cycle, all on the committed
binary:

| program | the reference | this backend |
|---|---|---|
| `print({"a": 1}[[1]])` | `TypeError: unhashable type: 'list'` | `KeyError: key not found`, exit 3 |
| `d = {"a": 1}` / `print(d[[1]])` | `TypeError: unhashable type: 'list'` | `KeyError: key not found`, exit 3 |
| `x = [1]` / `d = {"a": 1}` / `print(d[x])` | `TypeError: unhashable type: 'list'` | `KeyError: key not found`, exit 3 |
| `print([1] in {"a": 1})` | `TypeError: unhashable type: 'list'` | **`False`, exit 0** |
| `print([1] in {1, 2})` | `TypeError: unhashable type: 'list'` | **`False`, exit 0** |
| `d = {}` / `d[[1]] = 2` / `print(d)` | `TypeError: unhashable type: 'list'` | **`{[1]: 2}`, exit 0** |

The mechanism was the one ADR 0189 wrote down and ADR 0232 made a soundness rule: a dict slot is a
(payload, tag) pair, and a lookup that compares payloads alone cannot tell "this key is absent" from
"this key could never be asked". `rt_dict_find` scans, finds no match, and `checkKeyRead` reports the
generic `KeyError: key not found` — `keyNotFoundMessage` has no key to name, which is where that sentence
comes from. The membership door had the same fault and no checker at all, so it *answered*: `[1] in
{"a": 1}` came back `False` and the program carried on. The store door had the fault worst of all — it
put the handle in the slot and printed `{[1]: 2}` as if a list had been a key all along.

Two of those three are wrong answers at exit 0, which this repo treats as worse than a refusal.

## Decision

**Ask the program the hashing question at every door that consults a dict, before the door consults the
dict.** `pkg/lang/unhashable_key.go` owns it:

- `unhashableKeyName(e)` answers the type name the reference would use, read back from the source: a list,
  dict or set literal; a tuple, whose elements are walked left to right so that `(1, [2])` names `'list'`
  the way the reference's hash does; and a `Name` the program most recently filled with a container, read
  from the same maps (`listVars`, `runtimeDicts`, `runtimeSets`) the print and membership doors already
  trust. Where the kind is only a run-time fact the answer is *no* — the door keeps the behaviour it
  always had rather than raise a TypeError the program may not deserve.
- `unhashableKeyGate(b, key, span)` raises CPython's own sentence — `unhashable type: 'list'`, class
  `TypeError` — through `raiseTo`, the same non-local road every other exception takes, so
  `except TypeError:` catches it and an uncaught one exits 3 with the reference's two lines. It ends the
  block it branched out of and reopens the continuation, so a door that refuses stays a valid module.
- The doors: `checkKeyRead` and `checkKeyReadTagged` (both subscript roads, literal and variable) and
  `assignIndex` (the store), plus the membership test in `value()` — asked before the tagged-membership
  dispatch and before any fold, so no arm of the test gets to answer the question first.
- `haystackHashesKeys(e)` is the gate's boundary, and the part this ADR exists to record: **a list and a
  tuple answer membership by walking**. `[1] in [[1], [2]]` is `True` in the reference. The first version
  of the gate fired on the needle alone and broke exactly that — a soundness bug in the fix, caught by
  asking the reference rather than by reading the code. The gate now fires only where the language
  actually asks for a hash, and a haystack whose kind the compiler cannot name is left alone.

## Alternatives rejected

- **A runtime tag check inside `rt_dict_find`.** The honest general answer, and rejected for this cycle
  for the reason ADR 0327 records: a dict literal's slots are constants emitted at the use, and there is
  no tag register beside them to read; doing it properly is L11.1's tagged value word, which that row (and
  ADR 0232) owns. The front-end question covers what the source states, which is where every measured
  wrong answer came from; the run-time-only case is left honest rather than guessed, and named.
- **Raising `KeyError` with a better message.** Cheaper and keeps one class, and wrong: the reference
  distinguishes "no such entry" from "you cannot use that as a key", and `except KeyError:` must not catch
  what `except TypeError:` is for. The class is the API.
- **Refusing to compile instead of raising.** `[1] in {"a": 1}` is a *program*, and CPython runs it — up to
  the raise. A refusal would also be unconditional, killing a program whose bad line is behind a `False`
  branch. A trap is the dynamic answer to a dynamic construct.
- **Letting the store road keep storing.** It prints `{[1]: 2}`, which is not a dict the reference can
  produce; the entry must not exist.

## Consequences

- `pkg/lang/unhashable_key.go` + `pkg/lang/unhashable_key_test.go` (thirteen traps asserted as the
  reference states them; the soundness table for list haystacks; the classifier and the haystack question
  asked directly, away from codegen; every trap asserted to print nothing on stdout; the record leg
  reading `exnType`/`exnMsg` beside the compiled raise) and `integration/unhashable_key_test.go` (the same
  doors through the CLI with exit code and empty stdout asserted, and
  `integration/programs/unhashable_key.gy` compared line-for-line with CPython, added to the conformance
  matrix as an asserted match row).
- The record already agreed: the thirty newly-recorded sources came back
  `exnType: "TypeError"`, `exnMsg: "unhashable type: 'list'"`. The retired engine had the rule; the
  compiled backend did not. That is the record leg doing the job it is for.
- Two neighbours measured on the way are filed rather than bundled: **Gap R.221** (a dict *variable* keyed
  by `None` or a bool reads the wrong entry — `d = {None: 3.5}` prints `3`, `d = {False: [2]}` prints a slot
  number — the unfinished half of ADR 0327's pair door) and **Gap R.222** (a list or string subscript asked
  with a key of the wrong kind answers a number instead of raising `list indices must be integers or
  slices, not str`). Both are wrong answers at exit 0, both measured on the committed binary.
- A tuple *key* remains honest refusal where the key's kind cannot travel with it (`Gap R.212`): refusal,
  not a wrong answer, and the row stays open.
