# ADR 0437 — a refusal names the rule it is missing: the container door splits into a key door and an element door, and no emitted sentence offers a way to run that ADR 0302 retired

## Status

Accepted, 2026-10-28. Closes **roadmap Gap R.288** and files, unfixed, the four shapes that the same
probe walked past: **Gap R.485** (a set literal accepts a member the reference calls unhashable), **Gap
R.486** (a container held in a NAME, written as a literal element, answers the slot), **Gap R.487** (a
COMPREHENSION written as a literal element does the same), **Gap R.488** (the same shape one level deeper
puts a compile-time global in an operand position and `llc` rejects the module — exit **2**, the code
**ADR 0166** reserves for a compiler bug).

Read after **ADR 0302** (the AST interpreter is retired; `--interp` is a usage error), **ADR 0249** (a
refusal names the missing half), **ADR 0232** (one question, one reading), **ADR 0166** (the exit-code
contract), **Gap R.193** (the witness vocabulary polices the documents, not the emitter) and **Gap
R.116** (diagnostics are prose a programmer acts on, not a log line).

## Context

One program, three answers on record:

```gusty
d = {[1]: "x"}
print(d[[1]])
```

| leg | answer |
|---|---|
| CPython 3.12.3 | `TypeError: unhashable type: 'list'` |
| this backend, `4fdb87c` | refused — and the sentence ended **"…or run it interpreted"** |
| the record | no entry; `tools/recmerge` refuses to write one for a refusal that has no answer to compare |

The declining itself was honest. A compiled dict cannot be keyed by a container: an entry is filed by the
hash of its key, this backend has no hashing rule for a container, and so the pair would be filed under
whichever handle the allocator happened to hand out — recoverable only by the program that stored it. What
was not honest was the last five words. **ADR 0302** deleted the AST interpreter, took its entry points
with it, and made `--interp` a usage error; there is no way left to "run it interpreted". The sentence was
therefore a promise the toolchain cannot keep, aimed at a reader who cannot tell it is a promise.

It was also invisible to every guard in the tree. `pkg/lang/witness_claim_test.go` reads the tests, the
CLI's own text, the conformance registry and the agent-facing documents for phrases like "both engines" —
and none of those files contains what `pkg/lang` prints. **Gap R.193** named exactly that hole. Grepping
every string literal in the production files of `pkg/lang` and `cmd/gustyc` for the retired engine found
three sentences, not one:

| file | the sentence said | what was wrong with it |
|---|---|---|
| `pkg/lang/heapargs.go` | `…Build that inner value separately, or run it interpreted` | offers an engine that does not exist, and one sentence recited **both** rulings whether the position asked about a key or an element |
| `pkg/lang/semantic.go` | `the interpreter uses the built-in and the compiled backend uses that definition` | explains a two-engine disagreement that no longer happens; the refusal it annotates is still right, and still fires |
| `pkg/lang/schema.go` | `…shared by the interpreter's heap objects (obj.kind → obj.tag)…` | describes a `valueTag` consumer that is gone, in the JSON schema an agent reads to learn the ABI |

And one sentence was untrue in a second way, found by walking the comprehension road to the same door. At
`4fdb87c`:

```
$ gustyc --file - <<< 'print([{[1]: 2} for x in [1]])'
gustyc: jit: codegen: codegen: a compiled container cannot hold a dict literal yet; a dict keyed by a
container has no hashing rule, and an element nested deeper than every inner element can be tagged would be
printed by handle and compared by address (roadmap L11.1). Build that inner value separately, or run it
interpreted
```

`codegen:` twice — the comprehension road wrapped the door's error in a prefix the door had already
written. Same class of defect as the retired offer: a sentence about the toolchain that is not true.

## Decision

**1 — One door per missing rule, and the door says which rule.** `nestedContainerErr` is now a dispatcher
over two sentences that share the family stem `a compiled container cannot hold %s`:

- the **key** door — `…cannot hold %s as a key yet: a dict finds an entry by hashing its key, and this
  backend has no hashing rule for a container, so the entry would live under whichever handle the
  allocator handed out and nothing but the program that stored it could find it again (roadmap L11.1). Key
  the dict by a value that has a hash — a text, an integer, a float, a verdict or the void — and keep the
  container in the value`;
- the **element** door — `…cannot hold %s yet: a slot of a compiled container carries one word and the tag
  that says what that word is, and this element nests deeper than any tag the compiler writes, so the word
  would be the inner object's handle — printed by handle and compared by address (roadmap L11.1). Build that
  inner value separately, one level shallower, so every element is a value a slot can tag`.

Both keep the stem, because a reader who has met one of these refusals recognises the shape of the answer,
and both keep a roadmap row in the sentence, because the sentence's job is not only to decline but to say
where the decline is owned.

**2 — The element door walks into what it declines.** `exprWantingContainerKey` descends through nested
literals (bounded at depth 3, the depth the rulings themselves reach, **Gap R.75**) and, if any dict it
passes has a key with no hash, reports the **key** rule about **that** expression. So
`print([{{[1]: 2}}])` names `[1]` — the object the reader has to change — where a plain element sentence
would have sent them off to fix a tag that was never the problem. This is **ADR 0232**'s one question asked
of the object rather than of the call site.

**3 — The prefix is written once.** The comprehension road returns the door's error rather than wrapping it,
so a refusal prints `codegen:` once.

**4 — The retired-engine sentence list is data, and it is asked of the emitter.**
`pkg/lang/witness_emitter_test.go` walks the string literals of every non-test file in `pkg/lang` and
`cmd/gustyc` and fails on any of the phrases in `pkg/lang/testdata/witness-banned-phrases.txt` (the engine
claims the document guard already used) and the new
`pkg/lang/testdata/witness-emitter-banned-phrases.txt` (the offers to run it interpreted, the `--interp`
flag, the retired entry-point names). Exemptions live in `pkg/lang/testdata/witness-emitter-ledger.json`,
one row per tolerated string, and **a row that matches no emitted literal fails the case** — an exemption
cannot outlive its sentence and cannot be added to make a run green. The ledger ships empty: nothing needed
tolerating. Banned phrases stay out of Go source, including out of the tests that forbid them, so no guard
can ever be satisfied by deleting its own test (**Gap R.193**'s reason for the same rule).

**5 — The stale-mention fixes are the same commit as the door,** because the guard that catches them is
the guard this row adds, and a guard landed beside the sentences it certifies is the only arrangement where
a green run means anything.

**6 — Where the prose moves, the machine path moves with it.** The two doors are two **capability codes**,
`lower.unsupported.container-key` and `lower.unsupported.container-element`, registered in
`pkg/lang/capability.go` with their sample, markers, suggestion and owning rows. Both refusals were
`"capability": null` before this commit, so an agent planning a compilation could only match on the wording — the
thing **ADR 0346** exists to prevent — and one code covering both shapes would have reproduced this row's original
sin in JSON: a branch that cannot tell "re-key this dict" from "flatten that value".

**7 — A document column had been selling the same impossibility for twenty cycles.**
`docs/operations.md`'s capability table has a *Workaround* column, and eleven of its rows told the reader to use
the `interpreter` or to "run it on the record". Neither is a thing that can be done: one engine, and the record is
an answer on record, not a runner. The witness guard missed it because its phrase list is written for prose claims
("both engines agree"), not for an imperative in a table cell. The eleven cells now say what to write instead, and
the rule is printed above the table.

## Consequences

- The refusal paths are `nestedContainerErr` (the dispatcher), `nestedContainerKeyErr`,
  `nestedContainerElemErr`, `dictLitWantsContainerKey`, `dictKeyHasNoHash`, `exprWantingContainerKey`, and
  the call sites that ask them: `heapElemKind` and `heapDictFrom` in `pkg/lang/heapargs.go`, and the three
  comprehension roads in `pkg/lang/codegen_comp.go`, `pkg/lang/codegen_comp_nested.go` and
  `pkg/lang/codegen_comp_setdict.go`. The change is **refusal-only**: every program that
  compiled still compiles to the same IR, and no IR is emitted on a refused path at all. The one
  non-refusal edit in `semantic.go` changes a diagnostic's wording, not its condition; the `schema.go` edit
  changes a `description` string, not the schema's shape.
- Exit codes are unchanged and pinned: a refusal is exit **1** with `phase: "codegen"` and `ok: false`, and
  the `--json` diagnostic's `error` field carries **the same sentence** the human sees — one sentence, two
  consumers (**ADR 0166**, **ADR 0006**). The `capability` member beside it is non-null for both doors now, and
  `gustyc --schema`'s `capabilityRefusalCode` enum grew the two members — the enum is spliced out of the registry
  at init, so there is no second list of the fact to drift, and `docs/operations.md`'s code table is generated by
  `go run ./tools/captable` and compared against the registry by `pkg/lang/capability_docs_test.go` (**ADR 0346**).
- Four new measured rows, and one rule kept: none of their sources is on the record, because
  `tools/recmerge` refuses a source whose compiled bytes differ from the reference's. The cross-check
  earning its keep is the reason those shapes have rows instead of records.
- `pkg/lang/testdata/interpreter-golden.json` grows by 8 entries (9878 → 9886), all of them **controls**:
  the hashable keys and the shallow nested elements that answered before and must answer now. A missing
  record fails a case, so pinning the controls on the record is what stops the door from widening.

## Alternatives rejected

- **Delete the "or run it interpreted" clause and stop there.** A refusal that says only "not supported"
  tells the reader nothing about which half is missing; **ADR 0249** already decided that a decline names
  the missing rule. The clause carried two rulings at once, and after deletion the reader would have to
  guess which one applied.
- **One sentence, with a conditional clause inside it.** Rejected: it is how the row started, and it is
  untestable — no case can assert that the sentence said the *relevant* half, because it always says both.
  Two doors make "the element door wore the key door's hat" a failure.
- **Point the sentence at the ADR number instead of the roadmap row.** Rejected: **ADR 0302** retired the
  thing the number pointed at, and the roadmap row is what an agent can look up and act on; the row is
  already the vocabulary every other refusal in `pkg/lang` uses.
- **Reuse the document phrase list verbatim for the emitter.** Rejected: the document list contains phrases
  that legitimately appear in compiler prose about the retirement itself (`ADR 0302` retired…), and the
  emitter list needs the offers (`run it interpreted`, `--interp`, `EvalExpr`) the documents discuss by
  name. Two lists, one file each, one guard that reads both — with the marker list
  `pkg/lang/testdata/witness-history-markers.txt`, which licenses a phrase only on the line it sits on, so a
  literal can talk about the retirement without any clause of it being licensed by an honest neighbour.
- **Scan the emitted strings at runtime only (compile a program, read stderr).** Rejected as the primary
  check: it can only test refusals someone thought to run. The static scan covers every literal in the tree
  including roads no test reaches; the per-refusal assertions in
  `refusal_names_the_missing_rule_test.go` are what check the sentence's *content*.
- **Leave the stale `semantic.go` and `schema.go` sentences for a later cleanup.** Rejected: they are
  agent-facing text (one is in the JSON schema), and the guard added here would fail the next cycle for the
  absence of an edit nobody owed — a guard that ships red is a guard that gets deleted.

## Note on this cycle's method (tool-trust)

Every "before" byte above was read off a build of `4fdb87c` in a clean worktree (`/tmp/gusty-base`), not off
memory, and every "after" byte off a build of this tree; CPython 3.12.3 was run beside each program rather
than quoted. Two things were caught by measuring instead of reasoning: the comprehension road's doubled
`codegen:` prefix (which no reading of the door alone would have surfaced — the wrap lives in a different
file), and the fact that `print([[[1]]])` and `print([[{"a": [1]}]])` **answer correctly**, so a door that
declined them would have been a regression rather than a fix; they are in the control table for that reason.
