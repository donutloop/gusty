# `str()` and `repr()` answer for a value whose kind the run time decides

## Decision

The compiled backend renders a value through **one** door — the tag-reading printer
(`rt_print_mixed_value`) pointed either at stdout or at a capture buffer — and every place that needs a
value's text asks that door. Concretely, these now answer instead of refusing (roadmap `L11.1` / `L11.2`,
closing the `str`/`repr` half of `Gap R.171` and taking `Gap R.146` and `Gap R.189` further):

```python
xs = []; xs.append(3); xs.append("a"); xs.append([1, 2]); xs.append(None)
str(xs[0])            # 3        (was refused)
str(xs[1])            # a        (was refused)
repr(xs[1])           # 'a'      (was refused)
str(xs[2])            # [1, 2]   (was refused)
n = xs[1]; str(n)     # a        (was refused; `n + 1` blamed a loop the program never wrote)
xs = []; xs.append(1); xs.append("b")
for v in xs: str(v)   # 1, b     (was refused)
```

and the prompt reports a slot's kind from the tag rather than from a guess:

```console
$ gusty --json --eval 'xs = [True, 1]\nxs[0]'
{"output": "", "backend": "aot", "exit": 0, "result": "True", "type": "bool"}
$ gusty --json --eval 'xs = [True, 1]\nxs[1]'
{"output": "", "backend": "aot", "exit": 0, "result": "1", "type": "int"}
```

## Why this is the shape

The rendering pair (`str`/`repr`) and the tagged value word (`L11.1`) are the same feature seen from two
ends: both ask *what is this value*, and the answer has always lived in the tag, next to the payload, in
the object. `print(xs[i])` had been asking it for a dozen ADRs. `str(xs[i])` went to a different road — the
compile-time str/repr table, which knows an integer, a text, `None`, a verdict, and the container literals —
and refused everything the table has no row for. That set is exactly "the values whose kind is a run-time
fact", which is why the two refusals (`str on non-integer`, `repr of an element read is not implemented`) had
survived so long: they described a limitation of one road while the other road was already correct.

The rule this ADR sets is therefore not "add str/repr support"; it is **one door**:

| Question | Door |
|---|---|
| print a value | `rt_print_mixed_value(payload, tag, quote)` → stdout |
| `str(v)` / `repr(v)` | the same printer → a capture buffer, interned as a string (`rt_str_of_value`) |
| announce a snippet's value (the prompt) | the same printer → the capture buffer, plus the kind named from the tag (`rt_echo_pair`) |
| a container's own slots | the object answers (`rt_str_of_container` → `rt_print_container_value`), because only it knows how its slots are stored |

`quote` is the str/repr half of the pair: a text writes its characters under `str` and its quoted repr under
`repr`, and only the caller knows which context it is in (ADR 0185's flag, now carrying the pair). A second
renderer is what ADR 0258 exists to keep dead — two number formatters once disagreed about a container and
`str([1, 2])` answered `0`.

## What the binding road had to learn along the way

`n = xs[0]` over a container the program **built** bound a bare payload. A payload without its tag is a
number wearing another object's bits — the interned index of `"a"` is a small integer — so `str(n)` refused
and `n + 1` refused *with a sentence that blamed a loop the program never wrote*:

> `n comes from a loop over a mixed list`

That is `Gap R.38`'s defect (a diagnostic that describes a program the reader cannot find) produced by a
fallback clause rather than by a pasted template, and it is fixed in the same place: every road that binds a
pair now records where the pair came from (`taggedOrigin`), and the refusal asks that record — `holds a slot
the program built at run time, read by position` for a binding, `is the element a loop stepped over a
container that mixes kinds` only when a loop really did it. `TestANumberPositionStillRefusesAndNamesTheTruth`
fails if the sentence mentions a loop for a program with none.

The binding itself now writes both words (`bindSlotReadPair`), which ADR 0185 always required and this one
road had never been taken; `TestThePairBindingWritesBothWords` asserts the tag alloca is written, because the
regression there is silent (the value prints as an unrelated small integer).

## `KeyError` names its key on two more doors

Threading the key's own expression into `checkKeyReadTagged` (`mixedDictPair`, `containerSlotRead`,
`slotReadUnderTag`) moved `Gap R.189` again:

```python
d = {};            print(d["z"])   # KeyError: 'z'   (was: KeyError: key not found)
d = {}; d[1] = 2;  print(d["k"])   # KeyError: 'k'   (was: KeyError: key not found)
```

The generic sentence remains only where the key's kind is itself a run-time question with no expression to
quote. The three paid rows went out of the drift ledger the way every paid row must — the ratchet failed the
run until they were deleted.

## What is still refused, and what it costs

`n + 1`, `abs(n)`, `[n]`, `min(n, 3)` — the positions that keep **one** word for a whole value — still
refuse, now with a sentence that names the origin and the missing word (roadmap `Gap R.146`, under `L11.1`).
The echo still declines a user-defined call, because rendering means lowering twice and a call whose body
prints prints twice (`L13.1`).

The tag-naming decision has a cost worth recording: the prompt now reads the kind out of the *same* table the
operand-type messages read (`rt_kind_name`, in the tagged-arithmetic block), so the echo block drags that
block into any module that echoes a pair. That is a few hundred bytes of IR in exchange for the prompt and a
`TypeError` never calling a value by two names — the ADR 0259 argument, in the other direction.

## Alternatives rejected

- **Widen the compile-time str/repr table** to recognize more expression shapes. It is how the bug got made:
  the table is a static guess about a value whose kind the run time owns, and every extension is another way
  to disagree with `print`.
- **Answer `str(v)` as the plain payload when the kind is unknown.** `str(x)` answering `0` for `None` is the
  silent-truncation class ADR 0258 files; a refusal is a worse user experience and a better compiler.
- **Have the echo keep saying `object`.** Before this cycle `--json` reported `"type": "object"` for slot
  reads — a prompt reporting the compiler's blind spot as though it were the value's type. ADR 0259 already
  decided what a slot *is*; the prompt was the last place that had not heard.
- **Give the echo block its own kind→name table.** One tag vocabulary (value.go) is what keeps the printer,
  the comparison, the raise message and now the prompt describing values with the same words.
- **Leave `n = xs[0]` binding one word and let the refusal explain it.** A payload-only binding is not a
  refused program, it is a wrong-typed one: the name reads as an integer in every position that accepts the
  i32 without asking.

## References

`roadmap.md` L11.1 (the tagged value word — this cycle's `str`/`repr` door), `Gap R.171` (the renderer),
`Gap R.146` (the one-word positions), `Gap R.189` (`KeyError` names its key), `Gap R.38` (a refusal must not
describe a program the reader cannot find), `L13.1` (the prompt's silent call) · ADR 0185 (a pair is read or
written as a pair), ADR 0258 (one renderer), ADR 0259 (what a slot is), ADR 0166 (a refusal names the half it
lacks), ADR 0302 (one backend; the record and the ledgers) ·
`pkg/lang/str_pair_test.go`, `integration/str_pair_test.go`, `pkg/lang/render.go`, `pkg/lang/echo.go`,
`pkg/lang/heapargs.go`
