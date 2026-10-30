# ADR 0474 — a membership needle that IS a slot read asks the pair the equality door already computes

**Status:** Accepted
**Date:** 2026-11-01
**Decides:** roadmap **Gap R.232** (a membership test whose needle is a slot of a tagged container refuses, and its refusal names no construct)
**Files:** **Gap R.582** (a DICT literal's slots describe nothing, so a needle read out of one keeps one word — it answers `False` where the reference answers `True`, and answers `False` where the reference raises `TypeError: unhashable type: 'list'`), **Gap R.583** (a LIST literal whose element is a slot read of a call's answer is not claimed as a container whose slots describe themselves, so `xs[0] in xs` answers `False` where the reference answers `True`)
**Keeps open:** **Gap R.212** (a needle whose kind is a run-time fact the source never settled — `pick`'s two-armed answer), **Gap R.83** (the same operand at the equality door), **Gap R.146** (the one-word position, which is what still refuses when the haystack is a container the SOURCE spells), **Gap R.82**, **Gap R.563** (a refusal sentence handing the reader Go where the program belongs)
**Related:** ADR 0232 (one question asked once — the rule this row is an instance of), ADR 0247 (the equality door's `(payload, tag)` pair), ADR 0323/ADR 0324 (the membership door over a container the program built, and its needle), ADR 0332 (a literal's element that is a read: the same trade, at the builder), ADR 0334 (a call's container answer materialised under a rooted slot — the road the needle's own base takes), ADR 0459 (an object the run time builds is given a name, and the roads that read a container variable read it too), ADR 0456 (a container the source spells asks the door that builds it), ADR 0249 (a refusal names the construct, not the machinery), ADR 0166 (exit 2 is our bug — the reason the pair is built in a scratch builder), ADR 0214/ADR 0211 (a trap is the program's, and its exit code is 3), ADR 0187 (a payload never travels without its tag), ADR 0302 (the two witness legs)

## The rule

**A slot read has one answer to "what kind is this?", and every door that asks the question asks the same door.**
The membership test and the equality test ask one question of the same two words — *is this value one of the
values those slots hold*. The equality road has answered it with the object's own `(payload, tag)` pair since
**ADR 0247**; the membership road asked only the door that settles a kind from the SOURCE (`taggedOperand`) and,
when that failed, refused. One question, two answers, is **ADR 0232**'s defect, and — because the refusal was
printed with `%s` over an AST node — **ADR 0249**'s as well.

## Context

```
$ gustyc --eval 'def words():
      return ["hi", "ho"]

  print(words()[0] in words())'
gustyc: jit: codegen: testing &{%!s(*lang.Call=&{0x4000… [] … list[str]}) …} against the container
        "__cans10" whose slots describe themselves needs a needle whose kind the compiler can prove …
$ echo $?
1

$ python3 -c 'def words():
...     return ["hi", "ho"]
...
... print(words()[0] in words())'
True
```

Three things were wrong with that, and only the first was the missing feature:

1. **The refusal was a limitation the compiler does not have.** `xs[i] in xs` refused while `xs[i] == xs[j]`
   answered, over the same container, with the same pair available from the same door (`comparePair`).
   The haystack's own slots already carry tags — `rt_contains_tagged` takes `(haystack, payload, tag)` and
   compares the tag alongside the payload — so what was missing was not a representation but an edge.
2. **The sentence was unreadable by design.** `%s` over `n.L` printed `&{%!s(*lang.Call=&{0x4000…}) …}` — the
   needle's Go representation, with the compiler's own temp name (`__cans10`) where the container's name
   belongs. A reader, human or agent, was told neither what was asked nor what is missing. That is
   **Gap R.249**/`Gap R.563`'s disease at a fifth door.
3. **The two spellings of one program disagreed.** `w = words()` / `w[0] in w` and `words()[0] in words()` are
   the same program; the second had an answer-shaped hole where the first had a refusal, because the needle's
   base — a call that hands back a container — is materialised by **ADR 0334** into exactly the rooted slot the
   pair door reads.

## The decision

`mixedMembership` (in `pkg/lang/codegen_arith.go`) asks the door the equality road asks, and asks it in a
scratch builder:

```go
needle, tag, tok := g.taggedOperand(b, n.L)
if !tok {
	var scratch strings.Builder
	probe := n.L
	if redo, okAns := g.containerAnswerSubscript(&scratch, n.L); okAns {
		probe = redo                       // ADR 0334: the answer becomes the slot a variable occupies
	}
	if p, t, okPair := g.comparePair(&scratch, probe); okPair {
		b.WriteString(scratch.String())    // only a COMPLETE pair reaches the statement
		needle, tag, tok = p, t, true
	}
}
```

Four things this shape is load-bearing for, and each is a test rather than a claim:

* **One door, asked twice.** `comparePair` is not reimplemented, cloned or special-cased for membership. The
  guard is a table (`TestTheMembershipDoorNeverRefusesWhatTheEqualityDoorAnswers`) that runs the equality
  spelling and the membership spelling of the same read and fails if the equality door answers while the
  membership door declines.
* **The scratch builder.** A slot read emits its own bounds check, and the `IndexError` branch **terminates a
  block**. Emitting half a pair into the statement's builder and then falling back would leave instructions
  after a terminator — `llc` rejects the module, and **exit 2** is the code **ADR 0166** reserves for a compiler
  bug. The pair is appended only when it is complete; the trap rows (`xs[9] in xs`, `words()[5] in words()`,
  `d["b"] in d`, `xs[0][0] in xs`) raise the reference's class and sentence at exit 3, catchably, and the
  loop-shaped one (`while i < 3: print(i, xs[i] in xs)`) prints CPython's two lines before it raises.
* **The needle's base is asked first.** `containerAnswerSubscript` is consulted before the door declines, so
  the call-answer spelling and the bound spelling answer alike — **ADR 0459**'s rule ("name what the run time
  built, then let the roads that read a variable read it") applied one door further down.
* **A refusal stays a refusal, in the program's words.** Where the needle's kind is a run-time fact the source
  never settled — `pick`'s two-armed answer, **Gap R.83**/`Gap R.212` — the door still declines, and the
  sentence quotes the program: `testing pick(0) against the container xs …`. `exprSurface` replaced the `%s`
  of the AST node, and the refusal tables assert the absence of `%!s(`, `*lang.` and `__cans` — a claim about
  every refusal the door can print, not about the sample it was filed with. The capability registry's
  `lower.unsupported.membership-needle-unprovable` sample moved onto a still-refused program, so the machine
  surface cannot certify a door that no longer refuses what its sample says it refuses.

The untagged scan is left exactly where it was: when the container's slots do not describe themselves and the
needle's kind is not provable, the door reports "not handled" rather than answering, because a payload-only
scan is what makes `1 in ["1"]` come out true (**ADR 0232**'s collision). Widening THAT is **Gap R.582**'s row,
not this one — see *Why not the alternatives*.

## What the door buys, measured

Before/after was measured with a build of this commit's parent (`/tmp/gc_base` vs `/tmp/gc` over the same
sources, `python3` beside both). Every changed answer is a refusal → the reference's answer; **no program that
answered before answers differently**:

| program | before | after | CPython |
|---|---|---|---|
| `def words(): return ["hi","ho"]` / `print(words()[0] in words())` | exit 1, `%!s(*lang.Call=…)` | `True` | `True` |
| `w = words()` / `print(w[0] in w)` | exit 1 | `True` | `True` |
| `xs = [1, "a"]` / `print(xs[1] in xs)` | exit 1 | `True` | `True` |
| `xs = [1, "a"]` / `i = 1` / `print(xs[i] in xs)` | exit 1 | `True` | `True` |
| `xs = [1, "a", None, 2.5]` / `print(xs[2] in xs)` | exit 1 | `True` | `True` |
| `xs = [[7, 8], "a"]` / `print(xs[0] in xs)` | exit 1 | `True` | `True` |
| `xs = []` / `xs.append("q")` / `xs.append(1)` / `print(xs[0] in xs)` | exit 1 | `True` | `True` |
| `xs = [1, "a"]` / `i = 0` / `while i < 3:` / `print(i, xs[i] in xs)` | exit 1 | `0 True`/`1 True` then `IndexError` | same |
| `xs = [1, "a"]` / `print(xs[9] in xs)` | exit 1 | exit 3 `IndexError: list index out of range` | same class+message |
| `ints = [1, 2]` / `texts = ["zz", 1]` / `print(texts[0] in ints)` | exit 1 | `False` | `False` |
| `def pick(v): …` / `print(1 if pick(0) in xs else 0)` | exit 1, Go text | exit 1, `testing pick(0) against the container xs …` | `0` |

Witnesses:

* `pkg/lang/membership_needle_pair_test.go` — 33 answered rows (each also run through `PythonRun` in the case,
  so the expectation is read, not remembered), the same 33 asked of the RECORD leg (`goldenLookup` +
  `goldenStdout`: a missing entry fails), the tag-register IR row (the membership call's third argument must be
  `i32 %reg`, never a constant), the negative-control table (a needle the source settles, and a folded
  membership, still emit neither `rt_get_elem` nor `rt_tag_of` — the door cannot have widened into the fold),
  6 trap rows + catchability, the loop-blocks row, 3 refusal rows with the no-Go-text assertion, and the filed
  table that pins **Gap R.582**/**Gap R.583**/**Gap R.212**/`Gap R.146` with today's answer beside the
  reference's.
* `integration/membership_needle_pair_test.go` — 20 CLI rows against a live `python3`, 5 raise rows whose
  `Class: message` is read out of the reference per row (exit 3, empty stdout, no `codegen:` prefix, exit 2
  forbidden) plus their handlers, 3 refusal rows at exit 1, the `--verify-llvm` + `--emit-llvm` module table
  (verified, tag register, no global in an i32 operand), and the corpus program asserted line for line.
* `integration/programs/membership_needle_pair.gy` joined `conformanceStandalone()`: corpus 293 → 294, matrix
  279 → 280, parity-asserted 243 → 244, oracle `match` 229 → 230, `fail` 0, `drift` 0.
* **52 record entries** came in through `tools/recmerge` (**11031 → 11083**), every one read from the reference
  with the compiled leg cross-checked; the places that quote the count (`AGENTS.md`, `docs/operations.md`,
  `roadmap.md`'s Snapshot, `pkg/lang/leading_dot_float_test.go`) moved with it (**Gap R.204**).

## Why not the alternatives

* **Refuse less by comparing payloads when the needle has no tag.** That is how `1 in ["1"]` became true: a
  stored text is an `@str_tab` index and an integer payload can equal it. **ADR 0232** settled that a tag is
  not optional at a scan; the answer is to supply the tag the objects wrote, not to drop the question.
* **Extend the door to the container the SOURCE spells (a literal haystack, a dict literal's slots).** Measured
  first, then declined: the haystack side of those programs is a different road (`ADR 0456`'s build-the-literal
  door, and the untagged `rt_dict_has` scan), so opening it here means editing the fold's contract, which is
  where the exit-2 class lives (**ADR 0192**). The measurement is kept as **Gap R.582**/**Gap R.583** with each
  leg's answer written into the row, and the two `False`-where-the-reference-says-`True` rows are pinned in
  this commit's filed table so a future cycle cannot ship the same answer without owning the row.
* **Teach `taggedOperand` about subscripts instead.** `taggedOperand` answers "what kind did the SOURCE settle";
  a subscript's kind is the object's fact, and folding it in would make the two predicates one question again
  while losing the negative control (the fold's shapes would start paying for a door they never needed).
* **Print the needle's type name when refusing.** That is what the row was filed about (**ADR 0249**, and again
  as `Gap R.563` at the copy road). The sentence quotes the program because a diagnostic is an agent's input.
* **Add a run-time "membership asked" helper.** `rt_contains_tagged`, `rt_dict_has_tagged` and
  `rt_set_contains_tagged` already take the pair; a fourth helper would be a second answer about what a slot
  holds (**ADR 0232**), and it would have to be kept in step with the three that print, compare and hash.

## Consequences

* A membership test over a container whose slots describe itself no longer distinguishes between a needle the
  source kinds and a needle the object kinds; both arrive as a pair, and the tag decides the arm.
* The residue is written down where a reader looks for it: the capability registry's sample is a program that
  still refuses, `docs/operations.md`'s capability table is the registry (`go run ./tools/captable -check`
  asserts it), and the four shapes the door leaves are rows with IDs, evidence and the reference's answer
  beside each.
* The scratch-builder pattern is now stated in the code where the next pair-hungry door will find it: any road
  that asks `comparePair` mid-statement is asking something that can end a block.
