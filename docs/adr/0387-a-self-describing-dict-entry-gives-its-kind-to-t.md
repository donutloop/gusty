# A self-describing dict entry gives its kind to the signless call and the folds

**Roadmap:** Gap R.287 (closed here — `abs(d[None])`, `min(d[None], 2)`, `max(d[None], 2)` and `sum([d[None], 0])` over a float entry) ·
L11.1 (the tagged value word in the positions that read it) ·
ADR 0309 (`abs` over the pair road) ·
ADR 0316 (`@rt_pair_fold` and `@rt_num_arith` for `min`/`max`/`sum`) ·
ADR 0353 (the dict read by a `None`/bool key carries its tag to the print door) ·
ADR 0356 (the f-string field asks the same subscript pair) ·
ADR 0273 (a gate decides what the program states; one-word positions do not invent a missing half)

## Status

Accepted. Landed 2026-10-20.

## Context

`d = {None: 3.5}` / `print(d[None])` has printed `3.5` since ADR 0353. The same value through a signless call
or a fold did not:

```python
print(abs(d[None]))        # CPython 3.5 — backend: 3 at exit 0
print(max(d[None], 2))     # CPython 3.5 — backend: 3 at exit 0
print(sum([d[None], 0]))   # CPython 3.5 — backend: 3 at exit 0
```

The dict road was not missing a pair. `rt_dict_get_tagged` and `rt_dict_value_tag` were already in the module.
The two callers were missing a gate. `abs` used `arithWouldRefuse`, whose `Index` arm asks whether the object
behind the container is *runtime-provided*; a dict that began as a literal answers no even when its value is a
float. The fold used the same question for every candidate. So both roads received the float box's handle and
treated it as the value itself.

This was not the key's defect. An int key, a text key and a bool key produced the same truncated answer. The
key road had already been paid; the callers had not followed.

## Decision

**The signless call and the fold ask the entry's tag whenever its kind cannot travel in the payload alone.
The ordinary numeric road keeps a statically provable integer entry.**

`entryReadNeedsTheTag` is the one question behind both widened gates:

1. If the subscript's static walk names the selected entry and that entry is an integer, return false. Its
   payload already is the value; sending `abs(d[1])` through the object would add work and make the static
   negative control false.
2. If the static entry is float, bool, void, text, list, dict or set, return true. Those payloads are box
   handles, interned indices, verdicts or container handles.
3. If the key or entry cannot be statically resolved, return true and ask the object. The absence of knowledge
   is not evidence that the payload is a number.

`absOperandMayNeedPair` and `foldOperandMayNeedPair` add that question to the existing gate. They do not alter
`arithWouldRefuse`. A fold answer remains outside the ordinary arithmetic road, so `max(d[None], 2) + 1`, a
call's one-word parameter, and a call's one-word return cannot silently read the winner's payload. Those remain
Gap R.146/R.220 territory and are pinned with today's answer, not promoted by this row.

## What answers, and what still refuses

| Program | CPython | Backend after ADR 0387 |
|---|---:|---|
| `abs(d[None])`, `d = {None: -3.5}` | `3.5` | `3.5` |
| `abs(d[1])`, `d = {1: -3.5}` | `3.5` | `3.5` |
| `abs(d[True])`, `d = {True: -1.25}` | `1.25` | `1.25` |
| `min(d[None], 2)` over `-3.5` | `-3.5` | `-3.5` |
| `max(d[None], 2)` over `3.5` | `3.5` | `3.5` |
| `max(d[None], 1)` over `2.0` | `2.0` | `2.0` |
| `max(d[None], 1)` over `True` | `True` | `True` |
| `sum([d[None], 0])` over `-3.5` | `-3.5` | `-3.5` |
| nested `abs(xs[0][None])` | `2.5` | `2.5` |
| `abs(d[None])` over text | `TypeError: bad operand type for abs(): 'str'` | same raise, exit 3 |
| `min(d[None], 1)` over list | `TypeError: '<' not supported between instances of 'int' and 'list'` | same raise, exit 3 |
| `f(max(d[None], 2))` into a one-word call | `3.5` | still today's `3` — Gap R.146/R.220's boundary, not this row's door |

## Machine path

No new capability is needed. Every answered shape is CLI output; every non-number emits an existing CPython
class and message from the runtime door. `gustyc --file integration/programs/dict_none_bool_key.gy` now carries
six signless/fold lines whose expected bytes are CPython's. Thirty-nine sources involved in the row, including
former wrong answers and traps, were passed to `tools/recmerge`; the recorder wrote only those for which CPython
and the compiled leg agreed.

The emitted program is checked rather than trusted: the abs rows must call `@rt_dict_get_tagged`,
`@rt_dict_value_tag` and `@rt_num_arith`; fold rows must call `@rt_pair_fold`; and the program body must not
carry `sub i32 0` for a float entry. A statically integer dict entry must not reach either self-describing door.

## Consequences

- The pair road is defined by the **reader's question**, not by which road first built the value. Print, str,
  repr, an f-string field, and now the signless/fold calls ask the same subscript pair.
- The static fold continues to mean “the compiler can name the value”, not “the compiler may guess its kind”.
  Integer entries are unchanged in both answer and emitted road.
- Text, void and container entries answer through the runtime's per-tag raise rather than the former static
  approximation. The message comes from one table and remains catchable by `except TypeError`.
- A one-word boundary still has no place for a tag. The row leaves it wrong or refusing under its existing rows
  rather than fabricating an integer and calling the program answered.

## Alternatives rejected

* **Make `arithWouldRefuse` accept every self-describing dict entry.** That would route `max(d[None], 2) + 1`,
  call arguments and other one-word arithmetic operands through the pair road without giving them a tag. The
  result would be a payload treated as a value — the same defect class moved to a different expression.
* **Ask the pair door for every dict subscript.** This answers the float bug but regresses the negative control
  for entries whose integer payload is already the value and needlessly allocates/tagging-reads static data.
* **Specialize the `None` key only.** The int-key and text-key twins printed the same `3`; a key-shaped fix would
  have left two of the three wrong.
* **Render non-numbers by a new static approximation.** `abs` of text, void, and container already needed exact
  CPython sentences; a fold needed CPython's operand-ordering sentence. The runtime doors own those messages.
* **Record this backend's `3` and call the row consistent.** The record leg is evidence, not the definition.
  CPython's answer was measured first; the compiled leg was cross-checked after it reached that answer.
