# 0165. `print` is `print(*args, sep=" ", end="\n")`

Status: Accepted
Date: 2026-09-27
Audience: language users, compiler maintainers, agents generating programs

## Context

`print` was implemented as "for each argument, write its rendering followed by a
newline" — in the interpreter (`fmt.Println(e.Repr(v))`) and in the AOT backend (one
`printf` per argument, each format ending in `\n`). Both backends agreed, so parity,
conformance and every benchmark stayed green while the most-used builtin in the
language behaved nothing like Python:

```python
print("n =", 42)     # emitted "n =" and "42" on two lines
print()              # emitted nothing at all
```

The corpus could not see it: its programs mix a string and a value inside f-strings
(`f"n = {n}"`), rarely as separate arguments, and the harness compares the two
backends against *each other* rather than against Python.

Two consequences made this worth fixing properly rather than cosmetically:

- A builtin that disagrees with Python on its most visible behaviour undermines the
  "familiar syntax, familiar semantics" promise of the language.
- The newline was baked into each argument's `printf` format, so there was no place
  to put a separator or a custom terminator at all.

## Decision

`print` implements Python's signature semantics in both backends:

```
print(*args, sep=" ", end="\n")
```

- Arguments render exactly as `str()`/`repr()` renders them (interpreter `Repr`,
  AOT per-kind renderer) and are joined with `sep`; `end` is written once, after the
  last argument. `print()` therefore writes a blank line, and `print(x, end="")`
  leaves the line open.
- **No argument format carries a newline.** Scalar arguments use bare `%d` / `%g` /
  `%s` formats; the runtime container printers take the newline as an explicit flag
  (`rt_print_list(i32 %h, i32 %nl)`, same for dict/set) and branch on it. The
  terminator is emitted once by the call, which is what keeps `end=""` honest for
  every argument kind, including containers.
- Arguments are written **as they are evaluated, separator first**, in both backends,
  so `print("got", f())` where `f` prints interleaves identically. The interpreter
  resolves `sep`/`end` before writing anything, so keyword order does not matter.
- A `%` in `sep`/`end` is literal text (escaped to `%%` for `printf`), not a directive.
- `sep`/`end` must be keyword arguments; anything else is an error naming the argument
  (`print got an unexpected keyword argument "junk"`). In AOT they must be compile-time
  string constants and say so
  (`print's sep must be a compile-time string constant (the interpreter accepts any
  expression)`) instead of emitting IR that only LLVM's verifier would reject.
- Sets now render as `{1, 2}` (`set()` when empty) in the interpreter, matching
  `rt_set_print` — previously `<set>`.

## Rationale (agentic)

An agent writing gusty code from Python intuition gets `print("n =", n)`; if that
produces two lines, every downstream tool (expected-output diffing, log parsing,
doctest-style evals) inherits a silent mismatch, and the agent has no signal that the
compiler — not its program — is unusual. Machine consumers get one more stable
surface: `--emit-llvm` output for print is now predictable and inspectable
(value, separator, value, terminator), and the golden-regeneration path
(`go test ./integration -run TestCLIBuild -args -update`) means a deliberate
semantics change is a reviewable diff instead of a hand-edited expectation.

## Codegen / IR implications

- `print(1, 2)` lowers to four `printf`s: `%d`, `" "`, `%d`, `"\n"` — asserted in
  `TestIRMultiArgPrintCompilesWithLLC`, which also pins that no argument format
  contains `\0A`.
- `print()` is a single terminator `printf` (`TestIRZeroArgPrintCompilesWithLLC`).
- `rt_print_list` / `rt_dict_print` / `rt_set_print` gained an `i32 %nl` parameter and
  a conditional trailing newline; every call site passes the flag explicitly, so
  nothing can sneak a newline back in.
- Goldens under `integration/expected/` (ctrl/data/features/math) and `ir.ll` were
  regenerated; `programs/print_args.gy` joins the conformance corpus, so the print
  shape is now parity-checked on both backends instead of being defined by
  implementation accident.

## Alternatives rejected

- **Leave it: parity is preserved** — parity between two backends that are both wrong
  is not correctness; the corpus could not see the difference because it never
  compared against Python semantics.
- **Join in the interpreter only, keep one-printf-per-line in AOT** — recreates the
  divergence class that ADR 0161/0163 spent this session eliminating.
- **Buffer each line and join in the runtime** — needs a string type and a buffer in
  the runtime for a behaviour change that only needs a format-string flag; it would
  also lose the argument-evaluation interleaving that both backends currently share.
- **`print(args, sep)` positionally** — Python spells these as keywords; accepting
  positional separators would break the moment a program prints a tuple-like argument.

## Consequences

- `docs/language.md` documents the signature; README advertises it; the roadmap closes
  Gap J.1 and re-scopes Gap J.2 (the interpreter's `<set>` rendering went with this
  change).
- Programs that *wanted* one-value-per-line must now say `print(x, end="\n")` or
  print separately — a deliberate break, chosen because Python compatibility is the
  contract this language makes.
- Discovered while writing the corpus: string arguments to user functions emit
  unverifiable IR in AOT (Gap J.5, same root cause as Gap I.2).
