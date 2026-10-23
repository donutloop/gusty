# The truth of a text is its length: the condition road asks the one text question like every other

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.407** (the truth of a text that arrives from a call was asked of
the intern table). Closes, in the same commit, two machine-face rows it measured on the way — **Gap R.408** (two
refusals reached agents with no capability code) and **Gap R.410** (the parallelism guard could rewrite another
package's drift ledger). Files **Gap R.409** (a parameter two calls fill with different kinds).

## Context

```gusty
def f():
    return "abc"

if f():          # CPython runs the body; this backend printed `f` at exit 0
    print("t")
```

One wrong question, answering a whole family. The condition road (`truthOperandErr`) has an arm for every shape
it had been asked about — a literal, a name the binding road recorded as holding a constant text, a `%`-formatting
door, a repeat — and its **fallback tests the operand against zero**. A text's value in this backend is an index
into `@str_tab`, so that fallback does not ask whether the text has any characters: it asks **which table entry
the intern table handed out**. The first text a program interns is entry 0, and every program below is about the
first text:

| program | CPython | this backend, before |
|---|---|---|
| `def f(): return "abc"` / `if f():` | runs the body | took the else arm, exit 0 |
| `def f(i): return "ab"[:i]` / `while f(i):` | loops twice | never entered the body |
| `def f(): return "abc"` / `print(not f())` | `False` | `True` |
| `def f(): return "abc"` / `print("y" if f() else "n")` | `y` | `n` |
| `def f(): return "abc"` / `s = f()` / `if s:` | runs the body | took the else arm |
| `class T: def m(self): return "abc"` / `if T().m():` | runs the body | took the else arm |
| `except ZeroDivisionError as err:` / `if err:` | runs the body | took the else arm |

Twelve of the twenty-eight sources this file's family carries were wrong on the parent commit, all of them at
the exit code that means the program ran. Nothing refused, nothing crashed, nothing verified badly.

The fix was not new machinery. The unrolled **loop item** already asks `rt_str_len` for exactly this reason — it
is the arm `@rt_pair_truth` takes for the text tag — and the print door, the operation door and the sequence door
had each, in their own cycle, been told **ADR 0232**'s rule: *a fact the compiler asks twice is a fact that
answers yes once*. The condition road was the last reader of a text that had not been told, because until recent
cycles there was no road at all that could answer `if f():` — the container-answer door (**ADR 0333**) and the
answer-kind gate (**ADR 0368**) arrived later, and each widened what a call's answer can be without the condition
road being asked to follow.

## Decision

1. **The condition road asks the one text question.** Before its fallback, `truthAsksTheInternedText(e)` asks
   whether the operand is a text whose index no fold can read — a call into a text-returning `def` or method,
   `str(...)`, `repr(...)`, a text method's answer, a slice of a built text, a concatenation of built texts, the
   name an `except … as` clause bound. If it is, the condition is `rt_str_len(operand) != 0`, the same door
   `@rt_pair_truth` takes for the text tag. No new runtime entry point.
2. **What the fold can read still folds.** A literal, and a name the binding road recorded against a constant
   text, keep their compile-time `true`/`false`; the IR guard asserts both halves — the shapes that must emit
   `@rt_str_len` do, and the shapes that must not, don't, so a future rewrite that routes the easy case through
   the runtime fails as loudly as one that routes the hard case through the fold.
3. **The roads that own other objects keep their roads.** A container's condition still calls the container's own
   length helper; a float's is still an `fcmp`; a body that answers more than one kind is still carried by the
   answer plan and reads its own tag (**ADR 0368**); a loop item still goes through `loopElemTag` (**ADR 0386/0393**);
   a pair-supplied parameter still reads the tag the caller wrote (**ADR 0389/0393**). Those arms sit above this
   one and the guard family pins each of them, because the danger of a truth door learning one more shape is that
   it starts answering an object that already had a better answer.
4. **The caught name is a text for this purpose too.** `g.caughtVars` joins `g.internedVars` in the name test:
   **ADR 0406** binds `except … as err:` to the interned message, and the binding road that fills `internedVars`
   never sees an `except` clause, so `if err:` was falling to the payload test. What this does *not* claim is that
   the caught name is the exception object — that is **Gap R.395**, and `if err:` on an exception with an empty
   message is the reference's truthiness question, not this row's.
5. **Two refusals this road walked past got codes** (**Gap R.408**, the **L11.8**/**ADR 0346** contract):
   `lower.unsupported.bool-of-a-container` (`bool([1])` spelled as a call, beside **Gap R.131**/**Gap R.370**) and
   `lower.unsupported.argument-kind-undecided` (`def w(v)` called with `w(0)` and `w("a")`, which is **Gap R.409**).
   Both sentences were well-written prose that resolved to nothing, so an agent had to string-match them.
6. **The harness stopped writing artifacts it was never asked to write** (**Gap R.410**). The parallelism guard
   shells out to a *filtered* `go test ./pkg/lang/`, and `pkg/lang`'s `TestMain` adjudicates its drift ledger:
   with `GUSTY_GOLDEN_UPDATE=1` in the environment — the way the house runs the artifact-writing mode — that
   subset rewrote the ledger with the one row its own four cases diverge on, deleting 317. `parifyEnv` now strips
   `GUSTY_GOLDEN_UPDATE`, `GUSTY_GOLDEN_MISSING`, `GUSTY_GOLDEN_REPORT` and `GUSTY_DEBT_UPDATE`, and a case asserts
   the scrubbing holds when the parent has each set.

## Machine path

- `--json` on either refusal now carries a `capability` member with `code`, `suggestion` and `rows`; the registry's
  generated table in `docs/operations.md` is regenerated from `pkg/lang/capability.go` by `tools/captable`, and the
  guard that compares them byte for byte is what keeps the human and machine faces the same inventory.
- `--emit-llvm` on `if f():` shows the `call i32 @rt_str_len(i32 %…)` and the `icmp ne` that follows it, which is
  what the IR assertion reads; a tool can tell "measures the text" from "tests the index" without running anything.
- The answered family walks both witness legs per program: the compiled leg and the record's stdout
  (`pkg/lang/truth_of_a_built_text_test.go`), and the CLI against the pinned CPython plus the record-membership
  assertion (`integration/truth_of_a_built_text_test.go`). No expectation here was written by this backend.

## IR implications

- A text condition emits `%n = call i32 @rt_str_len(i32 %v)` then `%p = icmp ne i32 %n, 0`, and the `br i1` that
  consumes it is unchanged. `@rt_str_len` is the existing internal helper in the arena's runtime block — **no new
  door**, and the assertion that a folded condition emits none is what stops the fold from quietly decaying.
- Because the answer is a condition (`i1`) rather than a value, the value word is untouched: this row does not add
  a kind, does not touch the tag table, and does not widen what crosses a call boundary. **L11.1** still owns the
  representation; this row only stopped the last reader of a text from reading it wrong.

## Alternatives rejected

- **Testing the index against zero "conservatively".** It is not conservative: the first interned text is entry 0,
  so the guess is wrong for the smallest and most ordinary programs, and right for everything written after them —
  the worst possible distribution, because the failing case looks like a toy and the passing cases look like proof.
- **Reusing `@rt_pair_truth` with a text tag invented at the condition.** The pair door wants a payload *and* a tag
  the emitter wrote; here there is no tag, only a known-kind expression. Asking the length door directly is one
  call, no invented tag, and the same helper the loop item uses.
- **Making every text a tagged value word first** (waiting for **L11.1**). The tag would fix this row's family as
  a side effect, and it would also need the four bare-payload doors of **Gap R.324** and the answer-plan's name arm
  of **Gap R.406** to be finished first. The condition road's bug is answerable today with a door that exists, and
  a wrong answer at exit 0 does not get to wait for a representation change.
- **Refusing `if <non-literal text>:` instead.** Exit 1 with prose where the reference runs a body is the
  **ADR 0166** inversion this house has ruled against many times: honest refusal is for answers the backend cannot
  build, not for answers it builds and reads wrongly.
- **Deleting the drifted assertions to get green.** The family's controls (a call's number, a call's container, a
  mixed-kind body, a loop item, a pair-supplied parameter) are pinned in the same tables precisely so that "the
  truth door now answers everything" is a claim the suite can falsify.
- **Letting the guard keep the environment it was born with** (Gap R.410). The obvious defence — "only run the
  artifact mode serially, and never with `-run`" — is a rule a person has to remember. An artifact is written by
  the one process that asked for it, or the artifact is not evidence.

## Measurements (taken, not remembered)

The family is the 28 sources these two test files carry, each asked of the pinned CPython, with a build of the
parent commit (`b552b8d`) beside a build of this one:

| | parent `b552b8d` | this commit |
|---|---|---|
| agree with the reference (bytes + exit) | **16** | **28** |
| newly answering | — | **12** |
| regressed | — | **0** |

A second, deliberate control family — 30 truth programs the door must **not** reach (a call's number, a call's
container, a mixed-kind body, an unrolled loop item, a pair-supplied parameter, `and`/`or`, an ordering condition,
the literal fold) — agrees with the reference on 28 of 30 on **both** builds, the two exceptions being pre-existing
refusals rather than this row: the mixed-kind parameter (**Gap R.409**) and `bool(<container>)` (**Gap R.131**).
Byte-for-byte identity between the two builds on those two rows is what attributes them.

Artifacts: `tools/recmerge` wrote **28** entries for this family from CPython (record `8884` → `8912`), and one
compiled-vs-record row **arrived** rather than went paid (`print(bool([1]))`, ledger `18` → `19`), which is the
ratchet's other face — a refusal a case has newly started asking about becomes a named row instead of a skipped
test. `pkg/lang`'s ledger stayed at 318 rows only because Gap R.410's fix stopped a filtered subprocess from
rewriting it.
