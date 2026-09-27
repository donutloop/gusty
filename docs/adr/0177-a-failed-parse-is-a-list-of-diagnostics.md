# ADR 0177: a failed parse is a list of diagnostics, not a Go error

## Status

Accepted.

## Context

L4.1 promised "on an unexpected character, emit a `TokError` token carrying the
span + message and *resume* … so the parser/semantic can report **multiple
diagnostics per run**". The lexer and parser had been built out to do exactly
that — recovery, a `ParseErrors` forest, `TokError` tokens with rich spans (L4.2)
— and it was still not visible to a user, because the last hop dropped it:

```console
$ gustyc check broken.gy
gustyc: check: parse broken.gy: 2 parse errors
	parse error at 2:6: unexpected token
	parse error at 3:5: unexpected token
$ gustyc --json check broken.gy        # ← no JSON at all: the same prose
```

`CheckSource`/`CheckFile` did `if err != nil { return nil, fmt.Errorf(...) }`, so
a parse failure (a) threw away `prog.Diags` — the precise lexer diagnostics such
as `unexpected character "$"` at `2:5` — (b) collapsed the forest into one
`Error()` string, (c) skipped `Analyze`, so type errors in the statements that
*had* parsed were never reported, and (d) escaped as a CLI usage error, which
left `--json check` printing prose and breaking the machine contract.

That is the "fix one error, get told the next one" loop this project exists to
end, in the compiler's own face.

## Decision

**A parse failure is data, and all of it travels the diagnostic channel.**

- `checkParseErrors(err)` converts a parse failure (unwrapped with `errors.As`,
  so a wrapped `*ParseErrors` still yields its forest) into one `Diagnostic` per
  error, each carrying its span and the new stable code **`parse.error`**.
- `CheckSource`/`CheckFile` no longer return a Go error for a parse failure;
  they return a normal `CheckResult` with `ok:false` and the standard `exit:1`,
  so the human and JSON paths are the same code path with the same shape.
- `Analyze(prog)` runs **anyway**. Recovery only ever yields complete
  statements, so the code below a broken line is still checkable, and its type
  errors are reported in the same run.
- Lexer-recovered diagnostics come from `Analyze` (which already merges
  `prog.Diags`); the parse-error converter deliberately does *not* add them, or
  the same squiggle appears twice — a duplicate diagnostic is a bug users see.
- `buildResult` sorts by line, column, message. Parse errors and semantic
  errors come from different collectors; document order is what a human reads
  and what makes JSON output diffable.
- `filterLex` stamps lexer error tokens with `parse.error` too, so agents branch
  on a code rather than message prose. `Analyze(nil)` is now safe.

## Consequences

- One `gustyc check` run on a file with two bad lines and one type error below
  them reports **all three**, in document order, each with a span, in both
  shapes:

  ```console
  error at 2:5: unexpected character "$"
  error at 2:6: unexpected token
  error at 3:5: unexpected token
  ```

  ```json
  {"files":["broken.gy"],"diagnostics":[
    {"level":"error","span":{"line":2,"col":5},"msg":"unexpected character \"$\"","code":"parse.error"},
    {"level":"error","span":{"line":2,"col":6},"msg":"unexpected token","code":"parse.error"},
    {"level":"error","span":{"line":3,"col":5},"msg":"unexpected token","code":"parse.error"}
  ],"ok":false,"exit":1}
  ```
- **The LSP inherits the fix** — it feeds off the same `Program.Diags` and
  `ParseErrors`, so hover/squiggle quality improves with no separate work.
- **`parse.error` is a contract**: it joins the table in `docs/operations.md`,
  and an agent can rely on it to distinguish "did not parse" from a semantic
  failure without string-matching.
- **Exit codes are unchanged** (still `1` for a failing check) — this is about
  content, not about codes, so no other CLI contract moved.

## Alternatives rejected

- **Return the first parse error only** — rejected: exactly the sequential loop
  recovery was built to eliminate, and it hides the second and later errors from
  both backends' users.
- **Return a Go error but attach the list** — rejected: the CLI then needs two
  code paths for one concept, which is how `--json check` ended up printing
  prose in the first place.
- **Skip `Analyze` when the parse failed** — rejected: the safest-looking choice
  and the worst UX, because the type errors below the broken line reappear one
  per keystroke. It is also unnecessary: recovery yields only complete
  statements, and `Analyze` tolerates a nil program.
- **Duplicate lexer diagnostics in the parse-error converter** — rejected:
  `Analyze` already merges `prog.Diags`; doing it twice shows the same error
  twice, and users stop trusting the tool.

## References

- `pkg/lang/check.go` — `checkParseErrors`, `CheckSource`, `CheckFile`,
  `buildResult`, `CodeParseError`
- `pkg/lang/incremental.go` — `filterLex` (lexer diagnostics get the code)
- `pkg/lang/semantic.go` — `Analyze` nil guard
- `pkg/lang/check_test.go` — `TestCheckSourceParseError`,
  `TestCheckSourceReportsEveryRecoveredError`,
  `TestCheckSourceChecksWhatParsed`
- `docs/operations.md` — the code table and the `--check` behaviour
- ADR 0166 (unsupported lowering is a diagnostic), ADR 0169 (runtime failures
  report and fail) — the same principle applied to the front end
- ADR 0176 (incremental parsing) — the previous cycle in this loop
