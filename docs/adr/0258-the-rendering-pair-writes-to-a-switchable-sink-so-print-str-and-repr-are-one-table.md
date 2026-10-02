# ADR 0258 — the rendering pair writes to a sink, so `print`, `str()` and `repr()` are one table

Date: 2026-10-02 · Status: Accepted · Roadmap: L11.2 (closes Gap L.2), Gaps R.114 / R.115 (filed), Gap R.67 / R.112 / L11.3 (named as the boundary), ADR 0183 (the `str()` folds), ADR 0185 (the `quote` flag), ADR 0187/0189/0232 (a slot is a payload *and* a tag; an object says how its slots are stored), ADR 0209 (a runtime block carries what it calls), ADR 0224 (a string value is an intern index), ADR 0233 (a float prints with Python's rendering), ADR 0257 (a verdict is printed from the expression), ADR 0166 (a broken module is our bug), ADR 0186 (the three-leg oracle)

## Context

The roadmap row is one line: *`str()` vs `repr()` are one pair per backend*, closing Gap L.2 (`str()` vs `repr()` decided in two places). Its definition of done: *a table test drives every value form through both backends + CPython; `print(set())` is `set()`*.

Measuring first, against `python3`, found that `repr` did not exist and `str` was a second renderer that had never been kept in step with the first:

| program | CPython | `--interp` | `--aot`, before |
|---|---|---|---|
| `repr(1)` | `1` | `NameError`, exit 3 | `unsupported call "repr"`, exit 1 |
| `xs = [1, 2]` · `str(xs)` | `[1, 2]` | `[1, 2]` | **`0`**, exit 0 |
| `str(None)` | `None` | `None` | **`0`**, exit 0 |
| `x = 1.5` · `str(x)` | `1.5` | `1.5` | `str on non-integer`, exit 1 |
| `str({1})`, `str(set())` | `{1}`, `set()` | same | **exit 2** — `llc` rejected the module |
| `xs = [[1, 2], [3]]` · `str(xs)` | `[[1, 2], [3]]` | same | **`[1, 2]`** — two inner handles |
| `xs = ["a", 1]` · `str(xs)` | `['a', 1]` | same | **`[0, 1]`** — two intern indexes |
| `xs.append("v" + str(7))` · `print(xs)` | `['v7']` | `['v7']` | **`(null)`** |
| `n = 42` · `repr(n)` | `42` | `42` | refusal |

Six of those had **exit 0**. That is the shape of the defect: `print` had a printer — the module's `rt_print_*` family, chosen per shape, quoting container elements — and `str()` had a *number formatter* beside it, so any shape the second one had not been told about fell through to the integer underneath. A list answered with its heap handle, `None` answered `0`, and nothing in the output distinguished "wrong" from "answered". The two backends agreeing on `0` was not rare, so the two-backend matrix saw nothing; the CPython leg is the only engine in the building that could see it, and it had not been pointed at these programs.

Two neighbouring facts came out of the same sweep. First, the compiled float renderer is `rt_fmt_double` (ADR 0233) and `str()` had been refusing floats rather than calling it — so the pair's disagreement about a float was `1.5` versus a refusal, not `1.5` versus `1.5`. Second, three container builders (the list-assignment path among them) wrote per-slot tags **without saying so on the object**, so `print(["a", 1])` picked its printer from the compiler's scope and was right while anything that asks the object was wrong; the set and dict assignment paths had set `@estr` bit 8 all along and the list one had not. That is Gap L.2's disease — one fact decided in two places — showing up one level below where the row said to look for it.

## Decision

**One renderer per backend, asked twice, writing to a switchable sink.** A form is added to one table, or it is added to neither.

*Compiled.* The `rt_print_*` family no longer calls `printf`. All 51 of its writes go through two functions:

```
@rt_capturing = internal global i32 0
@rt_cap_len   = internal global i32 0
@rt_cap       = internal global [65536 x i8] zeroinitializer

define internal void @rt_out_txt(i8* %p)   ; puts, or append to @rt_cap
define internal void @rt_out_int(i32 %v)   ; printf "%d", or snprintf into @rt_cap
```

With `@rt_capturing` 0 that is the old behaviour, one instruction apart. With it 1, the same bytes land in `@rt_cap` and nowhere else. `str(x)` and `repr(x)` are then the three-line program: point the sink at the buffer, run the printer `print` would have run, intern what was written. `rt_str_of_container(h, quote)` asks the *object* which printers its slots need — the same dispatch `print` reaches by the same route — and `rt_str_of_value(v, tag, quote)` renders one value by its tag. The `quote` flag ADR 0185 introduced for container elements now carries the pair, and it is read only by the text arms at the top of a render: a text is the one value whose two halves differ, and inside a container both halves quote, which is what makes `print(xs)` and `str(xs)` the same line rather than two opinions.

*Interpreter.* `Repr` was already the renderer `print`, `str()` and the REPL echo shared. `repr()` delegates to it and differs only where Python's pair differs — a text returns `pyReprString(o.sval)`. `ValueForm` (`FormStr`/`FormRepr`) is the whole vocabulary of the choice, in one file, beside the codegen half.

*Neither half guesses.* A form the expression cannot name — a value handed over as an untagged word — is refused in words with exit 1, naming which half is missing. A tuple refuses rather than printing `[1, 2]`. An unknown that reaches the digits arm is the residual, filed as Gap R.115 rather than quietly kept.

## Consequences

Fixed, all measured before and after against `python3`: the seven rows of the table above, plus `str(2.0)` answering `2.0` and not `2`, plus `str(set())`/`str(list())`/`str(dict())` (a constructor is a container once the checker's inferred type — the second witness `renderPairContainer` now consults — says its kind), plus `print(repr(x))` writing the text instead of the intern table's position, which needed `repr` registered in *both* string-answer predicates (`exprIsString` **and** `printsAsInternedStr`), because print asks the second one and `str()` asks the first.

Three container builders now mark their objects: bit 1 for interned text, bit 8 for slots that must be read one at a time (`heapListFrom`, the list-assignment builder; the dict and set twins already did). `print` does not need this and never did — it reads the compiler's record — which is exactly the point: the renderer now has callers the builder's scope never saw, and an object that cannot describe its own slots misrenders for them.

`str()` gained a capability nobody asked for and nobody can take back: a **double can travel into `str()`**, because the renderer that prints a float writes into the capture buffer. Two refusal rows left `slot_division_test.go` (unit and integration) for that reason and are pinned as parity at CPython's `3.0` instead; the sinks that store an `i32` word — a call argument, a dict slot, `+=` onto an `int` — still refuse, so ADR 0226's boundary moved exactly as far as the pair reaches and no further.

Two latent bugs fell out of code the pair had to exercise. `rt_repr_of_text` — the runtime half of quoting, needed because only the compiler had ever produced a repr (ADR 0224 filled the repr slot only for text it could see) — called `@strlen` whose `declare` lives in a different runtime chunk: a module that rendered a repr without building a string had the call and not the declaration, and `llc` called that an undefined value (ADR 0209's rule, re-earned). Its first draft also never stored the closing quote, so a run-time-built text rendered as `['v7]` — a test written against CPython caught it; a test written against the interpreter would not have.

The pair is now asserted structurally, not just behaviourally: `TestRenderPairIsOneTableNotTwo` fails a module that stops reaching `rt_str_of_container` / `rt_out_txt`, or that grows a second value renderer. Behaviour tests show a wrong answer; this one shows the *return* of the condition that made wrong answers possible.

## Agentic rationale

`--json --eval 'repr("hi")'` → `{"result": "'hi'", "type": "str", "backend": "interpreter", "exit": 0}`. An agent that wants a value as text asks for it as text and gets the same characters the console shows, from either backend, with the type it rendered as; an agent that hits the boundary gets exit 1 with the missing half named in the `error` member. `programs/probe_render_pair.gy` is `match` on all three legs, so a regression in either backend's half of the pair fails the matrix rather than being caught by one backend agreeing with the other. `TestPairAgreesWithTheOracle` keeps the `--oracle` leg honest about the pair's unfinished neighbours: a bool inside a rendered container is still exit 6 debt (Gap R.112), because a pair that renders the container is not a pair that knows what the slot holds.

## Alternatives rejected

- **Duplicate the container printers as string-producing twins.** Rejected as the same mistake in better clothes: two tables that must be edited together are two places, and Gap L.2 *is* the record of two places drifting. The capture sink costs one global and one branch in two functions, and thereafter a form exists once.
- **A shared Go `ValueForm` renderer used by both backends** (the obvious "one table" reading). Rejected because the two backends render from different data — the interpreter from heap objects with kind strings, the compiler from untagged i32 words plus `@estr` flags and tag arrays — and a Go table cannot see a tag it was not handed. It would have been a third table, and the third would have quietly disagreed with the two that produce output. What is shared instead is the *test*: one table, every form, both backends, CPython's answer.
- **Give `str()` its own runtime string builder** (`snprintf` per shape into a fresh buffer). Rejected: it is the twin again, and it would have had to re-derive the `@estr`-flag and tag dispatch that already decides how a container's slots are read.
- **Answer digits for anything the compiler cannot classify, as `str()` already did.** Rejected for the halves the compiler *can* name (a container is never a number again) and filed for the rest as Gap R.115 rather than defended: `str(x)` answering `3` for an empty set is the exact failure mode this ADR exists to make impossible, and an answer that cannot be distinguished from a bug is not a convenience.
- **Let `str()` fold at compile time where it can and call the runtime where it cannot, keeping the folds as the fast path.** Kept, but only after the folds were made to agree with the runtime: `str(2.0)` folding to `2` while `str(x)` of the same value rendered `2.0` is the two-places disease inside one builtin, and the fold now asks `pyFloatRepr`, the same function the print path asks.
- **Point `printf` at a redirected stdout for the capture.** Rejected: it would capture the *program's* output too — a `print` inside a rendered expression would land inside the string — and the module's other `printf` callers (traps, diagnostics) would be swallowed mid-render.
