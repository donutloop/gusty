# ADR 0243: an element the compiler can see holds a number is compiled as that number

Status: accepted. Roadmap L11.1 (tagged value word), fourth step; follows ADR 0238 (float boxes),
ADR 0239 (nested containers) and ADR 0241 (the tagged element read).

## The measured starting point

ADR 0241 left one clause of L11.1's first row open — the numeric use of an element. Both backends were
refusing programs whose answer CPython gives without ambiguity:

| program | interpreter | compiled (before) | CPython |
|---|---|---|---|
| `xs = [1, "a"]; print(xs[0] + 1)` | `2` | refused | `2` |
| `xs = [1.5, "a"]; print(xs[0] + 1)` | `2.5` | refused | `2.5` |
| `xs = [1, "a"]; print(1 if xs[0] > 2 else 0)` | `0` | refused | `0` |
| `xs = [1.5, 2]; print(xs[0] + xs[1])` | `3.5` | refused | `3.5` |
| `t = [[1, 2]]; print(t[0][0] + 1)` | `2` | refused | `2` |
| `xs = [1, "a"]; y = xs[0] + 1; print(y)` | `2` | refused | `2` |

One message covered all of them: *“a compiled list holds elements of more than one kind, so one element is
a (value, tag) pair … this context needs a single static kind”*. True as a description of the slot, and
the wrong reason: the context does not need a static kind, it needs the **value**, and for a container the
program spelled out and never changed, the value is something the compiler already knows.

## The decision

**Compile the element, not the slot.** When the container is one the compiler can still see through —
bound exactly once to a container literal, never mutated, never handed to a callee this pass cannot see
(the ADR 0241 promise, `containerLiteralsOf`) — and the element it names is a **numeric literal** (int,
float, or the bool both backends already store as a number), a numeric use of `xs[0]` compiles the
element expression itself. `xs = [1, "a"]; print(xs[0] + 1)` is an addition on `1`. `xs = [1.5, "a"]`
takes the float path, and takes it *as a float*, because `isFloat` asks the same question of the slot and
answers from the element.

That is the whole mechanism. No new value representation, no dispatch opcode, no runtime helper: the
three sites that refused (`g.value`'s mixed-list read, its slot-of-slot default, and `floatValue`/
`floatEval` for the float arms) now consult one question — `staticNumericElem`, asked through
`numericElemUse` where the answer has to be compiled — and fold.

## Why not a runtime tagged-arithmetic opcode

The alternative was `rt_num_add(payload, tag, payload, tag, i32* tagOut)`: an i32 that branches on the
tags, traps `TypeError` for a text or `None` operand, and returns a (payload, tag) pair so the result can
still print itself. It is the general answer and it is what a tagged value word eventually implies, but
today it would buy exactly the programs this ADR already answers (containers built from literals are the
programs people write in these probes) at the cost of:

- a second arithmetic engine, parallel to the int and double paths, where a mismatch (rounding, `%`/`//`
  sign rules, division-by-zero traps) becomes a wrong answer instead of a refusal;
- a `tagOut` pointer parameter — the module's first arithmetic helper that returns two things, which the
  rest of the runtime has always avoided;
- printing an arithmetic *temporary* rather than a variable, which the tag-carrying machinery handles for
  loop variables and bindings but not for unnamed results.

Folding needs none of that, and its precondition is the same promise the container read already needs.
The general engine stays on the roadmap as what a *dynamic* tag requires; this ADR covers the static case,
which is where the refusals were.

## Why literals and not names

`staticNumericElem` refuses an element that is a *variable*, even though `staticElemExpr` can resolve it:

```gy
a = 1
xs = [a, "b"]
a = 5
print(xs[0] + 1)     # CPython: 2
```

The container is unchanged, so the ADR 0241 promise still holds — but the promise is about the *object*,
not about what its elements' names were bound to. The slot holds the value `a` had when the list was
built; reading the variable at the point of use reads the value it has now. A literal cannot drift, so the
fold is restricted to literals and their negations. (A runtime-built or name-filled element keeps the
refusal, which says which promise ran out.)

## What still refuses, and says so

| shape | message |
|---|---|
| text/None/container in the slot, used arithmetically | `… this context needs a single static kind (roadmap L11.1, ADR 0187)` — CPython raises `TypeError` here, and refusing is closer than computing on an interned index |
| an element of a mutated or handed-off container | `… cannot reach into xs's slots: the name was rebound, mutated, or handed to code this pass cannot see …` |
| a loop variable over a mixed list | `… using it as a number needs a tagged value …` |
| a runtime index (`xs[i]`) | the fold needs a constant key, so the refusal stands |

## Coverage

- `pkg/lang/container_slot_read_test.go` — the answering table gained the numeric rows (`+ - * / // %`,
  unary `-`, a negated literal element, comparisons, an element equal to a literal, bool elements in a
  test and against `True`, two elements, an element through a function, a bound element, an element
  accumulated in a loop, a dict value's element, a float element two slots down) and the refusal table
  gained the shapes that must still refuse (container element arithmetic, a text element used as a number,
  a `None` element used as a number, a mutated container's element, a loop variable).
- `pkg/lang/mixed_list_test.go` — `TestMixedListElementUsesStillRefuse` and
  `TestTaggedElementRefusalNamesWhatWorks` moved from `[0]` (now answered) to `[1]` (a text element, still
  a refusal), and the second now also pins the loop-variable refusal.
- `integration/container_slot_read_test.go` — the same numerics through the CLI against CPython with the
  exit-2 guard; the trap table gained a text element and a container element used as a number, where
  CPython raises `TypeError` and both engines must not exit 0; `TestNestedShapesThatStillRefuse` keeps the
  remaining shapes refused.

Measured: 19 of 20 probe programs (arith, comparisons, calls, conditions, loop bodies, nested reads, text
concatenation of an element through `str()`) now print CPython's answer on **both** backends. The one that
does not — `max(xs[0], 5)` — is not a tagged-value gap: this backend takes one argument for `min`/`max`
on both engines, so it is a separate missing feature and gets its own row (Gap R.73).

## Consequences

- `docs/language.md`: the element-read section now lists the numeric uses and the four refusals.
- Roadmap L11.1's first row keeps its 🟨 status: what is left of the tagged value word is a *dynamic* tag
  used numerically — a loop variable over a mixed list, an element read through a runtime index, a
  container no literal ever described.
- Gap R.73 filed: `min`/`max` accept one argument on both engines where CPython accepts two (measured while
  writing the numeric probe table; both engines agree, so it is missing surface rather than a divergence).