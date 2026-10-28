# ADR 0448 — a body that declares a name writes the MODULE's binding, and the checker asks before the printer

- **Status**: Accepted
- **Date**: 2026-10-29
- **Cycle**: the one after **ADR 0447** (which closed **Gap R.55**; row **Gap R.48** was the top of the Open queue)
- **Roadmap**: **Gap R.48** (`There is no global statement`) done. Three shapes the sweep found that this row
  does not own are filed as **Gap R.511** (a `global` written in a class body), **Gap R.512** (a declared
  global whose value the module's scalar word cannot carry) and **Gap R.513** (an unwritten global should
  RAISE NameError, not be refused at compile time). **L11.1** owns the tagged value word **Gap R.512** waits on.
- **Builds measured**: parent `c2240b4` (**ADR 0447**) and this commit, every byte from the gate's `-tags=llvm20`.

## Context

`global` lexed as an ordinary name, so `global count` was an expression mentioning a name called `global`, and the
resolver answered `undefined name "global"`. The idiom was unwritable in every spelling: the counter a helper bumps,
the accumulator a loop body fills, the flag a setup function sets, the name a method writes. A probe had been carrying
that fact since **ADR 0228** — `integration/programs/probe_global_statement.gy`, whose `global gz` the compiled backend
refused and which this cycle turns into a named refusal of a different shape.

The storage was already there. **ADR 0227** mints `@gy_mod_<name>` for a module-bound name a body reads without
binding it, and `storeModuleScalar` keeps main's bindings and those slots the same object. So the read half existed and
the write half did not — which is the shape of this decision: **the declaration is not a new storage, it is the
permission to write the storage that reads already use.**

Measured on the parent, the whole family answered the same sentence:

```
$ gustyc --eval 'count = 0
def bump():
    global count
    count = count + 1
bump()
print(count)'
gustyc: jit: 1 error(s) in source
  - error at 2:5: undefined name "global"        reference: 1
```

## Decision

1. **A declaration removes a name from the frame, and nothing else.** `enterBody` records what a body declares
   (`globalDeclsIn`, the walk `collectLocals` and `moduleBodyReads` already use) and DELETES those names from
   `funcLocals`. The read road in `value()` already answers "does this body own this name?" for `@gy_mod_<name>`, and
   the store road asks the same question — so removing the name from one table sends both roads to the module's slot
   without a new arm in either. The statement itself emits nothing where it is written: its whole effect is which slot
   the OTHER statements in the body write. A module-level `global` is legal and means nothing, and the reference agrees.
2. **A declared name is module state, so it is not a constant.** `moduleEnvFor`'s rule is that a body's bindings are
   local (**ADR 0220**) and therefore cannot make a constant mutable; the declaration is exactly the exception the
   reference makes, so a declared name leaves the fold table and enters `moduleSlotNames` as if its first write were
   scalar — the shape `def f():` / `    global y` / `        y = 5` / `f()` / `print(y)`, where no module-level
   assignment exists to ask about and the reference still prints 5.
3. **The VALUE records are demoted for a declared global; the KIND records are not.** `k = "a"` / `def setup(): global
   k; k = "b"` / `setup()` / `print(k)` printed the module's `a`: the record of "what was last bound here" only ever
   sees the module's own statements, and a body's write arrives after all of them. Dropping the frozen VALUE is what
   makes the answer honest about a write the compiler cannot see — and, for a text, what turns a stale answer into a
   refusal, because the only thing left to print is the intern index (**Decision 5**). Dropping the KIND records too —
   measured, and reverted — printed `1` for a declared global the module had bound to `False` and `(null)` for one
   holding a text: those records are the same ones that format an ordinary variable, and a declared global is entitled
   to them (**Gap R.22**'s signature is what their absence produces).
4. **The checker runs over the whole program, not over what gets emitted.** The reference's refusals are compile-time:
   `def f(x): global x` and `def f(): print(x); global x` are SyntaxErrors even when nothing calls `f`. Both of the
   reference's refusals are said, with its own words inside the sentence:
   `name 'x' is parameter and global`, and the pair this repo keeps apart —
   `name 'x' is used prior to global declaration` for a READ above the line, `name 'x' is assigned to before global
   declaration` for a BINDING above it. The walk is in **document order**, because that is the input the reference
   reads: this parser leaves many statement spans at zero, and a first draft that compared line numbers refused
   `while`, `main-like`, `read-only` and `global-then-local` — four programs the reference answers — while claiming each
   was "used prior", at "line 0". A diagnostic that prints a line number the parser does not have is the thing
   **Gap R.116** forbids, so the line appears in the sentence only when the parser knew it.
5. **Three shapes are refused rather than answered**, because each would print a handle at the exit code of success:
   a declared global whose writes spell two kinds (`reg = 0` then `reg = [1, 2]` printed `0`); a declared global whose
   value the module's int-sized word cannot carry (a fractional number, a container, a text with a claim or against
   one — `1` for a bool with no record, `(null)` for a text); and a declared global nothing ever writes, or which a body
   reads before any statement wrote — including a write the program GUARDS, which is what the ADR 0228 probe is:
   `if 0:` / `gz = 1` / `return gz` is a NameError in the reference and an answer of `0` here. Each sentence names the
   reference's own outcome and the row that owns the difference (**L11.1** for the word, **Gap R.513** for the raise).
   Only a write at the body's OWN level licenses a later read; a guarded write is a write that may never happen.
6. **A `global` written in a class body is refused.** The class road keeps what a body binds (**ADR 0418**) while the
   reference routes that write to the module; two readers, no writer between them, and the module printing a literal
   only the class body touched. Filed as **Gap R.511** rather than guessed at.

## Consequences

- The module's scalar word is the boundary of what this row answers: a number, a verdict and the void come out of it,
  and every other kind is **L11.1**'s. That is stated in the refusal rather than discovered by a reader — a program
  that would print `0` for `[1, 2]` stops with a sentence naming the tagged word.
- One slot per declared name, not two. A module-shape test asserts a body that never declares a name is given NO
  `@gy_mod_` storage for it (the frame slot is the rule and the declaration the one exception), and that a body that
  declares one reaches the module's slot rather than keeping an unread frame word behind it (**ADR 0351**'s defect,
  asked from the declaration side).
- `probe_global_statement.gy` keeps refusing, for a better reason: it used to fail on `undefined name "global"` and now
  fails on the read of a name nothing ever wrote, which is the reference's own `NameError`.
- The corpus is the witness: every pre-existing conformance program is byte-identical parent to child, and the two that
  move are this cycle's own program (parent refuses, child answers, child is byte-identical to CPython) and
  `probe_global_statement.gy` (parent: `undefined name "global"`; child: the NameError-naming refusal).
- `global` is a KEYWORD now. The corpus carries no program that uses it as an identifier, which is what the sweep
  measured; a program that did would find a parse error rather than a silent change of meaning.
- **The measured surface, said plainly.** A declared global ANSWERS a number, a verdict and the void — the counter,
  the accumulator, the countdown, the flag, the name a method or a nested body writes. It REFUSES a text (an interned
  index beside a length the slot cannot carry — even a text the module never bound), a fractional number, any container,
  a name whose writes spell two kinds, a name nothing writes or only a GUARDED branch writes, and a name declared in a
  class body. Every refusal exits 1 with the reference's own outcome inside the sentence and the row that owns it —
  **L11.1** for the word, **Gap R.511** for the class body, **Gap R.513** for the raise — and none of them exits 0 with
  a handle. Reading a module text from a body that does NOT declare it is answered; the refusal is about a body writing
  a text into the one-word slot.
- **`nonlocal` is this statement's twin and stays somebody else's row.** Re-measured on the way: `def outer():` /
  `    total = 0` / `    def inner():` / `        nonlocal total` / `        total = total + 1` answers `2` in the
  reference and `undefined name "nonlocal"` here. **Gap R.59** (`assert`, `del`, `nonlocal` are identifiers, not
  keywords) owns it, and one keyword table is the right fix for all four — this row deliberately did not half-land a
  second statement beside the one it was asked for.

## Alternatives rejected

- **A new storage class for declared globals.** Rejected: `@gy_mod_<name>` is the object the reads already resolve to,
  and a second one would be the two-slot defect this row then has to refuse.
- **A line-number-based "used prior" check.** Rejected on measurement, not taste: the spans are not there, and the
  draft refused four answerable programs while reporting "line 0".
- **Answering the uncarryable kinds with the identity/handle value, or with `0`.** Rejected: it is **Gap R.22**, and it
  is the reason the golden record is a reviewed artifact.
- **Pre-running the body to learn whether a guarded write happened.** Rejected: it runs the program to decide whether
  to compile it, and a body that prints, counts or mutates would be caught doing it.
- **Demoting the kind records with the value records.** Rejected on measurement: the module's own binding is what tells
  the printer whether to write `True`, `None` or `1`.

## Where the answer lives

- `pkg/lang/codegen_global.go` — `globalDeclsIn` / `programGlobalDeclNames` (who declares what), `globalFacts` /
  `globalSpelledKind` (what the writes say they are), `moduleBindsName` / `globalReadBeforeWrite` (whether the read has
  a write behind it), `checkGlobalDecls` / `globalDeclChecks` (the five refusals) and the sentences.
- `pkg/lang/parser.go` / `ast.go` / `token.go` / `fmt.go` — the statement, its node, its keyword and its printer.
- `pkg/lang/codegen.go` (`enterBody`, `globalNames`), `pkg/lang/codegen_module.go` (`storeModuleScalar`,
  `moduleEnvFor`, `moduleSlotNames`), `pkg/lang/codegen_assign.go` (the two record roads).
- Answers: `pkg/lang/global_stmt_test.go` (17 answer rows on the compiled leg and the record leg, 9 refusal rows, the
  module-shape rows, and the never-called-body row) and `integration/global_statement_test.go` (11 CLI rows re-asking
  CPython inside the case, one `cc` link-and-run row, and the same refusals read off the `--json` diagnostic with their
  exit code). `integration/programs/global_statement.gy` + its `conformanceProbes()` row carry the whole family — ten
  prints, byte-identical to the reference, the parent refusing it at exit 1. **18 record entries (10243 → 10261)** were
  written from reference/compiled agreements, none refused.
