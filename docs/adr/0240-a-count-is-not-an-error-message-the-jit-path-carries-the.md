# A count is not an error message: the `jit:` path carries the checker's own sentences

Status: accepted. Records an interface fix found while measuring roadmap **L11.1 step 2** (ADR 0239):
a `;`-separated line ran on the interpreter and was refused by the compiled path, and the refusal
said only `jit: 1 error(s) in source`. Cites: ADR 0166 (a refusal, never an invalid module — and a
refusal nobody can read is a refusal nobody can act on), ADR 0233 (implement, refuse, or be absent —
never answer wrong, and never answer *vaguely*), ADR 0006 (the exit-code contract: exit 1 is "the
program is refused", which only means something if the text says what was refused), ADR 0224 (the
JSON diagnostics schema is the machine surface; the prose and the JSON must tell one story).

## What had been

`JITWithOptions` parsed, analysed, and — if anything came back at error level — returned:

```
gustyc: jit: 1 error(s) in source
```

The checker had produced `error at 1:6: unexpected character ";"` and the `Diagnostic` list on the
result still carried it, so the information existed twice: once in the machine surface, once nowhere.
A reader at the terminal had a count. Measured on the same line, three surfaces said three things:

| surface | `x = 5; print(x+1)` |
|---|---|
| `--interp --file`, `--eval`, `--repl` | `6` — the CPython answer |
| `--jit` / `--aot` | exit 1, `jit: 1 error(s) in source` |
| `--emit-llvm` | exit 0, a module that verifies under `llc -filetype=null` |

The disagreement itself is Gap R.72 (the lexer has no `;` and the entry points disagree about what to
do with the diagnostic it raises). This ADR is about the second row: whatever the verdict, it has to
be *readable*. Before the fix, telling the two rows apart took reading the compiler's source; after
it, one line does:

```
gustyc: jit: 1 error(s) in source
  - error at 1:6: unexpected character ";"
```

## The decision

**The error text carries every error-level diagnostic, in source order, in the same rendering the
CLI already uses for stderr (`Diagnostic.Error`, which is `level at line:col: msg`).** The count
stays — it is part of the existing message and scripts may match it — and the messages ride under it,
prefixed `- ` so a reader can see they are a list and an agent can split them on a stable boundary.

- **One renderer, not two.** The lines come from `Diagnostic.Error()`, the method the CLI prints with.
  A second formatter is a second opinion, and the two drift: the whole reason this was found is that
  `--json` already had the sentence while `--file` did not.
- **All of them, not the first.** A program with two bad statements gets two sentences. Reporting one
  and counting N invites the reader to fix the visible one and re-run in a loop; the fix-everything-in-
  one-pass workflow needs the whole list, which is also what `Analyze` already produces.
- **Span first.** `1:6` is what makes the sentence actionable from a file position alone, and it is the
  part that was thrown away with the count.

## Agentic rationale

An agent comparing backends (`--interp` says `6`, `--jit` says exit 1) is doing a differential debug
and needs the *reason* in the same channel as the verdict, because that channel is what it reads: the
error string, not the `Diagnostics` array of a result object it may not have kept. The rule generalises
the exit-code contract rather than adding to it: exit 1 promises "refused, and here is why", so the
text is part of the promise. Nothing else changed — no new flag, no new code, no new exit code; the
`--json` path keeps emitting the structured form (`{"level","span","msg","code","suggestion"}`) and
the human path stops withholding what the machine path already had.

## Codegen and IR implications

None. This is the reporting layer above `GenerateIR`; the emitted module for any accepted program is
byte-identical, and the verifier's output (the `llc`/`opt` text, exit 2) is untouched.

## Alternatives rejected

- **Print only the first diagnostic.** Cheaper to type, and it makes every multi-error program a
  two-round trip. `Analyze` does not stop at the first error, so the extra information is already paid for.
- **A new `error()`-typed wrapper struct instead of a string.** The JIT API's contract is `(*JITResult,
  error)` and its callers (the CLI, `--bench`, tests) print the error; a richer type would need every
  caller to change to get back what a string already carries.
- **Point the reader at `--json` instead.** Rejected by the observed workflow: the reader at a terminal
  is not going to re-run the same refusal through a different flag to find out what it said. The JSON
  is the machine surface, not a paywall for the human one.
- **Fold the fix into Gap R.72 (the `;` gap).** Rejected: the lexer decision may go either way (`;`
  accepted, or refused everywhere including the interpreter), and the readability of a refusal must not
  depend on which. Two rows, two decisions.
