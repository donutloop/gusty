# ADR 0281 — a function that renders its answer is a string-returning function, and its callers are told

Date: 2026-10-06
Status: Accepted
Roadmap: L11.2 (text and rendering), closing **Gap R.163**.
Depends on: ADR 0258 (`str()`/`repr()` are one rendering pair), ADR 0224 (a call's string answer is
known to its callers — `print(Dog().sound())`), ADR 0199/Gap R.6 (a program that takes a builtin's
name owns that name), ADR 0166 (exit 2 is the compiler's bug, not the program's).

## The measure

`python3` 3.12.3 is the oracle; `--interp` and `--aot` are the two engines. Measured at the CLI.

| program | CPython | `--interp` | `--aot` before | `--aot` after |
| --- | --- | --- | --- | --- |
| `def g(): return str(42)` → `print(g())` | `42` | `42` | **`0` at exit 0** | `42` |
| `x = 7` / `def g(): return str(x)` | `7` | `7` | **`0`** | `7` |
| `def g(): return str(2.5)` | `2.5` | `2.5` | **`0`** | `2.5` |
| `def g(): return repr(42)` | `42` | `42` | **`0`** | `42` |
| `def g(): return str([1, 2])` | `[1, 2]` | `[1, 2]` | **`0`** | `[1, 2]` |
| `def g(): return str(None)` | `None` | `None` | **`0`** | `None` |
| `s = g()` / `print(s)` / `len(g())` / `g().upper()` | `42`,`2`,`42` | same | **`0`**, exit 1 at `len` | `42`,`2`,`42` |
| `def str(x): return x + 7` / `def g(): return str(42)` | `49` | `49` | `49` | `49` |
| `print(str(42))` (control, no function) | `42` | `42` | `42` | `42` |

Every wrong answer was the **same** wrong answer: `0`. That number is not a rendering of anything —
it is the *index* of the interned string, handed to `printf` with `%d`.

## The diagnosis

The body was never wrong. Emitted for `def g(): return str(42)` the callee is:

```llvm
define i32 @gy_g() {
  %t1 = call i32 @rt_str_intern2(i8* @.str1, i8* @.str2)   ; "42" and its repr, interned
  ret i32 %t1                                              ; the @str_tab index — correct
}
```

and the caller was:

```llvm
  %t5 = call i32 @gy_g()
  %t8 = call i32 (i8*, ...) @printf(i8* @.fmt1, i32 %t5)   ; %d on an index: prints 0
```

A text in this backend is an index into `@str_tab`, and the print dispatch chooses the text path by
asking `callReturnsStr`, which reads one table: `strFuncs`, filled by `strReturningFuncs`. That
predicate knew a literal return (`return "hi"`), an f-string, a string parameter, a concat, and a
call to an already-known string-returning function — but not `return str(x)`. So the callee emitted
the correct rendering through ADR 0258's door while the caller printed its index as a number. The
two halves agreed on nothing, and the disagreement surfaced as a plausible digit at exit 0.

This is ADR 0224's bug class exactly — `print(Dog().sound())` printed `0` until the method's verdict
was registered — reappearing one door earlier, because `strReturningFuncs` grew its cases one
spelling at a time (`StrLit`, `FString`, param, concat, known callee) and `str(…)` was never added
to the list.

## The decision

**A `return str(…)` / `return repr(…)` makes a function string-returning**, in the same program-wide
predicate that decides what every call site prints:

```go
case *Call:
    if nm, ok := v.Fn.(*Name); ok {
        if !defines[nm.Value] && (nm.Value == "str" || nm.Value == "repr") && len(v.Args) == 1 {
            return true
        }
        return out[nm.Value]
    }
```

Three details carry the weight:

* **`!defines[nm.Value]`** — Gap R.6's rule, ADR 0199's shape: a program that defines `str` itself
  gets its own function. `def str(x): return x + 7` beside `def g(): return str(42)` answers `49`,
  a number, and its row is in the test table so this predicate cannot silently re-adopt the builtin's
  meaning.
* **`len(v.Args) == 1`** — the rendering door serves exactly one argument; a call the door would
  refuse must not be classified as a text either. A wrong *classification* is as expensive as a wrong
  number: it moves the value onto the text path.
* **The fixed point already existed.** `strReturningFuncs` rounds until stable, so
  `def a(): return str(1)` / `def b(): return a()` is marked by the existing callee-of-known-callee
  rule. No second pass, no new state.

The answer is a text in the *value* positions too, not just under `print` — `s = g()`, `len(g())`,
`g().upper()`, `"joined: " + g()` — and each of those is a different consumer of the same index, so
the probe program exercises all four.

## Alternatives rejected

* **Intercept at the print dispatch** (`if the callee emitted a rendering, print it as text`).
  Rejected: the print dispatch's whole design is that it asks one table, `callReturnsStr`, and a
  second source of truth about a value's kind is how `print` and `str()` came to disagree about a
  container in the first place (ADR 0258's reason for existing).
* **Make the callee return the string's address instead of its index.** Rejected: ADR 0224 already
  removed `ret i32 @.strN` from this road precisely because a global in an `i32` slot made `llc`
  reject the module — exit 2 on an ordinary program.
* **Widen the rule to any call in a string-returning-looking position** (e.g. `return f(x)` where `f`
  is unknown). Rejected: an unknown callee is exactly what the predicate must *not* guess, and the
  rounds already answer the case where the callee is knowable.
* **Have `print` special-case the constant `0`.** Rejected on sight: `0` is what a missing kind looks
  like here, and other shapes print a real zero.

## Agentic rationale

The failure mode this ADR removes is the worst one for an agent consuming the compiler: **exit 0 with
a plausible number**. `print(g())` → `0` is indistinguishable, by exit code alone, from a program
that legitimately prints zero. After the change the classification is decidable by a single
`--emit-llvm` — a caller that reaches `rt_str_ptr` prints text, one that reaches `printf` with `%d`
does not — so a consuming agent can verify the rendering path without running the program, and the
IR-shape assertion in `pkg/lang/string_value_test.go` fails on any regression rather than on a
mismatched digit that a human would have to notice.

## Codegen / IR notes

* No new runtime, no new global. The emitted callee is byte-identical to before; the change is in
  the caller's dispatch, visible as `rt_str_ptr` where `%d` used to sit.
* `strReturningFuncs` stays a pure program-wide pre-pass (six rounds to a fixed point), so the
  arity/representation decisions remain independent of file order — the same rule ADR 0273 established
  for the pair scan.

## Tests

* `pkg/lang/string_value_test.go` — `TestStrResultOfAFuncIsKnownToItsCallers`: nine rows (literal,
  module name, double, `repr`, container, `None`, the bound/measured/upcased family, the shadowing
  control, and a number-returning control), each asserting the interpreter's stdout, the compiled
  stdout, a verifying module, and the IR-shape clause that the print reached the text lookup rather
  than `printf`'s `%d`.
* `integration/string_value_test.go` — the same family through the CLI on **both** engines, pinned to
  bytes measured against `python3`, beside ADR 0224's method rows so the two doors cannot drift apart.
* `integration/programs/probe_return_str.gy` — ten lines, three legs, one answer; registered in
  `conformanceStandalone()`. The matrix moved 149 → 150 rows, 113 → 114 parity, 98 → 99 oracle
  `match`, 0 fail, 0 drift, and the whole-corpus sweep (166 files, both engines, against the
  pre-cycle binary) moved **only** this probe.
