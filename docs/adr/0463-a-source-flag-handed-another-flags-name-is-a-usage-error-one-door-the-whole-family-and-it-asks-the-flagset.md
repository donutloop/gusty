# ADR 0463 — a source flag handed another flag's name is a **usage error**: one door, the whole family, and it asks the FlagSet rather than a leading dash

**Status:** Accepted
**Date:** 2026-10-29
**Decides:** roadmap **Gap R.549**, the guard `--verify-llvm` and `--debug-info` each carried privately, and
the reach of `reorderFlags`'s token-ownership rule
**Filed:** **Gap R.550** (a source flag handed a path on disk measures the path's *name*, and only
`--emit-source-map` honours the path-or-text convention the CLI documents)
**Keeps open:** **Gap R.546**, **Gap R.547**, **Gap R.548**, and every residual **ADR 0461** left standing
**Related:** ADR 0004, ADR 0006, ADR 0166, ADR 0337, ADR 0346, ADR 0379, ADR 0302 (a flag whose meaning has
gone is refused rather than ignored — the same honesty rule, applied to a flag's *value*), ADR 0461 (whose
machine path is where this was measured)

## The rule

**A tool that answers a question about a program the caller did not name has not answered a neutral question —
it has answered the wrong one at full confidence. When a value that can only be a command-line token arrives
where a program was expected, the tool refuses and names the fix; it does not compile the token.**

## Context

The command line that filed the row is what any agent reaches for, because every other input mode of this CLI
is a path:

```
$ gustyc --lambda-answers --file prog.gy
{
  "tool": "callable-values",
  "pass": "hoist-applied-lambda-answers",
  "schema": "gusty.lambda-answers.v1",
  "rewritten": false, "settled": false,
  "declarations": [], "symbols": [], "declined": [], "refused": [],
  "reads_folded": 0, "folded_reads": []
}
$ echo $?
0
```

`--lambda-answers` takes a **source string**. The token after it was `--file`, so the flag's value became the
six characters `--file`, which parses — as the expression `-(-file)` — and the toolchain printed a well-formed
`gusty.lambda-answers.v1` account of *that* program and left through the exit code of success. The document
named no file, quoted no path, and carried nothing that would let its reader tell an account of `prog.gy` from
an account of the command line's own punctuation.

This was not a hypothetical. It was met while measuring **ADR 0461**'s machine path: the fold had demonstrably
fired two lines away in the same binary, and the first reading of the all-zero document above was "the fold did
not fire". ADR 0346 and ADR 0379 already state the rule this row tests — an account document is trusted because
it cannot describe a program the compiler did not compile, and a capability payload that describes the wrong
subject is not a neutral answer. Neither had been applied to where the subject is *chosen*: the command line.

Two doors already knew. `--verify-llvm` and `--debug-info` each carried four lines of their own:

```go
for _, v := range []string{*verifyLLVMF, *verifyLLVMFile} {
        if strings.HasPrefix(v, "-") {
                fmt.Fprintf(os.Stderr, "gustyc: %s expects a source, not %q — pass it last or use --flag=<source>\n", "--verify-llvm", v)
                return exitUsage
        }
}
```

…with the reason written out beside it: *"A value-taking flag followed by another flag swallows it:
`--verify-llvm --json "src"` would compile the string `--json` instead. Catch it as the usage error it is
rather than compiling nonsense."* The hole was that the family is not two flags. It is twenty-six, and the
rest of them had nothing.

## The finding that made the obvious fix wrong

The first implementation refused a value that **names a flag the FlagSet defines** — the narrowest rule that
covers the reported shape, in the spirit of **ADR 0337**'s "filter, don't widen". It was built, and then the
adjacent case was run:

```
$ gustyc --lambda-answers --not-a-flag      # nothing defines --not-a-flag
{ … "schema": "gusty.lambda-answers.v1", "reads_folded": 0, … }   exit 0
```

The same well-formed account of a program nobody defines, at the same exit code. The predicate had asked the
wrong question — *is this one of my flags?* — when the question the harm asks is *could this token have come
from anywhere except a program?* A name the FlagSet knows is sufficient evidence; it is not necessary.

Widening to "any leading dash" was the other obvious move, and it is also wrong: `gustyc --eval -1` has always
meant *run the program `-1`*, and `--opt-level 2`, `--target -x86_64-apple-darwin` and a `--cc` path beginning
with a dash are all real values. A guard that closed those would be a new bug filed under the same row, which
is exactly the failure **Gap R.289**'s row documents — a filed refusal that quietly becomes an answer (or here,
an answer that quietly becomes a refusal) must fail something first.

## The decision

`cmd/gustyc/main.go` gains one door, `refuseFlagShapedSource`, consulted once after `flag.Parse` and before
anything reads a source, over a table of the flags whose value is read as a program, a path, an artifact or a
bench tree — `--eval`, `--file`, `--verify`, `--check`, `--effects`, `--oracle(-file)`, `--emit-llvm`,
`--emit-ast`, `--emit-source-map`, `--verify-llvm(-file)`, `--debug-info(-file)`, `--callables`,
`--lambda-answers`, `--bench(-file)`, `--fmt(-file)`, `--bench-dir`, `--bench-baseline(-update)`, `--build`,
`--source-map-out`, `--stdlib` — and the two hand-written guards are deleted, because they are its special case.

1. **The predicate is `looksLikeAnOption`, and it asks the FlagSet.** A value is refused when it begins with
   `--` (the long-option shape, defined or not), or when `fs.Lookup` recognises it as one of this binary's own
   flags (which is how Go's flag package reads `-json` too). `-1`, `-2.5`, `-O2` are not options and stay
   programs. `-` (stdin) and `--` (the end-of-flags marker) are the command line's punctuation and are exempt
   whatever they look like. Asking the FlagSet rather than the string is also what lets `--file`, `--build` and
   `--target` keep accepting any real path.
2. **The answer is the usage class, exit 4** (**ADR 0006**). The mistake is in the command line, not in a
   program, so the tool must run neither leg to report it — and `--interp`'s refusal (ADR 0302) set the
   precedent that a command-line lie is answered in words, not honoured quietly. Exit 0 was the bug; exit 1
   would have blamed the user's code for a shell mistake, which is the error the `.gy`-suffixed positional
   guard already refuses to make.
3. **The sentence is the one the two guards already printed**, with two additions: it names *the flag that was
   refused* (the hand-written loop printed `--verify-llvm` even for its `--verify-llvm-file` sibling), and its
   escape hatch points at a form that works. `--bench-dir` and friends say *path* rather than *source*, because
   a flag that wants a directory should not claim to want text.
4. **`--flag=<source>` is the documented escape, and it is real.** `reorderFlags` now reports which flags
   arrived in the attached spelling, and the door honours that report: `gustyc --eval=--file` compiles the two-character
   program `--file` and answers `undefined name "file"` — the ordinary name error, about a program, from a
   caller who meant it. The row's Definition of done asks that the two spellings answer the *same* account for
   any real source, so the guard cannot be satisfied by pushing the trap one keystroke away;
   `TestTheAttachedAndSpacedSpellingsAnswerTheSameAccount` pins that byte-for-byte.
5. **`reorderFlags` keeps its token-ownership rule.** A value-taking flag still owns the following token
   whatever that token is, which is what makes `gustyc --build out src.gy --opt-level=2` and `--eval "-1"` work.
   The door does not fix the mis-association; it refuses to *answer* one. Those are different jobs, and doing
   only the second is what keeps the first from becoming a regression.

```
$ gustyc --lambda-answers --file prog.gy
gustyc: --lambda-answers expects a source, not "--file" — pass it last or use --lambda-answers=<source>
$ gustyc --bench-dir --json
gustyc: --bench-dir expects a path, not "--json" — pass it last or use --bench-dir=<path>
```

## Machine path

There is no new payload, deliberately. Exit 4 is the machine-readable fact (**ADR 0006**), no other usage error
in this CLI emits JSON, and inventing one for this class alone would split the usage contract in two — the
failure mode **ADR 0302** avoided by making `--interp` a plain refusal too. What the door adds to the machine
path is that stderr now carries the *refused flag's own name* and the offending value as quoted text, which an
agent can match exactly instead of guessing which flag the compiler decided to blame. `stdout` is empty on
every refusal, asserted per flag, because a refusal that also prints a document is the bug this ADR exists for.

## What this row does not do — **Gap R.550**

Measuring the family turned up the adjacent case, which this door deliberately does **not** touch:

| command | today |
|---|---|
| `gustyc --lambda-answers real.gy` (`real.gy` exists on disk) | exit **1** — `gustyc: lambda-answers: parse error at 1:1: unexpected token` |
| `gustyc --emit-source-map real.gy` | exit **0** — reads the file |
| `gustyc --file prog.gy --lambda-answers` | exit **4** — `flag needs an argument: -lambda-answers` (the flag package already answers this one; it is *not* a gap) |

`--emit-source-map`'s own comment states the convention as fact — *"a value that is not on disk stays source
text, like every other value-taking flag in this CLI"* — and measures as false for the rest of them:
`--lambda-answers real.gy` compiles the nine characters of the path's name as a program and reports a parse
error at the source's own first column, blaming line 1 of a file it never opened. It fails visibly, which is
why it is a usability row and not an exit-code one; the third line above is recorded here precisely because it
was hypothesised as a gap, measured as already correct, and therefore is not owed. **Gap R.550** is the row
that makes every value-taking flag read a value that is on disk as a path, or say in words that it will not.

## Tests

- `cmd/gustyc/source_flag_door_test.go` — the predicate over 14 values (`--file`, `-json`, `--not-a-flag`,
  `--eval=--file`, `-1`, `-2.5`, `-O2`, `-`, `--`, `""`, an ordinary source, …), the door over five command
  lines, and `reorderFlags`' attached-spelling report. Run as functions, so a rule that drifts fails a decision
  row and not a program.
- `integration/source_flag_door_test.go` — the family at the real binary: each of the 26 flags handed `--file`
  must exit 4, print nothing on stdout, and name itself in stderr; the controls prove a real source still runs
  (`--eval -1`, `--eval=--file`, a flag after the positional, a bare positional); and no row — refused or
  answered — may reach exit 2 (**ADR 0166**).
- `TestEveryFlagTheCLIGuardsIsAFlagTheCLIHas` asks the binary's own `--help` whether each guarded flag still
  exists, so the table cannot rot into certifying a door nobody walks through.
- `integration/lambda_module_read_test.go`'s filed table, written to fail the day the door spoke, moved from
  "`--lambda-answers` answers an all-zero account at exit 0" to the refusal, by its own text.

## Alternatives rejected

- **Refuse a value with a leading dash** (what the two deleted guards did, and what the row's own wording asked
  for). Breaks `--eval -1`, `--opt-level 2`, and any `--target`/`--cc` value that begins with a dash, and it is
  the sweep **ADR 0337** warns against: it would have been a new wrong answer, filed under a bug fix.
- **Refuse only a value that names a defined flag.** Measured, and it does not close the hole:
  `--lambda-answers --not-a-flag` still prints a well-formed account at exit 0.
- **Make `reorderFlags` stop consuming a flag-shaped token.** Then `gustyc --eval -1` leaves `-1` as a
  positional and the flag package reports `flag needs an argument`; the toolchain's answer to a source string
  becomes a usage error about a flag. The door belongs at the point of *use*, not at the parser.
- **Compile the flag's name and let the program's own error speak.** That is today's bug: `--check` and
  `--emit-ast` fail with `undefined name "file"`, which blames the user's source for a shell mistake, and
  `--lambda-answers` and `--callables` do not fail at all.
- **A JSON payload for this usage class.** See *Machine path*.
- **Silently read `--file prog.gy` as if the user had written `--file prog.gy`.** Guessing which flag the user
  meant, and running a program on that guess, is the wrong-subject answer again with more confidence.

## Consequences

- `docs/operations.md` § *Flags anywhere on the command line* gains the door, the table of five command lines
  above, and the list of guarded flags; its exit-code table now says the usage class covers a source flag handed
  a flag.
- The paragraph claiming `--eval --help` "still evaluates the text `--help`" is corrected: that command line is
  a usage error now, and `--eval=--help` is the form that means it.
- **Gap R.550** joins the open queue: path-or-text, honoured by one door or named in words by all of them.
- Two guards that could drift apart from their family are gone; the family has one sentence, tested against the
  binary's own help text.
