# A higher-order recursion inherits the entry it was cloned for, rather than asking the self-call to name it again

**Roadmap:** Gap R.335 (closed here — `run(n - 1)` omitted a defaulted callable parameter and the clone's audit called that an unfilled position) ·
L11.7 (functions are values that compile) · ADR 0364 (the recursive clone) · ADR 0371 (an entry tuple read from the body rather than the call site, paid for every call outside a body) ·
ADR 0345 (an entry is a symbol, not a callable value) · ADR 0273 (a door may decide only a fact the AST states — the reference's required-argument `TypeError` is not the pass's to answer) ·
ADR 0379 (the pass's decline reaches the terminal beside the ordinary refusal) · ADR 0381 / ADR 0384 (preceding cycles in the same name, frame, and value-tagging families)

## Status

Accepted. Landed 2026-10-20.

## Context

ADR 0371 taught the callable-defunctionalization pass to read an entry tuple from the body: an external call may leave a defaulted parameter out, because the parameter's default says which function the clone will call. The recursion road kept its own audit and demanded that a call of the higher-order function from inside its own body fill every position:

```python
def run(n, f=lambda x: x):
    return 0 if n == 0 else f(n) + run(n - 1)

print(run(3))       # CPython 6    this backend: exit 1, "f" is not a callable it can build
```

The recursive call had one argument although `run` has two parameters. A clone is selected by a tuple of entry symbols, and the old audit refused to guess the second member. The omission was not a hole in the plan: the clone already knew which function its `f` slot had arrived as. It had just been taught to ask the self-call a question the self-call never needed to answer.

The same program written with an explicit second argument was refused for a related reason. The second lambda is structurally the same function as the clone's entry, yet parsing made it a different AST node:

```python
def run(n, f=lambda x: x):
    return 0 if n == 0 else f(n) + run(n - 1, lambda x: x)
```

The audit demanded that a called parameter literally be handed back by its own name. That is enough for a fixed entry, but it cannot express the ordinary spelling of repeating the same closed lambda.

## Decision

**A recursive call may omit, name, or repeat the entry; it may not choose a different one.**

1. **The self-call's entry tuple is the enclosing clone's tuple.** For each called parameter:
   - if the recursive call supplies the parameter's own name, the entry is unchanged;
   - if it omits the slot, and the parameter has a default, the clone supplies its own entry;
   - if it supplies a function expression whose canonical source spelling is the clone's current entry, it is still the same entry.
   The argument is then replaced by the pass's ordinary placeholder, because a clone dispatches on the name it was minted for and never reads the called slot.
2. **A required omission remains the reference's error.** `def run(n, f): ... run(n - 1)` is `TypeError: run() missing 1 required positional argument: 'f'`, not a clone-entry question. Such a self call does not register the function as higher-order. The front-end arity road owns the message, so the compiler never invents a placeholder for an argument the reference insists upon.
3. **An omitted ordinary parameter still needs its default.** A non-called, omitted slot is filled from the parameter's default exactly as ADR 0371 did. If it has no default, the recursion is not eligible for the clone and the ordinary checker sees the missing argument.
4. **Entry identity is the source shape, not pointer identity.** `callableCanonicalExprText` renders an entry expression without source positions. Two separately parsed `lambda x: x` nodes are one entry; `lambda x: x` and `lambda x: x + 1` are not. A declared function's name remains its own entry.
5. **A different entry still declines.** If a recursion supplies a different function, the clone would have to choose between the entry it was minted for and a new one mid-recursion. The pass says which parameter and line it declined, and the recursion stays in the ledger rather than becoming a wrong answer.

## What answers, and what still refuses

| Shape | CPython | This backend after ADR 0385 |
|---|---:|---|
| `def run(n, f=lambda x: x): return 0 if n == 0 else f(n) + run(n - 1)` | `6` | `6` |
| the same with `run(n - 1, lambda x: x)` | `6` | `6` |
| the same with `run(n - 1, f)` | `6` | `6` |
| `def run(n, f=inc): ... run(n - 1)` | `9` | `9` |
| two callable parameters and one ordinary defaulted slot omitted in the self call | `13` | `13` |
| required `def run(n, f): ... run(n - 1)` | `TypeError` missing argument | front-end arity refusal at exit `1` |
| `run(n - 1, other)` | recursive result with the new callable | refuses by naming `f` and the recursion row |
| `run(n - 1, "x")` | `TypeError` at the call | refuses rather than guessing a callable |

The CLI's `--callables` account says the truth about an accepted recursion: the program is rewritten, has one clone and one recursive site, and reports the external omitted parameter. The old test that expected the pass to say “left a slot out” is replaced by a test that expects it to report the inherited slot.

## Machine path

The callable report's `recursive_sites`, `omitted_arguments`, `clones`, and `symbols` fields are unchanged in shape; the recursion road now populates them for these programs rather than declining before a report can. `--json` continues to classify an explicit different callable as `lower.unsupported.indirect-call`, quoting both the ordinary refusal and the pass's more precise account. No new capability code was needed: ADR 0379's quoted decline already gave the existing refusal the pass's reason.

## Consequences

- The entry tuple is now read from the clone for the recursive position and from the program for the external call position. The rule is no longer “every call writes every parameter,” but “a self call may not change the clone's entry.”
- A separately parsed copy of a closed lambda no longer forces a refusal merely because it is a distinct node. Canonical textual comparison is deliberately conservative: it cannot turn a different source shape into an entry match.
- Four new answer programs are on the record, written by `tools/recmerge` from CPython and cross-checked against this backend.
- The row closes without making the pass answer a required-argument error. A compiler may not turn the reference's `TypeError` into a working recursive program.

## Alternatives rejected

* **Fill an omitted called slot from its default.** An ordinary call omits a default at its call site; a recursive call omits a value the *frame* already carries. The recursive entry is the enclosing clone's symbol, not the parameter's default expression.
* **Fill every omitted slot with the placeholder.** A required parameter has no default and the reference rejects the call. This would have produced an exit-0 answer to a program CPython never runs.
* **Compare AST pointers for entry equality.** Two parses of the same text produce distinct nodes. It rejects exactly the “repeat the entry” spelling this row is meant to pay.
* **Mint a new clone for every distinct function supplied by a self-call.** That gives the recursion road dynamic entry selection — a callable would become a value in the recursive frame — and needs the tagged value word that ADR 0345 deliberately declines to invent. The honest boundary is one entry per clone.
* **Let the generic indirect-call refusal answer required omission.** The reference's message is about missing arity, not an uncallable value; the front-end checker already has the better sentence.
