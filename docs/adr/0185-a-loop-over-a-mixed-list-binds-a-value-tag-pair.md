# ADR 0185: A loop over a mixed list binds a (value, tag) pair, not a value

Status: Accepted (roadmap L11.1, step 1a)
Date: 2026-08-04
Decides: how `for x in xs` works when `xs` is heterogeneous, what may be done with `x`, and
which printer context quotes.
Builds on ADR 0182 (one tag table) and ADR 0184 (per-element tags); extends both.

## Context

ADR 0184 made `[1, "a", None]` compile and print, and refused every element-wise *read* —
`xs[0]`, `xs.append(...)`, and `for x in xs` — because the read site was compiled against one
static kind. Printing the container worked because the printer could walk the tags itself.

The loop case is different from an index read, and that difference is the whole decision. With
`xs[0]` the compiler cannot know which element is wanted, so it cannot know the tag. With
`for x in xs` the tag is *available at the moment of binding* — the loop already computes the
index to fetch the element — so the tag can travel with the value instead of being guessed.

## Decision

1. **Bind both halves.** The runtime heap-list loop, when the iterable is a mixed list, fetches
   the element with `rt_get_elem(h, i)` **and** its tag with `rt_tag_of(h, i)`, storing them into
   `%_x` and `%_x_tag`. A loop variable over a mixed list is therefore a (value, tag) pair living
   in two allocas — the first place in the compiled backend where a local is more than one word.
2. **Printing dispatches on the tag at run time.** `print(x)` inside the loop emits
   `rt_print_mixed_value(v, tag, quote)`. The alternative — deciding the loop variable's kind
   statically — is exactly what produced wrong output whenever the kinds differed.
3. **The call site says str or repr.** `rt_print_mixed_value` gained a `quote` parameter:
   container printing passes 1 (Python shows `repr()`, quoted, and the interned table already
   carries the repr slot ADR 0174 added), top-level printing passes 0 (`str()`, the text
   itself). The tag cannot decide this — `[1, "a"]` and its elements are the same tags in two
   different contexts — so the caller does. This is the `str()`/`repr()` split of Gap L.2 arriving
   where it can be implemented, one context at a time, rather than as a rendering table for a
   value model that still has no bools.
4. **Anything else with the loop variable refuses.** `value()`'s `*Name` case checks
   `taggedVars` first and reports: `x comes from a loop over a mixed list; print(x) works, but
   using it as a number needs a tagged value`. Arithmetic, comparisons, calls and indexing all
   route through `value()`, so one guard covers them; the refusal is a front-end error (exit 1),
   not invalid IR. Rebinding the variable (`x = 5`) clears the tag, so a program that stops
   using the loop variable as tagged can use it normally.

## What this does not do

- Element reads by index (`xs[0]`) and appends still refuse (ADR 0184): there is no loop-carried
  tag to bring along.
- Mixed dicts/sets, floats in containers, and bools (→ L11.1 step 2) are unchanged.
- A tagged loop variable cannot be stored, passed, or compared. The pair is a printing
  convenience until the tagged value word makes a *value* itself carry its kind — the rest of
  L11.1 — at which point these refusals become ordinary typed operations.

## Verification

- `pkg/lang/mixed_list_test.go`: the loop emits `%_x_tag = alloca i32`, `call i32 @rt_tag_of(…)`
  and a tag-dispatched print; the top-level print passes `quote=0`; and using the loop variable
  as a number (`print(x + 1)`, `print(x > 2)`) refuses with a front-end diagnostic.
- `integration/mixed_list_test.go`: interpreter, JIT and CPython agree on `1\na\nNone\n`, on
  `a\n1\nNone\n['a', 1, None]\n` (str outside the container, repr inside it), on `sep=` with a
  tagged value, and on rebinding clearing the tag.
- Load-bearing by stub: pinning the tag to `0` makes the compiled loop print `1\n0\n0` and fails
  the parity rows; flipping the top-level `quote` flag fails the quote-context test.

## Alternatives rejected

- **Specialise the loop per element kind** (unroll/clone the body per tag): works for literals,
  and the static list-literal path already unrolls; but for a runtime list the kinds are only
  known at run time, and duplicating the body per kind multiplies diagnostics — one `print(x)`
  would report errors N times.
- **Make the loop variable the element's *string* form** (always stringify into a temp): would let
  `print(x)` work but make the value useless for anything else while *looking* like a value —
  precisely the "plausible but wrong" failure this area keeps producing.
- **Emit a `switch` on the tag inside every use**: correct, and what the tagged value word will
  effectively do — but doing it per use site today means every expression node needs a tag
  context, which is the L11.1 change itself, not a step toward it.
- **Refuse loops over mixed lists outright** (ADR 0184's first cut): rejected once the tag was
  demonstrably available at binding time — refusing a construct we can serve honestly is the
  same over-refusal ADR 0175 made for literals.
