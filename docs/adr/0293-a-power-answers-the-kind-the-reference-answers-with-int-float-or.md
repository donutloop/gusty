# `x ** y` answers the kind the reference answers with — int, float, raise, or a refusal

## Decision

`**` gets **one** answer-kind rule, asked once, in one file (`pkg/lang/power_kind.go`), and the four roads
that need it (the print formatter, the double road, the i32 road, the constant fold) all call it:

| operands | the reference answers | both engines now |
|---|---|---|
| `int ** int`, exponent ≥ 0 | int | `2 ** 3` → `8` |
| `int ** int`, exponent < 0 | **float** | `2 ** -1` → `0.5` |
| either side float | float | `4 ** 0.5` → `2.0`, `2.0 ** 10` → `1024.0` |
| base 0, exponent < 0 | `ZeroDivisionError` | raised at **run time**, one sentence for both spellings |
| negative base, fractional exponent | a **complex** number | refused in words — the language has no complex value |
| result past the machine word | an arbitrary-precision int | refused in words, never a wrapped number |

The rule is **not** "either side is written with a dot". `2 ** -1` is a float with two int literals, and
that single row was worth the cycle.

## The agentic rationale

Every shape above was, before this change, a **wrong number at exit 0** — the class an agent reading
`--aot`'s exit code cannot distinguish from success:

```
print(2 ** -1)    reference 0.5      both engines 0            # the int road returned 0 for a negative exponent
print(4 ** 0.5)   reference 2.0      compiled 1                # llvm.pow.f64's double truncated by fptosi
print(2.0 ** 10)  reference 1024.0   compiled 1024             # the double printed through %d
print(0 ** -1)    reference raises   compiled inf → 2147483647 # pow's own answer, truncated
print(2 ** 100)   reference 31-digit int   both engines 0      # the multiply wrapped in silence
```

Both backends agreed with each other on every one of them, so the two-backend parity matrix had nothing to
say; the oracle leg — running CPython on the same source — is the only instrument that found them. That
asymmetry is the argument for keeping the oracle leg in CI, and it is why `--emit-ast`/`--emit-llvm` on a
power now show a shape that agrees with what runs.

## Codegen / IR implications

- **The root cause was one missing token.** `isFloat`'s `*BinOp` arm listed `"+", "-", "*", "%", "//"` and
  omitted `"**"`, so nothing told `print` that a power answers a double. `llvm.pow.f64` computed the right
  answer and the caller pushed it through `fptosi` into `%d`. A one-word omission, two wrong answers per
  backend, and no test could see it.
- **The rule is a function, not a switch in each road.** ADR 0279/0280's one-question rule again: the print
  formatter and the arithmetic disagreeing is exactly how `print(4 ** 0.5)` could print `1` while the
  arithmetic underneath it was correct.
- **The zero-base raise is emitted at RUN TIME**, as two `fcmp`s and an `and` ahead of the `pow` call
  (`branchRaise`, the same shape the `/` guards use). A compile-time claim can only see a literal base, and
  `z = 0` / `print(z ** -1)` is the same program to the reference. It also cannot be emitted *inside*
  `floatBinOp`'s pre-pass: an early `return` there left the function frame's `ret double %t3` naming a
  temporary that had been allocated but never written — `use of undefined value '%t3'`, llc rejecting our
  module, which ADR 0166 counts as exit 2. Guard placement is a codegen constraint, not a style choice.
- **A guard's continuation block belongs to the same builder.** An early draft swapped in a fresh
  `strings.Builder` after `branchRaise`, which swallowed the `pow` call and produced the same undefined-
  `%t3` module. `branchRaise` returns the block the continuation runs in; writing to `b` afterwards is
  what the `//` and `/` guards beside it do.
- **The overflow refusal measures the word the backend actually has.** The compiled `int` is an `i32`
  (Gap R.133, owner L12.12); the interpreter's is 64-bit. Measuring int64 refused `2 ** 100` while letting
  `2 ** 32` print `0` and `2 ** 31` print `-2147483648` — a negative answer to a positive exponent. The
  constant fold declines rather than fold a wrapped value, so the road below can name it.
- **A folded literal and a name must get the same answer**, so the predicates take a reader hook
  (`installPowerReaders`) into the generator's module records. A nil hook is a segfault in the middle of a
  diagnostic — every read goes through `readIntLiteral`, which declines instead of guessing.

## Alternatives rejected

- **Answer `0` for a negative int exponent** (what shipped). Rejected: Python has no such operation; the
  comment defending it claimed it "mirrored Python's `int ** int`".
- **Print doubles through `%d` after truncating them.** Rejected: `1024` where the reference prints
  `1024.0` is a wrong number, and `4 ** 0.5` → `1` is a wrong number with a plausible face.
- **Answer `inf`/`nan` for `0 ** -1` and `(-8) ** (1/3)`.** `llvm.pow.f64` and `math.Pow` both answer a
  float for these; the reference raises and returns a complex number respectively. `inf` truncated to
  `2147483647`; `nan` printed at exit 0. Rejected — a raise must be a raise, and a value the language has
  no name for must be a refusal.
- **Refuse every power whose operands are not both literal ints.** Rejected after measuring it: it turns
  `2 ** 3 ** 2` → `512` into `512.0` and `(2 and 3) ** 2` → `9` into `9.0`. The first draft of this rule
  did exactly that, caught by two existing tests and one new one. An answer may not become a wrong number.
- **Fix the interpreter's one-ULP `math.Pow` disagreement with a private refinement** (exact integer powers
  × `math.Sqrt`). Tried and removed: it fixed `2 ** 1.5` and broke 21 rows of a 110-point grid on different
  lines than it fixed. A "fix" that changes *which* rows are wrong is not a fix; filed as Gap R.178 for
  L11.6, with a test that logs when one side is closed without the other.
- **Silently wrap past the word.** Rejected: a bounded integer is this language's defensible 2026 design;
  a *silently wrapping* one is not, and the wrap is invisible to parity because both backends wrap alike.
  The refusal quotes the true value so the author can act on it.

## Related

`Gap R.133` (the compiled i32 word), `Gap R.176` (this row), `Gap R.177` (unary `-` binds tighter than
`**`, found while measuring it, filed to the parser), `Gap R.178` (the one-ULP `math.Pow`/libm gap),
ADR 0279/0280 (one question per expression), ADR 0166 (exit codes), ADR 0215 (a raise is the reference's
sentence).
