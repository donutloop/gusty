# A statement's refusal is the door's own sentence, at the frame and line the program wrote it

**Roadmap:** **Gap R.377** (closed here — the refusal wrapper that leaked the statement's Go type name) ·
**L11.7** (the road the probe was filed from, whose sibling row ADR 0396 had to work around the wrapper) ·
**ADR 0166** / **ADR 0249** (a refusal names what is missing, in the program's words, and never the compiler's own
machinery) · **Gap R.38** (the same defect, closed for the `*FuncDef` arm: `a *lang.FuncDef statement` →
`function "m"`) · **ADR 0212** (the refusal class is `unsupported` — exit 1, never exit 2) · **ADR 0346** (a
capability refusal's rows ride on the `capability` object an agent branches on, not on prose) ·
**ADR 0345 §3** (the pass's private spellings never reach a reader's sentence — this row extends the rule from
symbols and minted parameters to the diagnostic wrapper) · **ADR 0232** (one question, one asking place:
`refusalIsSentence`) · **ADR 0343** (no process state; the wording is a pure function of the node and the payload) ·
**ADR 0302** (the record leg holds these programs' answers; a wording that moved a ledger row would be a divergence
that silently disappeared)

## Status

Accepted. Landed 2026-10-22.

## Context

ADR 0396 shipped the ordering door's hook lambda and left behind, in its own two test files, a pair of
carve-outs. Both files assert that a refusal leaks nothing from the compiler's own machinery — `*lang.`, `0x`,
`%!s(` — and both had to **cut a prefix off the front of the message first**:

```go
// pkg/lang/key_hook_frame_test.go, before this ADR
if m := regexp.MustCompile(`^codegen: a \*lang\.[A-Za-z]+Stmt statement cannot be compiled: `).FindString(said); m != "" {
        said = said[len(m):]
}
```

What they were cutting was what a reader sees:

```console
$ gustyc --jit nested_hook.gy
gustyc: jit: codegen: a *lang.ExprStmt statement cannot be compiled: `key=lambda ...` is refused: a lambda is a
function VALUE, and this backend has none — `def` bindings are declared, not assigned, so the key has no word to
carry to the call (roadmap L11.7)
```

The half after the colon is the door's own sentence and it is complete: it names the construct, the missing half
and the row that owns it. The half before the colon names a Go type the reader does not have, in front of a
sentence written to be acted on. It came from `noteUnlowered` (`pkg/lang/codegen.go`), which wraps whatever a body
emission returned:

```go
case Stmt:
        what = fmt.Sprintf("a %T statement", n)          // ← the leak
...
g.emitErr = fmt.Errorf("codegen: %s cannot be compiled: %v", what, err)
```

Three facts made this a row rather than a typo.

* **The sibling arm was already fixed.** Gap R.38 changed `a *lang.FuncDef statement cannot be compiled` to
  `function "m" cannot be compiled`, and the drift ledger (`pkg/lang/testdata/interpreter-golden-drift.json`)
  quotes that sentence in two rows. So the wrapper was not "always wrong": it is wrong when it names a **Go type**,
  and right when it names **the program's thing** — and the `*FuncDef` arm's text is now load-bearing for a ledger.
* **The family is bigger than the hook.** Measured on the parent build, the same wrapper rode on the arm for every
  statement kind a closure body can refuse — `ExprStmt`, `ReturnStmt`, `TryStmt`, `WithStmt`, `MatchStmt` — because
  the closure road (`closure.go`) funnels each body statement's error through this one helper so that a body that
  could not be lowered cannot also be executed (Gap R.35, ADR 0227).
* **A carve-out is a leak that has been promoted to the test suite.** Every new refusal row that wanted to assert
  its own sentence had to route around this one. Three did, in one commit.

## Decision

**A payload that is already a refusal is handed to the reader untouched; a payload that is not gets a sentence
about the statement, written in the program's words.** Two doors in one new file, `pkg/lang/refusal_surface.go`,
and one rewritten arm.

1. **`refusalIsSentence(err)` asks whether the payload needs an introduction.** The house rule makes it
   answerable instead of guessed: ADR 0166/0249 require a refusal to name the row that owns the missing half, so a
   sentence the backend wrote deliberately carries a `roadmap` citation (or an `ADR `/`Gap ` reference); and
   `ClassifyCapabilityRefusal` — the same classifier behind `--capabilities` and `--json`'s `capability` object
   (ADR 0346) — is the second witness that a payload is a refusal the backend wrote on purpose. A raw helper error
   (`list literal elements must be integers, not fn(any)`) carries neither mark, and keeps the wrapper.
2. **The passthrough keeps the payload verbatim and adds the two facts the wrapper knew** — the frame and the
   line — as a trailing clause:

   ```
   codegen: `key=lambda ...` is refused: a lambda is a function VALUE, and this backend has none — … (roadmap L11.7) (in `use`, line 4)
   ```

   `atLocation` prints the frame with `callableDisplayName`, so a clone or a hoisted lambda is named by the host it
   came from and its IR symbol never reaches the sentence (ADR 0345 §3); a statement with no span gets no clause,
   because a wrong line is worse than none.
3. **The wrapper arm cannot regress either.** When the payload is not a sentence, `what` comes from
   `stmtSurface(n)` — the KEYWORD the program wrote plus the line (`the \`return\` at line 6`,
   `the \`print(…)\` call at line 7`, `the \`with\` at line 3`) — so the sentence about an unlowered statement is
   still informative and still says nothing the reader cannot see in their own file. The statement-side sibling of
   `exprSurface`, and it is the fallback that matters: an unknown statement kind is still a statement at a line.
4. **The `*FuncDef` arm is excluded explicitly**, in code and in a test — not by accident of type assertion.
   `*FuncDef` implements `Stmt`, so a naive `where.(Stmt)` would have folded it into the passthrough, changed
   `function "m" cannot be compiled:` and made two drift-ledger rows silently disappear. The guard
   (`TestTheFuncDefArmStillNamesTheFunction`) fails if anyone "simplifies" that away, and its message says what to
   do if it ever legitimately changes.
5. **The same door writes the last `%T` in the statement road.** `codegen_stmt.go`'s default arm said
   `unsupported statement %T`; it now says `the \`with\` at line 4 has no compiled lowering, and this backend
   refuses to run a statement it cannot build (roadmap L11.7, ADR 0166)`.
6. **What this row does not reach is filed with its own row.** The `%T` leaks at the *assignment* and *item-store*
   doors (`unsupported assignment target *lang.Attr`, `… got *lang.ListLit`) and at the front end
   (`parse error … (got *lang.IntLit)`) are the same disease at different doors, measured on the parent build and
   pinned as still-leaking by `TestWhatTheRowLeavesStandingIsFiledNotFixed`; they are **Gap R.379**, and the pin
   fails when someone closes them so the row can be moved rather than re-discovered.

## What moved, measured

The before column is a build of `b1395ec` (the parent) run beside a build of this commit, not a memory. The probe
round was 23 programs — the 20 of the family (one per statement arm the road reaches, each taken from a closure body,
which is where the wrapper lived), the two controls that had to stay answering, and the one sibling shape the drift
ledger quotes — each asked of CPython first, with the harness refusing to produce a count until a known-good program
printed correctly through it (ADR 0394's lesson, applied without being re-derived).

| | before (`b1395ec`) | after |
|---|---|---|
| probes refused in words (exit 1, `phase: "codegen"`) | 18 | **18** — the same 18 |
| refusals carrying a Go type name | **6** | **0** |
| newly clean | — | **6** |
| answers or refusals regressed | — | **0** |
| the `*FuncDef` arm's sentence | as written | byte for byte the same |

| | the nested-`def` hook probe |
|---|---|
| before | `codegen: a *lang.ExprStmt statement cannot be compiled: \`key=lambda …\` is refused: …` |
| after | `codegen: \`key=lambda …\` is refused: … (roadmap L11.7) (in \`use\`, line 4)` |
| exit code, class, phase | `1` / `unsupported` / `codegen` — **unchanged on both roads** |

| family probe (closure-body arm) | before | after |
|---|---|---|
| key hook reading two frames out (`ExprStmt`) | wrapper + Go type | door's sentence + `(in \`use\`, line 4)` |
| body reads a declared function as a value (`ExprStmt`) | wrapper + `codegen:` doubled | door's sentence + `(in \`use\`, line 6)` |
| body calls a number (`ReturnStmt`) | wrapper | door's sentence + line 4 |
| comprehension over a text (`ReturnStmt`) | wrapper | door's sentence + line 3 |
| container copy inside `try` (`TryStmt`) | wrapper | registry sentence + line 3 |
| `with` on a number (`WithStmt`) | wrapper | door's sentence + line 3 |
| `match` arm calling a number (`MatchStmt`) | wrapper | door's sentence + line 3 |
| `if` block copying a container (`IfStmt`) | wrapper | door's sentence + `(in \`use\`, line 3)` |
| `while` block calling a number (`WhileStmt`) | wrapper | door's sentence + `(in \`use\`, line 4)` |

Nothing moved that should not have: the five controls the CLI leg re-derives from `python3` inside the case (the hook
that reads its OWN frame, the nested `def` that calls a declared function, the answering `try`, the answering `match`
and the answering `for` in a closure body) print identically on `--jit` and `--aot`, the four the record leg reads are
on the record with a **missing record failing**, every emitted module still passes `llc -filetype=null`, and exit 2 is
forbidden in every loop. The `*FuncDef` arm printed `function "m" cannot be compiled:` before and after, byte for byte.

## Agentic path

`--json` is where an agent reads a refusal, and the row is a machine-path fix as much as a human one: the
`"error"` string of these programs no longer contains a Go type name for a script to accidentally branch on, and the
refusal's rows arrive in the channel that is declared to hold them — `capability.rows` for a capability refusal
(ADR 0346), the roadmap citation in the prose for a door's own sentence. `TestTheMachinePathStillClassesAStatementRefusal`
pins the whole document: `ok:false`, `exit:1`, `phase:"codegen"`, the door's opening sentence, the frame-and-line
clause, and the rule that a refusal must name its row **in one of the two channels** rather than in neither. No flag
changed, no schema field added, no code invented (a refusal the registry does not know still gets no code).

## Alternatives rejected

1. **Delete the wrapper entirely.** Rejected: it is load-bearing for the payload that is *not* a sentence. A raw
   helper error from inside a body would reach the reader as a bare complaint with no statement attached, which is
   how Gap R.35's "a body that was never lowered answered 0" class of bug gets hidden. The row is about the WORDING
   of the wrapper, not its existence.
2. **Give the payload a location prefix (`line 4: …`).** Rejected: it moves the sentence the reader must act on
   behind a number, and it breaks every test that pins a refusal by its opening. A trailing clause keeps the
   payload byte-identical — which is also what lets the ADR 0396 rows' pins keep asserting what they always
   asserted, with their carve-out deleted instead of edited.
3. **Mark deliberate refusals with a Go type (`type refusalError struct` at every site).** Rejected for the reason
   ADR 0346 already records against the same idea: ~142 `codegen:` refusal sites, a diff to code that decides
   programs, and a refusal whose typed mark was forgotten would still compile — coverage that rots quietly. The
   classifier's failure mode is the loud one, and its tests are the leash.
4. **Leave the wrapper and strip the Go type from the leak-forbidden lists' exceptions.** Rejected: three carve-outs
   in one commit is the signal. A test that removes a leak before checking for leaks checks nothing.
5. **Fold the `*FuncDef` arm in, since `*FuncDef` is a `Stmt`.** Rejected by the ledger. That arm's sentence is
   Gap R.38's and is quoted verbatim by two drift rows; silently rewording it is a divergence disappearing, which
   `TestMain` exists to catch and which the exclusion + its guard make impossible.
6. **Fix the assignment, item-store and parser `%T` leaks in this commit too.** Rejected on scope, not on merit
   (AGENTS.md: one commit per feature, and a newly measured defect gets a fresh row): those doors name a *node*,
   not a statement, and their sentences belong to roads this row does not touch. They are **Gap R.379**, with the
   leaks pinned until then.

## Consequences

* `noteUnlowered` has one place that asks "is this payload already a sentence?" — `refusalIsSentence` — and one
  place that names a node: `exprTyName` for an expression, `function %q` for a definition, `stmtSurface` for a
  statement. A new arm that reaches for `%T` is the regression this file is written against, and the leak-forbidden
  list in both test files is the tripwire.
* The two ADR 0396 carve-outs are **deleted**, not edited: the tests now fail if the wrapper returns. Removing a
  workaround is the evidence that the workaround's cause is gone.
* Refusal sentences this row uncovered are now shared by every road that reaches the same door: the same program
  written at module level, inside a `def`, or inside a nested `def` prints the same sentence, differing only by the
  location clause. That is a claim with a test (`TestWhatAStatementRefusalSays` compares the opening of the message
  with the door's own text), and it is the reason the payload is passed through unprefixed rather than rewritten.
* `codegen: ` is added exactly once. Roads that write their own `codegen: ` mark have their prefix trimmed on the
  passthrough path, so the doubled sentence (`… cannot be compiled: codegen: "helper" names function…`) that the old
  wrapper produced for ADR 0283's refusal cannot come back.
* Nine record entries were written for the controls by `tools/recmerge` (the record to **8534**), none refused; the
  statement-wrapper family itself has nothing to record, because it refuses — and `recmerge -check` is the mode that
  says so rather than inventing an answer.
* Gap R.379 stands open with three measured leaks pinned in a table that fails when they are fixed — so that closing
  them is an edit to the tracker, not a discovery.
