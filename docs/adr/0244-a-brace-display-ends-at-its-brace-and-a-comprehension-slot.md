# ADR 0244: a `{…}` display ends at its brace, and a comprehension slot is written with its tag

Status: accepted. Roadmap Gap R.74 and Gap R.75 (both found and closed in this cycle); related:
L11.1 (the tagged value word), Gap R.46 (comprehension elements print their index), ADR 0187
(payload and tag are one write), ADR 0232 (a container stops claiming one kind), ADR 0234
(comprehensions), ADR 0239 (a container element is a handle), ADR 0241 (the tagged element read).

## The measured starting point

One shape, two independent defects, and both backends agreed on the wrong answer:

| program | CPython | both backends (before) |
|---|---|---|
| `[{1, 2} for x in [1]]` → `print(len(d[0]))` | `2` | refused (after a mis-parse that made it a set of one member) |
| `[{"k": x} for x in [1, 2]]` → `print(len(d))` | `2` | `1` — one dict, holding *both* entries |
| `[[1, 2] for x in [1]]` → `print(xs[0])` | `[1, 2]` | printed nothing: the module was rejected (exit 2) |
| `[1.5 for x in [1]]` → `print(xs)` | `[1.5]` | `[1]` |
| `[None for x in [1]]` → `print(xs)` | `[None]` | `[0]` |
| `["a" for x in [1]]` → `print(xs)` | `['a']` | `a` |

Nothing in a parity suite sees any of this: the two backends produced the same wrong text. It took
comparing each line against CPython to find it, which is the argument the oracle ledger exists to make.

**Defect one — the parser.** `parseDictOrSet` had two `for` branches: one *inside* the braces (correct —
that is how `{x for x in xs}` works) and one *after* the closing `}`. The second one exists to serve the
call-argument form `len({x * x} for x in xs)`, where the display really is the whole expression the
`for` completes. Inside a `[` it is a theft: the `for` belongs to the **enclosing list comprehension**,
and taking it turned `[{1, 2} for x in xs]` into "a list holding one set comprehension". Both backends
then faithfully evaluated the wrong program — which is why the dict row printed one dict containing
every entry: there was only ever one dict, and it was being filled once per item.

**Defect two — the comprehension builder.** `runtimeCompList` and `runtimeCompLoop` built each element
with `g.value` alone and appended it, falling back to the **untagged** `rt_append`/`rt_set_add` when
`elemKindTag` could not name the kind:

```
call void @rt_append_tagged(i32 %h1, i32 @.set1, i32 7)   ; llc: global variable reference must have pointer type
```

A container element compiled to the compiler's *global*, not to a heap handle. That is an exit 2 — the
CLI reporting that `llc` rejected a module this compiler wrote, which the exit-code contract classifies
as a bug rather than a refusal. The non-container elements were answered too, but by the wrong printer:
a float element printed its box handle (`[1]` for `[1.5]`), a `None` element reached the constant-fold
path where `foldConstInt(None)` is the integer 0 (right answer to `if None:`, wrong answer to "what is
in this slot"), and a text element made the *variable* an interned-string variable, so `print(xs)`
called `rt_print_str` on a list handle and wrote a bare `a`.

## The decision

**One: a display stops at its brace.** The parser carries `inListLit`, the number of `[` displays whose
element list an expression is being parsed inside. A `{…}` display may finish itself into a
comprehension on a following `for` **only at depth 0**. Inside a list display it returns the literal and
lets `parseListOrComp` build the comprehension over it. The call-argument reading is untouched — which
the AST table pins, because that form is the only reason the branch exists.

**Two: the element goes through the door `xs.append(v)` already used.** One helper,
`elemPayloadAndTag`, answers the payload word, the tag that says what it is, and whether the payload is
an interned index — via `heapElemKind`, which materialises a container element into the heap instead of
handing back a global. Both comprehension builders call it; there is no untagged `rt_append` or
`rt_set_add` left reachable from a comprehension, so a slot cannot be written with the tag of whoever
held it last (ADR 0187's rule, applied where it had been bypassed). `mixedElemTag` now delegates to the
same helper, so the append path and the comprehension path cannot drift.

**Three: an object whose slots need tags says so, and the read asks.** When the element's tag is one a
compiled word cannot render — float, `None`, another container — the builder marks the object with
`rt_mark_estr(h, 8)` ("ask my slots", ADR 0232) *and* the binding records the variable as tagged, so
`print(xs[0])` reads the payload and the tag together. Marking only the object left `print(xs)` correct
while `print(xs[0])` still printed the handle — half the pair, and the row that proves you have to
follow a tag all the way to the read.

**Four: the fold may not answer a value question with a truthiness answer.** `foldsToAnInteger` keeps a
`None`, text or float element out of the constant-fold path. That path builds an `i32` global of folded
integers, which is exactly why `[None …]` printed `[0]`.

## What this deliberately does not decide

`len({x * x} for x in xs)` — an unparenthesised display-then-`for` as a call argument — remains a
display comprehension in this language; CPython builds a **generator** there, so CPython raises
`TypeError: object of type 'generator' has no len()`. Changing that is a generator-object decision
(L7.x/Gap R.71 territory), not a brace-placement one, and it is left as it was: the AST table records
the reading this language gives the form, and the divergence is named in the roadmap rather than asserted
as CPython's answer.

Gap R.46 also stays open with its remaining shape narrowed: `[n for n in names if n == "a"]` where
`names` is a *run-time-grown* text list still prints `0`, because the loop variable's kind is lost when
the iterable is promoted to mixed. Measured identical on the pre-change binary, so it is not a
regression this change introduced — and it is not the same defect as the four above.

## Consequences

- `pkg/lang/comprehension_brace_element_test.go` — the AST shape table (the parser's own contract: two
  engines agreeing on output can still be agreeing on the wrong *program*), plus the interpreter rows.
- `integration/comprehension_brace_element_test.go` — 21 programs × both engines against CPython with
  the exit-2 guard, an honest-refusal table (the interpreter answers, the compiler refuses by naming the
  promise), and a trap table for the positions CPython itself dies on.
- Roadmap: Gap R.74 and Gap R.75 recorded as closed here; Gap R.46's row narrowed to the
  runtime-grown-iterable shape it still fails, with the pre-change measurement attached.
- `docs/language.md`: the comprehension section states the brace rule and that a comprehension's element
  is a value — a container, a float, `None` or text — and is stored as one.
