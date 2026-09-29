# 0204. A program's stdout is only what the program printed

## Status

Accepted. Implemented this cycle in `cmd/gustyc/main.go` (`evalSrcOrFile`); pinned by
`cmd/gustyc/echo_mode_test.go`. Roadmap Gap R.13.

## Context

```gusty
def f(x):
    return x * 2

f(5)
```

`gustyc --file prog.gy` printed `10`. `gustyc --aot prog.gy` printed nothing, and CPython printed
nothing — the compiled backend had never echoed anything, so the same source had two different
stdouts depending on which engine ran it.

The echo itself is not new and not unwanted: it is what makes `--eval "x = 1 + 2\nx"` print `3`, the
same courtesy as any REPL, and an earlier cycle had already narrowed it after it was found appending
a stray `0` to every program's output (the void value `print()` returned). What survived that
narrowing was still the wrong predicate — *"is the final statement a bare expression with a non-None
value?"* — when the question that matters is *"did this source arrive as a snippet, or as a file?"*.
A program that ends on a value-returning call is common (`main()` last, a benchmark call, a
`render()`), so the corrupted-stdout case was not exotic: it affects piped output, which is exactly
the path agents and scripts use.

The code comment described the intent more precisely than the code did — "a REPL courtesy for
*snippets*, not a program feature … matching `python prog.py`" — and the mismatch between the two is
the defect. When a comment states a rule the guard does not implement, file it as a bug rather than
trusting the comment.

## Decision

**The echo is keyed on provenance, not on shape.** `evalSrcOrFile` knows which it got (`--eval`
supplies source text; `--file`/positional supplies a path), so:

1. **A file echoes nothing.** `--file`, the default interpreted run, and `--interp` all produce
   exactly the program's own `print` output — identical to `--aot` and to CPython. Nothing is appended
   to piped stdout, ever, regardless of what the last statement is.
2. **A snippet keeps the courtesy.** `--eval` still echoes a final bare expression (`3` for
   `x = 1 + 2\nx`), because a prompt that swallows the value you asked for is a worse user experience
   than the bug being fixed. The REPL has its own echo path and is unchanged.
3. **`--json` still reports `result` for a file.** Suppressing the echo is about the *program's*
   stdout, not about withholding information: an agent reading structured output gets the evaluated
   value and its type as metadata, labelled as what it is (`result`/`type`/`backend`/`exit`), while the
   human path prints nothing. The asymmetry is deliberate and is now pinned by a test, so nobody
   "fixes" it by deleting the field or by re-adding the print.
4. **The two backends are asserted against each other, not just against a wish.** The tests compare
   `--file`/`--interp` output to `--aot` output for the same source, because "no stray echo" is only
   meaningful relative to what the other engine does.

## Consequences

- Program stdout is now reproducible across backends and matches `python prog.py`, which makes the
  corpus's three-engine parity checks usable for any program shape, including ones ending in a call.
- `docs/operations.md` states the rule per mode, so an agent can predict stdout without reading the
  implementation, and knows that `result` in JSON is not program output.
- The old narrowing (bare final expression, non-`None`) survives inside snippet mode, so
  `--eval "print(7)"` still prints exactly `7` and nothing else — asserted rather than assumed.
- Related to R.12/R.6 in that all three are about the interpreter and codegen disagreeing on the
  meaning of the same source; this one was in the CLI wrapper rather than in the evaluator, which is
  why it took three backends printing to different things to make someone compare them.

## Alternatives considered

- **Remove the echo entirely.** Rejected: `--eval` exists to answer "what is the value of this?", and
  a one-line evaluation tool that prints nothing is not that. Snippet versus file is the real
  distinction.
- **Echo only when stdout is a TTY.** Rejected: it would make program output depend on whether the
  caller piped it, which is precisely how "works on my terminal, wrong in CI" bugs are manufactured —
  and this CLI's whole agent story assumes piped stdout is first-class.
- **Echo for files too, in the compiled backend, for symmetry.** Rejected: `python prog.py` prints
  nothing, the AOT path already prints nothing, and adding an echo would corrupt piped output for the
  most automated path there is.
- **Keep `result` out of JSON for files as well.** Rejected: the field describes the evaluation, and
  dropping it would cost an agent information in order to make a superficial symmetry with a stdout
  rule that is about something else entirely.
