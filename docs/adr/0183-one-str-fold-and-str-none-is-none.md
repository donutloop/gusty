# ADR 0183: `str()` has one fold, and `str(None)` is "None"

Status: Accepted (Gap L.2 partial; feeds L11.1/L11.2)
Date: 2026-08-04
Decides: the compile-time text of `str(x)` for a foldable `x`, and the rule that a folded
string may be *printed* as a global but never *stored* as one.

## Context

`print(str(None))` printed `0`. So did `s = str(None); print(s)` — and the second form did
not even compile:

```
llc-20: error: global variable reference must have pointer type
  store i32 @.str1, i32* %_s
```

CPython, and our own interpreter, say `None` ✓. The cause was that `str()` of a foldable
argument was decided in **three** places, each with its own case list:

| site | role | knew about |
|---|---|---|
| `stringConst` | AST-level folder (`len(str(42))`, concatenation) | int literals only |
| `irGen.stringVal` | what print/len/concat consult | int literals, float constants |
| `case "str"` in the builtin lowering | what `print(str(x))` lowers through | int literals, float constants |

None was in none of them, so `str(None)` fell through to the arithmetic path — `value(None)`
is the int `0` (ADR 0172's rule for scalar positions ✓) — producing the text `"0"`. And
because the three disagreed about *whether the result is a string*, the assignment path took
the "store the folded value" route while the value was a string **global**, which is exactly
the constant-in-value-position shape ADR 0167 and ADR 0168 kept hitting: valid to pass to
`printf("%s", i8* @.str1)`, invalid to store into an `i32` slot.

## Decision

1. **One text per form, enforced by a test.** `stringConst`, `irGen.stringVal` and the
   builtin's lowering fold `str(x)` to the same text for every `x` they can see: int literal
   → decimal, `FloatLit` → `pyFloatRepr`, `NoneLit` → `"None"`, `StrLit` → the string itself.
   `TestStrFoldAgreesBetweenTheTwoFolders` pins the pair of folders against the interpreter's
   own `str()`, and `TestStrFormsMatchCPythonOnBothBackends` pins interpreter, compiled
   output and CPython on the same rows. Verified load-bearing: folding `str(None)` back to
   `"0"` fails both.
2. **A folded string prints as a global; it must never be stored as one.** Printing goes
   through `printf`/`rt_print_str` with an `i8*` ✓. Where a folded `str(...)` result is
   *stored*, the text is interned (`rt_str_intern2`) and the handle stored, as string
   function arguments already do (ADR 0167/0168). The IR-shape guard is blunt and
   module-wide: no line may contain `i32 @.` (a global in an `i32` position) or
   `store i32 @`.
3. **`str()` is `str()`, not `repr()`, for these forms.** `str("x")` is `x`; the quoted form
   belongs to container rendering. Full `str`/`repr` separation (quoted elements inside
   lists, `set()` for the empty set, `True`/`False`) stays in L11.2, gated on bools becoming
   values in L11.1 — `--json` still reports `"type": "int"` for `True`.

## What the fix looked like, and what it did not

The correct change was three small case-list additions. Two larger attempts were tried and
reverted, and both are worth recording:

- **Interning inside the builtin lowering** (`case "str"` emitting `rt_str_intern2` for every
  fold). Broke `w = str(42); print(len(w))` ("len of a non-string variable") and
  `"x" + s` (refused as runtime concatenation), because those paths key off the *folded text*
  and interning erased it.
- **Making `stringVal` delegate to `stringConst`.** Looked like the right deduplication and
  broke the same two programs: `stringConst` folds forms (`StrLit`, concatenations) that the
  string-value folder deliberately does not, so print/len started treating values as folded
  constants they must not. The case lists must *agree on the forms they share*, not be
  identical.

## Consequences

- `print(str(None))`, `s = str(None)`, `print("v=" + str(None))`, `str("x")`,
  `len(str(None))` and `str(1.5)` all give CPython's answer on both backends.
- `str(True)` still yields `1`. That is not an oversight: bools are integers in both backends'
  value model, so there is nothing to render. It is L11.1's step (2), and this ADR deliberately
  does not fake it at the printer — a `str(True)` that says `True` while `print(True)` says `1`
  would be a worse inconsistency than the current, honest one.
- The `i32 @.` guard is now a standing check for these programs. Extending it module-wide (the
  whole corpus) is part of L11.1's DoD, where per-element tags retire the rest of the family.

## Alternatives rejected

- **Print-dispatch-only patch** (`print(str(None))` → special-case the text in print): leaves
  `s = str(None)` emitting invalid IR and the folders still disagreeing.
- **Make `None` a non-zero sentinel value** so `value(None)` stops returning `0`: ADR 0172
  settled representation of `None` (a singleton heap object, `0` only in scalar positions);
  changing it is L11.1's tagged word, not a `str()` fix.
- **Refuse to compile `str(None)`** in AOT, per ADR 0166's "say which backend supports it":
  rejected because the interpreter and CPython agree on the answer and the fold is trivially
  available — refusing would hide a real capability behind a bookkeeping bug.
- **A single shared folder function for all three sites**: see "What the fix looked like" — it
  regressed two programs that depend on the folders *differing*.
