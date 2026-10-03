# ADR 0260 — a dict is a key → value mapping however the interpreter builds it

Date: 2026-10-03 · Status: Accepted · Roadmap: closes Gaps R.118 and R.120; files Gaps R.121, R.122 and
R.123 ·
Related: ADR 0259 (a bool is a nameable element kind — the same sweep, and the key-equality rule this one
leans on), ADR 0232 (a slot is a payload *and* a tag; dict lookup compares both), ADR 0186 (the three-leg
oracle and the promotion rule), ADR 0190 (every feature ships its least interesting program), ADR 0166 (a
refusal is a divergence, never a pass)

## Context

ADR 0259's sweep left one row that was not about bools at all. `{1: 2 for x in [1, 2]}` printed
`{1: 2, 1: 2}` in the interpreter and `{1: 2}` compiled, with CPython on the compiled side. Filed as Gap
R.118, it looked like a comprehension bug. Writing the probe's neighbours made the real shape obvious: the
literal has it too, and so does every other way the interpreter grows a dict.

Measured before this change, `python3` against `--interp` against `--aot`:

| program | CPython | `--interp` | `--aot` |
|---|---|---|---|
| `{"a": 1, "a": 2}` | `{'a': 2}` | **`{'a': 1, 'a': 2}`** | `{'a': 2}` |
| `{1: "a", 1: "b"}` | `{1: 'b'}` | **`{1: 'a', 1: 'b'}`** | `{1: 'b'}` |
| `{1: 1, True: 2}` | `{1: 2}` | **`{1: 1, True: 2}`** | `{1: 2}` |
| `{True: 1, 1: 2}` | `{True: 2}` | **`{True: 1, 1: 2}`** | `{True: 2}` |
| `{1.0: "a", 1: "b"}` | `{1.0: 'b'}` | **`{1.0: 'a', 1: 'b'}`** | `{1.0: 'b'}` |
| `{None: 1, None: 2}` | `{None: 2}` | **`{None: 1, None: 2}`** | `{None: 2}` |
| `d = {1: 2 for x in [1, 2]}` · `print(d)`, `len(d)` | `{1: 2}`, `1` | **`{1: 2, 1: 2}`, `2`** | `{1: 2}`, `1` |
| `d = {"a": 1}` · `d["a"] = 2` | `{'a': 2}` | `{'a': 2}` | `{'a': 2}` |

Three things about that table matter.

The **interpreter was the diverging engine**. That is rare enough to be worth saying twice: the corpus, the
matrix and this project's instincts are all built on "two backends agree because they share a bug", and here
the compiled backend was right on every row. Its fold deduplicates keys as it unrolls, and its runtime put
(`rt_dict_put_tagged`) updates the value in place. Nothing had ever asked the interpreter the same question.

The defect was **not the comprehension**. It was the interpreter's dict builders — the literal (`DictLit`),
the comprehension (`CompDict`) and the `dict(d)` copy — each of which grew the container with
`o.elems = append(o.elems, key); o.dvals = append(o.dvals, val)` and never asked whether the key was already
there. Item assignment, the fourth builder, did the lookup and was correct. Four builders, three rules.

And the row is **silent in exactly the way the last cycle's rows were**: `print(d)` looked like a dict, `len`
returned a number, and both were wrong. Only the CPython leg could see it, and the corpus had never written a
dictionary with a repeated key — the least-interesting program nobody probes because it works.

## Decision

**One door for every dict the interpreter builds.** `Evaluator.dictPut(o, key, val)` is now the only place a
dict's entry arrays grow:

```go
func (e *Evaluator) dictPut(o *obj, key, val int64) {
    for i, k := range o.elems {
        if e.dictKeyEq(k, key) {
            o.dvals[i] = val
            return
        }
    }
    o.elems = append(o.elems, key)
    o.dvals = append(o.dvals, val)
}
```

Three rules fall out of it, all of them CPython's:

* a repeated key **replaces the value** — the entry count is the number of distinct keys, so `len` and
  `for k in d` stop counting ghosts;
* the entry keeps its **position from first insertion** (`{"a": 1, "b": 2, "a": 3}` is
  `{'a': 3, 'b': 2}`), because insertion order is the language's dict ordering (ADR 0188's two-word entries
  and `for k in d` depend on it, and rewriting position on update would make iteration order a function of
  assignment history);
* the **key that survives is the first one written** — `{1: 'a', True: 'b'}` prints `{1: 'b'}`, not
  `{True: 'b'}` — which is what CPython's dict does and what makes the bool/float spellings agree with the
  numeric answers ADR 0259 settled.

The comparison is `dictKeyEq`, the same one item assignment, `d[k]`, `in` and `get` already used: it unboxes
a verdict and asks a float box for its number, so `1`, `True` and `1.0` are one key. That is why this change
could not land before ADR 0259 — with bools in container slots compared by handle, a `dictPut` built on the
old key equality would have merged the wrong pairs and looked like a regression.

All four builders call the door: the literal, the comprehension, item assignment (whose hand-rolled loop it
replaces) and `dict(d)` — the copy splicing two arrays before, which was only safe because the source could
not have had duplicates; now that safety is a property of the code rather than of the argument.

The compiled backend needed **no change**, and that is the result being recorded: the fix moved one engine to
the rule the other already followed, rather than adding a behaviour to both.

## Consequences

Fixed, measured before and after against `python3`, eight rows above all answering CPython's line on both
backends with exit 0. `programs/dict_key_rule.gy` joins the conformance corpus as the least-interesting
program that was missing (ADR 0190), and `programs/probe_dict_comprehension_duplicate_key.gy` — filed as debt
by ADR 0259 the same day — left the ledger for the parity list, which is ADR 0186's promotion rule firing
within one cycle. `TestOracleStillCallsTheBoolNameLossShapes` lost its fourth row rather than re-pinning it:
an exit-code contract pinned to a closed debt passes forever without asserting anything.

The behaviour is asserted three ways, because the condition is structural: a table of 20 shapes through both
backends (`pkg/lang/dict_key_rule_test.go`), the shipped binary against CPython plus the oracle's own exit
class (`integration/dict_key_rule_test.go`), and a source tripwire (`TestEveryDictBuilderWalksTheOneDoor`)
that fails if a fifth builder grows `dvals` outside `dictPut`. The last one is the one that matters in two
years: the behaviour rows would pass again the day someone re-introduces a second copy of the rule for a new
builder, which is exactly how three became four here.

Two shapes measured on the way and **not** fixed, filed instead (ADR 0166's discipline — a probe that cannot
run is recorded, not skipped):

* **Gap R.121** — dict display unpacking does not parse: `{**d, "a": 2}` dies with `parse error: unexpected
  token` on both paths, where CPython prints `{'a': 2}`. It belongs beside Gap R.58/L12.6 (the star half of
  the call surface), and until the parser has it nothing behind the parser can.
* **Gap R.122** — a dict comprehension with a tuple target, `{k: v for k, v in pairs}`, traps in the
  interpreter and refuses in codegen where CPython answers. Owner L11.3 (tuples are values): the target needs
  a tuple to unpack, and `TupleLit` still has no lowering at all.
* **Gap R.123** — a dict comprehension with a **text** key, `{"k": v for v in [1, 2, 3]}`, is `{'k': 3}` in
  the interpreter and CPython and `comprehension key must be constant` at exit 1 compiled, while the integer
  spelling of the same line compiles. An honest refusal, so nothing is wrong today; the interpreter leg is
  pinned against CPython in the same test so the day the builder opens, the answer is already on the record.

## Agentic rationale

An agent that generates a dict-building program cannot tell from the exit code that its dictionary has the
wrong shape: the interpreter exited 0, printed something dict-shaped, and answered `len` with a number. The
only artifact that said anything was the conformance ledger, which pinned that wrong answer as debt with a
per-leg `Stdout` pin — which is how this was found at all, and why the pins have to be per-leg rather than a
single "expected output": the compiled leg's answer was already CPython's and had to stay pinned that way or
the fix would have looked like a change to the wrong engine.

`--json` is the machine path for the fixed behaviour, and it now reports what the object is rather than what
the builder appended: `len(d)` is `1` and `d["a"]` is the value the last write put there. Nothing in the JSON
schema changed, because no new kind appeared — the change is that one engine's dicts mean what the other's
and the oracle's already meant.

## Alternatives rejected

* **Fix the comprehension only.** That was Gap R.118's own text — "the interpreter's `CompDict` path" — and
  it was written from the one shape that happened to be probed. Fixing the literal and the copy the same day
  costs one helper; leaving them costs the next reader a repeat.
* **Deduplicate at print/`len` time.** Filtering the display would hide two entries instead of removing
  them: `d[k]` would still find the first value where CPython returns the last, and `for k in d` would still
  visit the key twice for anything that iterates the real array.
* **Let the last write move the entry to the end.** Python does not: `{"a": 1, "b": 2, "a": 3}` is
  `{'a': 3, 'b': 2}`. Following CPython here is not fussy — dict order is language surface, and this
  language's own `for k in d` and ADR 0188's entry layout promise a stable, insertion-ordered walk.
* **Keep the bool key spelled by the last write.** `{1: 'a', True: 'b'}` printing `{True: 'b'}` would be the
  natural reading of "last write wins", and it is wrong: the entry is the first one, so the first key object
  is the one that stays.
* **Also give the compiled backend a `dictPut`.** There was nothing to change: the fold dedups keys and
  `rt_dict_put_tagged` updates in place. Writing a second implementation to make the commit look symmetric is
  how two rules start.
* **Leave `dict(d)` splicing its arrays.** It is not reachable today, and that is precisely the argument
  against: an unreachable path that encodes the old rule is a defect waiting for its first caller.
