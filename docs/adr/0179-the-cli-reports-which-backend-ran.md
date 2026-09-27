# ADR 0179: the CLI reports which backend executed the program

## Status

Accepted (part 1 of Gap M.2).

## Context

`AGENTS.md` and `docs/language.md` describe the two execution paths as
interpreter = REPL/`--eval`/`--verify` and **LLVM AOT = `--file`/`--emit-llvm`**.
The code disagreed: `evalSrcOrFile` ran the AST evaluator for `--file` unless
`--jit` happened to be passed. Nothing in the output said which engine had run
the program, so "I ran it and it printed the right answer" was ambiguous — and
in practice it meant the interpreter.

That ambiguity was not academic. While fixing string literals (ADR 0178) every
manual probe was made through `gustyc --file`, and each one showed the correct
answer for a program whose *compiled* form was broken:

- `"é" in greeting` emitted `rt_contains(i32 @.str14, …)` — a global in an i32
  slot, rejected by `llc`;
- `def g(): return "hello"` emitted `ret i32 @.str1`;
- `print(d["k"])` printed `6`, the interned-table index, where the interpreter
  printed `value`.

All three were caught only by the conformance harness, which really does
`llc` + `cc`. The default CLI path had been hiding an entire class of bug.

## Decision

**Make the backend explicit in both directions — requested and reported.**

- `--aot` (alias of `--jit`, so the flag matches the documented vocabulary) and
  `--interp` select the engine explicitly. Asking for both is a usage error
  (exit 4) naming the contradiction; it is never resolved by precedence, because
  a silently-chosen backend is the bug being fixed.
- Every execution result carries `"backend": "interpreter" | "aot"` in its JSON
  payload — the `--eval`/`--file` result line, the runtime-error line, and the
  captured-output line the compiled path emits. An agent that asked for AOT can
  *see* that it got AOT.
- `--show-backend` prints `gustyc: backend <name>` on **stderr**, so the human
  answer exists without contaminating program stdout (ADR 0169: stdout is the
  program's).

**Not decided here**: flipping `--file`'s default to the compiled backend. That
would turn programs which today "work" (interpreted) into failures (AOT-unsupported
constructs), which is a migration, not a fix. The roadmap keeps it open as Gap
M.2's remaining step, gated on the conformance matrix being green through the
compiled path.

## Consequences

- `--json --file p.gy` now answers `{"result": …, "backend": "interpreter", …}`;
  `--json --aot --eval 'print(6*7)'` answers
  `{"output": "42\n", "backend": "aot", "exit": 0}`.
- The AOT path stops being the undocumented one: scripts and agents can pin the
  engine, and CI can run the same program through both and diff.
- `TestCLIJITJSON`'s exact-shape assertion was updated rather than loosened — the
  payload shape is a contract, so it changes in a test that says so.
- New CLI tests cover command-line *shapes* (ADR 0175's standing rule): `--aot`
  with `--json`, `--jit` as an alias, `--interp` explicit, contradictory flags →
  exit 4, and `--show-backend` staying off stdout.

## Alternatives rejected

- **Report the backend only to stderr** — rejected: humans see it, agents (the
  stated second audience) get nothing parseable.
- **Report it only when `--aot`/`--jit` is passed** — rejected: the whole point is
  that the default case was invisible; the interpreter is the case that needs
  labelling.
- **Let `--interp` win, or let the last flag win, on conflict** — rejected: a
  contradiction is a user mistake, and silently picking one side is exactly how
  "which backend ran this?" became unanswerable. Exit 4 with a sentence naming
  both flags.
- **Flip `--file` to AOT in this commit** — rejected as too big and unrelated to
  the reporting fix: it needs the AOT path to be green for the corpus first
  (Gap M.2 keeps it as a gated follow-up).
- **Rename `--jit` to `--aot` outright** — rejected: `--jit` is a documented
  stable flag; an alias adds the vocabulary without breaking anyone.

## References

- `cmd/gustyc/main.go` — `--aot`, `--interp`, `--show-backend`, `backend`,
  `evalSrcOrFile`
- `cmd/gustyc/main_test.go` — `TestCLIReportsWhichBackendRan`,
  `TestCLIShowBackendGoesToStderr`, `TestCLIAotAndInterpContradict`,
  `TestCLIExplicitInterpBackendRuns`, `TestCLIJITJSON`
- `docs/operations.md` — flag table and JSON payload shapes
- ADR 0169 (stdout is program output), ADR 0176 (CLI tests must cover shapes),
  ADR 0178 (the bugs this hides)
