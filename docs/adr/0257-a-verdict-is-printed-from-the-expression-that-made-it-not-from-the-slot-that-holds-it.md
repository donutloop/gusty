# ADR 0257 — a verdict is printed from the expression that made it, not from the slot that holds it

Date: 2026-10-02 · Status: Accepted · Roadmap: L11.1 (bools are values, step 2 of the tagged-value row), Gaps R.111 / R.112 (filed), ADR 0186 (the three-leg oracle), ADR 0221 (a builtin returns the value its argument had), ADR 0232 (a slot's type word), ADR 0238/0239/0241 (the tagged container read), ADR 0253 (the double domain), ADR 0254 (one predicate answers a value's kind), ADR 0166 (a broken module is our bug)

## Context

L11.1's second step is one line of a roadmap table: *then bools are values (`print(True)` → `True`)*. Its definition of done was `probe_bool_value` promoted out of the probe list and `--json` reporting the bool type. The plan column said how: *give bool its own `ValueTag`; one line of `elemKindTag` and every container follows.*

Measuring the row first found that the missing answer was in two different places, and that only one of them is a tag.

| program | CPython | `--interp`, before | `--aot`, before |
|---|---|---|---|
| `print(True)` | `True` | `1` | `1` |
| `print(1 == 1)` | `True` | `1` | `1` |
| `print(0 == None)` | `False` | `0` | `0` |
| `print("yes" == "yes")` | `True` | `1` | `1` |
| `ok = 1 == 1` / `print(ok)` | `True` | `1` | `1` |
| `print(str(True))` | `True` | `1` | `1` |
| `print(f"{1 > 2}")` | `False` | `0` | `0` |
| `print([True, 1])` | `[True, 1]` | `[1, 1]` | `[1, 1]` |
| `def show(f): print(f)` / `show(True)` | `True` | `1` | `1` |
| `--json --eval 'True'` | — | `"type": "int"` | — |
| `print(a < b)` where `__lt__` returns an int | `1` | `1` | `1` |

Five oracle rows in the conformance ledger — `container_methods`, `none_values`, `string_containers`, `string_escapes`, `string_params` — were pinned as debt for exactly one reason each: a comparison in the program printed `1`/`0` where CPython printed `True`/`False`. Six rows, counting the probe itself. The comparison was never wrong; only its rendering was.

The row's plan was a tag, and a tag is what the container cases need — but the tag is heap-object vocabulary: `gc.kinds` numbers an `int`, a `str`, a `list`. A bool has no object; it is an immediate, the way `int` is, and the two backends already agree it is `0`/`1`. Giving bool a `ValueTag` would mean either inventing a heap cell per verdict — a new allocation for a value that fits in the word it already occupies — or numbering a kind no tag reader would ever ask about, because the readers ask about objects. And it would still not answer `print(1 == 1)`: a comparison's result never went near a container, which is where `elemKindTag` lives. The tag was the plan for half the row, and not the reachable half.

The other half had a different property: **nothing about those programs was unknown at compile time**. `1 == 1` answers a question. `not x` answers a question. `a in xs`, `f()` when every `return` in `f`'s body is a verdict — all of them are expressions whose being-a-bool is a fact about the AST, not about a run-time object. The renderer was asking the storage what it had, when it could have asked the program what it wrote.

## Decision

**The verdict's name comes from the expression, not the slot.** One function answers the question, in the shared package, and is the only thing allowed to decide how a value is written:

```go
func IsBoolExpr(e Expr, env BoolEnv) bool
```

`BoolEnv` carries what the site knows: the names in scope that were last bound to a verdict, a `Lookup` for a named callee's body, a `Shadowed` hook for a name the backend already lost to something it cannot rule out, and an `Instance` hook that says whether an operand is a class instance and therefore whether a comparison reached a dunder at all. Both engines build the same environment from the same AST walk: the interpreter's `Evaluator.boolVars`, the codegen's `irGen.boolVars`, both saved and restored on scope entry and swapped with the function's own scope, so recursion and nesting cannot leak.

The rule is the Python one, read off the node:

- `True`, `False` are verdicts.
- a comparison (`==`/`!=`/`<`/`<=`/`>`/`>=`), a membership (`in`, `not in`) and an identity test (`is`, `is not`) are verdicts — **unless** the receiver is an instance, because then it is the program's own `__lt__` that ran, and what it returned is its business, not a verdict. `dunder.gy`'s `print(a < 4)` prints `1` on all three engines and that is not a debt row.
- `not x` is a verdict; `and`/`or` are verdicts only when both sides are, because Python yields the *operand*, so `1 and 2` is `2` and must stay `2`.
- a ternary is a verdict only when both arms are: `True if c else False` is, `1 if c else 0` is not.
- a walrus is a verdict when the value bound is.
- a call is a verdict when it is a builtin that answers a question (`all`, `any`) or every `return` in the callee's body is a verdict — ADR 0254's rule, read from what the body does rather than from a declared word.
- a name is a verdict when its **latest** binding was one. `flag = 1 == 1` then `flag = 5` prints `5`, because the slot holds a number now. A name bound by anything this pass cannot see — a `for`/`while` loop variable, an `except ... as`, a parameter, an `import` alias — is not a verdict, and is *forgotten* rather than recorded false, so a name that was a verdict before the loop and an element inside it cannot print `True` for `2`.

Where the answer is yes, the printer writes the word; the storage is untouched:

- the interpreter's `print`, `str()` and f-string interpolation call `BoolText(truthy(v))`;
- the compiled backend's `print` gains one arm — after the container arms and the tag read, before the `%d` fallback — calling a new `rt_print_bool(i32 %v, i32 %nl)` over the two constants `@.fmttrue`/`@.fmtfalse`. Because that helper lives in the heap runtime, the arm sets `g.heapUsed`, so a module that prints a verdict pulls in the runtime's globals rather than `llc`'ing into `use of undefined value '@gc.kinds'`;
- `str(True)` selects between the two interned constants at run time and hands back an ordinary `@str_tab` index, so `.lower()` on it is text rather than a number wearing a mask;
- an f-string calls a new `rt_bool_text`;
- `--json` reports `{"result": "True", "type": "bool"}` and `--eval` echoes `True`, which is the machine path of the same answer, asked of the final expression rather than of the value.

Nothing about arithmetic changed. A verdict is still the `0`/`1` every numeric path already understood: `True + 1` is `2`, `True * 3` is `3`, `-True` is `-1`, `sum([True, True, False])` is `2`. The change is one-way: the way a bool is *read* is unchanged, only the way it is *written*.

Three paid rows and two new ones came out of this. `probe_bool_value` moved to `conformanceStandalone()`; `container_methods`, `none_values`, `string_containers`, `string_escapes` and `string_params` all flipped from debt to match, because their only divergence had been a comparison printing a number. And two shapes stayed, for the honest reason that they are where a tag really is needed:

- **Gap R.111** — a bool handed to a function prints `1`. The argument's expression is a fact the caller knows and the callee does not: a parameter is a fresh binding, and the AST that made the value is on the other side of the call. This is ADR 0256's Gap R.110 and Gap R.80's shape — the tag must *travel*, and only the tagged value word makes it travel.
- **Gap R.112** — a bool in a list or dict prints `1`. This one is the row's original plan, precisely: the element tag vocabulary a container carries has no bool in it, so the container printer reads the immediate. Adding a kind to that vocabulary is the closure, and it is a container-tag change, not a heap-object `ValueTag`.

## Consequences

- `pkg/lang/boolvalue.go` is new: the predicate, the environment, and `BoolText`. Neither backend owns the answer, which is the point — the two engines have disagreed about bools before, and a rule that lives twice is a rule that will.
- `pkg/lang/bool_value_test.go` pins 26 renderings × both engines against CPython's answer, the truthiness rules that must not move, the `IsBoolExpr` table (including the `and`/`or` and ternary asymmetries, which are the interesting part of the rule), the dunder exclusion, and — as they answer today, not as CPython does — the two filed shapes. `integration/bool_value_test.go` drives the same through the shipped CLI against real `python3`, and pins that `--oracle` still calls the two filed shapes divergences, so closing one has to change a verdict as well as a number.
- The two probe programs the gaps are measured by, `probe_bool_through_a_call.gy` and `probe_bool_in_a_container.gy`, are conformance surface with ledger rows carrying each engine's exact answer.
- `integration/escapes_test.go` **lost** its `normalizePy` helper, which rewrote CPython's `True` into `1` and its `False` into `0` before comparing. That helper was the reason six rows could sit in the ledger labelled debt for the wrong reason: the comparison had been written so the oracle could not lose. An oracle is only worth having when it can disagree.
- Roughly 20 pinned expectations across `pkg/lang` and `integration` were rewritten from the number a bool used to print to the word CPython prints — and each one was checked against `python3` rather than against the previous expectation, which is the only way a mass re-pin is safe.
- `TestSlotOrderOfABoolSlotNamesIntUntilL11_2` stays exactly as it is. It is not this row: it asks what *TypeError* says when a bool orders against a text, and that sentence names the slot's tag, which still has no bool (Gap R.112). Its comment now says so, because a test named for the L11.2 bool tag that has nothing to do with printing is a trap for the next reader.
- No CLI flag, JSON field or exit class changed; `type` gained the value `bool`, which the JSON schema already allowed as a string. `--oracle`'s contract is untouched, and the corpus moved 6 rows from debt to match (debt 20 → 14, match 72 → 78) with the two new probes filed back (16).
- L11.1's remaining bool work is stated where it belongs: not in this ADR's prose but in two gap rows with programs, pins and an owner.

## Alternatives rejected

- **Give bool a heap-object `ValueTag`, as the row's plan column said.** `ValueTag` numbers heap objects and `elemKindTag` classifies container elements. The programs that were wrong had neither a container nor an object in them; the tag would have been read by nothing that needed the answer, and `print(1 == 1)` would still have printed `1`. The container half of the plan is real and is filed (Gap R.112), with the correct vocabulary named.
- **Box every verdict as a heap object.** It would make `print(True)` a printer lookup and `True + 1` an unbox — an allocation per comparison, in a language whose whole argument is that a verdict fits in the word it already lives in, for a rendering.
- **Change the storage to `true`/`false` strings.** A bool is added, multiplied, summed, indexed and compared by every numeric path in both backends. Turning those into strings to fix a printing bug trades one wrong glyph for a hundred wrong answers.
- **Make the compiled backend print what the interpreter prints by giving each engine its own rule.** That is how the two backends got into positions where `print(a < b)` under a dunder is right on one leg and wrong on the other. The predicate has to be one function, in the shared package, called by both.
- **Let a name stay a verdict after a `for` rebinds it.** `flag = 1 == 1` / `for flag in [1, 2]: print(flag)` printed `True`, `False`. A `for` binds elements; the binding rule reads an expression, this one reads a name, and forgetting the name is the only answer that cannot be wrong for a reason the compiler cannot see.
- **Call every builtin that "sort of" returns a verdict a bool.** `isinstance` and `hasattr` are not implemented; `sorted([True, 0, 1])` returns a list whose elements keep their own kind; `bool(0)` is not implemented at all (and is left unfiled here rather than folded in, because it is a missing builtin with its own two-engine split, not a rendering rule). The set is `all` and `any`, and it grows when a builtin's argument list makes the question real.
- **Close the two remaining shapes by pinning them as parity, or by not writing tests for them.** They are tested, as they answer today, with the gap named in the failure text — Gap R.37's rule: a known wrong answer needs a failing check somewhere, not a comment.
