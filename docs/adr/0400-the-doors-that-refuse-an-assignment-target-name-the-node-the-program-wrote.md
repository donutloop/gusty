# The doors that refuse an assignment target name the node the program wrote, not the node the parser built

**Roadmap:** **Gap R.379** (closed here — a Go type name reaches a reader through an item store, an attribute store and
the parser's augmented-assignment target) · **Gap R.380** and **Gap R.381** (the two divergences this row measured and
filed instead of rewording) · **L11.1** / **L11.7** / **L11.8** (the assignment roads the three doors sit on, and a
refusal is part of the model just as the sentence it arrives in is) · **ADR 0398** (the statement door this moves to
expressions: `refusalIsSentence` + `stmtSurface` — this row is its sibling, not its copy) · **ADR 0386**
(`exprSurface` — the rendering this door shares, and whose `%T` fallback this door must not inherit) ·
**ADR 0166** / **ADR 0249** (name what is missing, name the row that owns it, never name the compiler's own machinery) ·
**ADR 0212** (all three sites were already the `unsupported` class at exit 1 — the wording was the whole defect, which
is what makes a wording change a legitimate one-feature commit) · **ADR 0346** / **ADR 0004** (`--json` shows a machine
the identical string, so both channels move together or neither does) · **ADR 0345 §3** (no private spelling in a
reader's sentence — extended from symbols and minted parameters to AST node types) · **ADR 0361** (one container
representation, two spellings: why a tuple literal is a `*ListLit` and still has to be called a tuple) ·
**ADR 0232** (one question, one asking place — three doors, one `nodeSurface`) · **ADR 0302** (the record leg holds the
controls' answers; a refusal is not recordable, and the row asserts that) · **Gap R.38** (the naming discipline, asked
per door rather than once) · **Gap R.214** / **ADR 0334** (the read side of the position **Gap R.381** files on the
write side) · **Gap R.272** (the attribute **read** door, named here and deliberately left standing)

## Status

Accepted. Landed 2026-10-23.

## Context

ADR 0398 closed a family by closing one member of it. Its rule — a refusal is written for the person who wrote the
program, so its subject is what they typed and never the node the parser built — was applied to the statement wrapper in
`noteUnlowered`, and its own test file left three rows behind in `TestWhatTheRowLeavesStandingIsFiledNotFixed`:

```console
$ gustyc --jit item_assign.gy         # [1, 2][0] = 3
gustyc: jit: codegen: item assignment needs a container variable on the left (d[k] = v), got *lang.ListLit; CPython accepts more forms
$ gustyc --jit attr_assign.gy         # (1).x = 2
gustyc: jit: codegen: unsupported assignment target *lang.Attr
$ gustyc --jit aug_assign.gy          # x = 1 ⏎ 3 += x
gustyc: parse error at 2:3: parser: augmented assignment requires a name or attribute target (got *lang.IntLit)
```

Three doors, three separate `fmt.Errorf` calls, three `%T`s — and none of them passes through `noteUnlowered`, which is
why ADR 0398's fix did not reach them and why "the family is closed" would have been a false claim one cycle early. Each
is already the right VERDICT at the right EXIT CODE: `unsupported`, exit 1 (ADR 0212). So the wording is the entire
defect — and a Go type name is the worst possible subject for a sentence, because `[1, 2][0] = 3` contains no
`ListLit`, the reader has no table to look it up in, and ADR 0004's `--json` consumer is handed the identical string.

## Decision

**One door for expressions, beside the one ADR 0398 built for statements.** `nodeSurface(Expr) string` joined
`stmtSurface` in `pkg/lang/refusal_surface.go`, and each of the three sites kept its own advice around it:

```
item assignment needs a container variable on the left (`d[k] = v`), and the program writes into the list literal
`[1, 2]` — bind the container to a name and store through it there (CPython accepts more forms; roadmap L11.1, Gap R.381)

cannot store an attribute on the attribute `x` of a number — this backend writes an attribute only on an instance the
compiler typed, and the reference raises AttributeError for this program (roadmap L11.1, Gap R.272)

parser: augmented assignment (`+=`) needs a name or an attribute on the left, and the program puts the number `3` there
(roadmap L11.1, Gap R.380)
```

Four rules make it a door rather than three new strings:

1. **Name the node twice over** — kind in the language's words, and the text as written: "the list literal `[1, 2]`",
   "the subscript `xs[0]`", "the text `"abc"`". A reader who wrote `xs[0] += 5` sees `xs[0]`, not a category.
2. **Kind comes from the node, never from a rendering the program did not contain.** An attribute is "the attribute `x`
   of a number" — the attribute NAME plus the kind of the thing it hangs off — because `1.x` is a string no author typed
   and no parser accepts. A tuple is named by ADR 0361's flag, and the parentheses are re-rendered because the canonical
   formatter prints the shared `[…]` shape; writing that rule is what caught the door disagreeing with itself (one tuple
   path said "the tuple `(1, 2)`", the other "the tuple literal `(1, 2)`"), which a shared door turns into a test
   failure instead of a review comment.
3. **The fallback may not be a type name.** `exprSurface` answers a node its formatter cannot write with `a %T` — right
   for the messages ADR 0386 built it for, fatal for a door whose whole claim is that it names the program.
   `nodeSurface` says "an expression the compiler cannot name": an honest gap in the reader's language rather than a
   fact about this compiler's AST, with a witness (`oddExpr`, a test-local node) so the arm cannot rot back into a leak.
4. **Name the row that owns the missing half.** A refusal an agent branches on says which roadmap row will remove it.
   Where the reference agrees with the verdict, the sentence says so in words ("the reference raises AttributeError for
   this program") instead of citing a row that does not exist.

## What this row deliberately did not touch

`unsupported attr expression` — the attribute **read** door — is the same disease one door further along, and the fix is
one line. It was not taken, because that exact string is quoted verbatim by **four rows of
`pkg/lang/testdata/interpreter-golden-drift.json`**, by `pkg/lang/dead_binding_test.go` and by the CLI's exit-code table
(`cmd/gustyc/ir_invariant_test.go`). ADR 0398's most-feared failure is a drift row whose wording changes under
`TestMain`, because that is a recorded divergence silently disappearing; rewording this sentence in this commit would
have produced exactly that, four at once. Its owner is **Gap R.272**, whose definition of done already asks for the
better wording ("name the base's origin in the sentence rather than `unsupported attr expression`") — and which must
therefore move those four ledger rows in the same commit as the sentence.
`TestWhatTheAttrReadDoorStillSpeaksTheLedgersSentence` pins the sentence today and fails if it changes, so the next cycle
is told, in the failure text, what has to be re-recorded.

## The family's fifth finding: five refusals the reference does not share

The table was written to cover every node kind a program can put on the left of `=` and `op=`, and asking `python3` each
of them split the sixteen refusals in two. Eleven are refusals the reference agrees with — a `TypeError` for
`{1, 2}[0] = 9`, an `AttributeError` for `xs.pop = 3`, a `SyntaxError` for `3 += x` — and for those, wording was the
whole fix. Five are programs the reference **answers**:

| the program | CPython | this backend, before and after this row |
|---|---|---|
| `xs[0] += 5` | `6` | exit 1, the parser's `op=` refusal |
| `def f(): return [1, 2]` / `f()[0] = 3` | `1` | exit 1, the container-VARIABLE refusal |
| `[1, 2][0] = 3` | `1` | exit 1, same door |
| `d["x"] = {}` / `d["x"][0] = 1` | `1` | exit 1, same door |
| `c.a[0] = 3` | `[3]` | exit 1, same door |

A refusal the reference does not share is not a wording bug, and rewording it is a cycle that buys nothing — so the row
stopped at the sentence and filed **Gap R.380** (the parser's `op=` rule admits `*Name`/`*Attr` in front of an
`*ir.InstIndex` lowering `genItemStoreFromExpr` has had since **Gap R.98**: a question answered by the stage that is not
the one holding the door, ADR 0232's shape again) and **Gap R.381** (`assignIndex`'s `*Name` rule refuses stores the
reference performs — the mirror image of what ADR 0334 closed for reads, and a door that has been admitting it in its own
sentence ever since: "CPython accepts more forms"). Both are cited from inside the sentences this row shipped, and both
are pinned in both legs against the live reference, in a table that fails if a program starts answering (the row is
closed — delete the pin) AND if the reference ever starts refusing (the pin's premise died).

## Measured

Sixteen door shapes, five controls, two declines, each asked of `python3` first and then of a build of the parent
(`d587d9b`) and a build of this commit side by side. The probe list is **extracted from the Go tables themselves**, so a
measurement cannot drift from what the tests assert — the failure this row most wanted to avoid, having written three
expectation rows from memory in the previous cycle and had the record catch all three.

| | parent (`d587d9b`) | this commit |
|---|---|---|
| the sixteen refusals, at exit 1 | 16 | **16** (none moved class or code) |
| of them carrying a Go type name (`*lang.X`, `*ir.X`) | **16** | **0** |
| of them naming a roadmap row / the reference's verdict | 0 | **16** |
| the five working target forms printing the reference's bytes | 5 | **5** |
| programs where the compiled leg prints what CPython does not | 0 | **0** |
| exit 2 anywhere | 0 | **0** |
| record entries written by `tools/recmerge` | 8563 | **8568** (the 5 controls, each from CPython) |
| refused shapes filed on the record | — | **0**, and asserted absent |

The last two rows are the pair worth reading together. A wording cycle moves no verdicts and still owes the record an
entry for every control it newly asks: those five target forms had never been asked in those exact spellings, so
`tools/recmerge` recorded all five from CPython with the compiled leg cross-checked. What it must never do is buy an entry
for a refusal — `TestTheTargetFormsTheReferenceAnswersAreOnTheRecord` fails if any of the sixteen refused shapes is on the
record, which doubles as the check that no future “fix” sneaks a refused program onto the artifact.

## Agentic path

`--json` shows a machine the identical string, so the fix landed on both channels in one commit and the pair is asserted
together: `ok:false`, `exit:1`, `phase` `codegen` (or `parse`, where a parse-time refusal has always belonged), the
program's node inside `"error"`, and no Go type name anywhere in the document. The `capability` object still resolves
whenever the registry knows the sentence (ADR 0346); these three doors are not in the registry today, and the table pins
that absence so a code cannot arrive or leave without the table being read.

An agent that branches on these failures branches on substrings a human can also read: `"item assignment needs a container variable"`, `"cannot store an attribute on"`, `"augmented assignment (\`+=\`) needs a name or an attribute"`.
No new field, no schema change, no version bump — the machine path got the same improvement the human path did, because
it was reading the same field all along.

## Alternatives rejected

1. **Give the AST node types better `String()` methods.** The reader does not have the type either, and a `String()` on
   an AST node invites every future diagnostic to reach for it — which is how the `%T` came to be in the sentence.
2. **Reuse `exprSurface` directly.** Rejected for its fallback, not its rendering: `a %T` is correct for what ADR 0386
   built it for and wrong for this door. `nodeSurface` shares the rendering (via `exprSourceText`) and owns its fallback.
3. **Reach for `exprTyName` ("a list literal") without the text.** Half the information: a reader who wrote
   `[1, 2][0] = 3` and is told "a list literal" has learned the compiler's category, not what to fix.
4. **Reuse `stmtSurface`.** It names statements (`the `n +=` assignment at line 4`); the target of an assignment is an
   expression, and "cannot assign to the `n +=` assignment at line 4" is worse than the `%T` it replaced.
5. **One generic sentence for all three doors** ("unsupported target"). Drops the advice, and ADR 0166's rule is that a
   refusal names what is missing and what to do: bind a name, write an attribute on a typed instance, put a name left of
   `op=`. Three doors, one shared node-naming answer, three sentences.
6. **Reword `unsupported attr expression` in the same commit, since it is the same bug.** Rejected above — four drift
   rows quote it verbatim, and a fix that makes four recorded divergences disappear is the failure ADR 0398 names.
7. **Answer the five programs the reference answers.** `xs[0] += 5` is nearly free. But a cycle that widens a parser rule
   is a feature, AGENTS.md is explicit that a feature is one row and one commit, and doing it here would have made this
   row's central claim — the set of programs that compile is unchanged — false. Filed as R.380/R.381 with pins that fail
   the day they are closed.
8. **Add the three doors to the capability registry.** A capability code is a branch an agent takes, and these three
   sentences are being read by anyone at all for the first time this cycle. The tables pin their absence so registration
   is a deliberate edit with its own evidence.
9. **Leave the parser door, on the theory that a `parse error` is not a codegen refusal.** `--json` shows it as-is to the
   same consumer; the row's claim is about what a reader reads, not about which phase wrote it.

## Consequences

* **Code.** `refusal_surface.go` is now the one place that answers "how do I refer to a piece of the program in a
  refusal?": `stmtSurface` for statements, `nodeSurface`/`nodeKind` for expressions, `callSurfaceName` for a callee,
  `refusalIsSentence` for whether a payload needs wrapping at all. A fourth door adds an arm there rather than a
  `fmt.Sprintf` of its own. `codegen_index.go`, `codegen_assign.go` and `parser.go` each lose one `%T`, gain one call and
  a citation; `grep -rn 'got %T\|target %T' pkg/lang` is the check that they stay gone.
* **IR.** Nothing. No emission, no runtime entry, no parser acceptance rule changed — which is why the controls are
  judged on bytes and the record is judged on presence rather than on new entries, and why exit 2 appears nowhere.
* **Tests.** `pkg/lang/target_surface_test.go` (16 doors for node/citation/leaks/ORDER, `nodeSurface` asked directly over
  12 nodes including `nil` and both tuple representations, 5 controls on the compiled and record legs with a **missing
  record failing**, 5 filed-not-fixed programs beside their owner row) and `integration/target_surface_test.go` (the same
  16 through the CLI on `--jit` **and** `--aot` at exit 1 with exit 2 forbidden, the `--json` leg, the controls
  re-derived from `python3` inside the case, the refusals asserted absent from the record, the debt pins asked of the live
  reference in both directions).
* **Pins that fired.** `TestWhatTheRowLeavesStandingIsFiledNotFixed` lost the three R.379 rows it pinned and says so in
  its own comment — the guard failed exactly as written ("move this row to DONE and delete the pin"), which is the
  difference between filing a leak and listing one.
* **Gap R.272** gains a measured member and an explicit obligation: the attribute read door, the four ledger rows that
  must move with it, and `def bump(c): c.v += 3` — the reference's `3`, this backend's `unsupported attr expression`, on
  no record and pinned nowhere until now.
