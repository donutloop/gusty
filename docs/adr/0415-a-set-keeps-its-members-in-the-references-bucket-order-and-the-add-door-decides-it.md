# A set keeps its members in the reference's bucket order, and the add door is where it is decided

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.421** (a set's print order puts the void last). Files two rows it
measured beside itself and did not buy: **Gap R.423** (the set built from a literal writes its members with
`rt_set_elem` and no tag, and keeps the order the source named them in) and **Gap R.424** (members whose buckets
collide, and a set that outgrows the first table, where the reference's probe sequence and its rehash decide).
It also files the arm of its own rule the payload cannot answer for — **Gap R.425** (a TEXT member sits after the
numbers here and at a salted hash's mercy in the reference) — and corrects a pin in
`integration/mixed_container_test.go` that had `"want CPython's"` above `{1, 'a', None}`, bytes eight unpinned
`python3` runs of that program do not produce.
**ADR 0412**/**ADR 0413**'s residue — **Gap R.420**, **Gap R.422**, **Gap R.417**, **Gap R.146**, **Gap R.174** — is
untouched here, and **Gap R.420**'s fix is an in-flight change that owns **ADR 0414**: that number is left unissued
in this commit on purpose, so the two rows cannot be confused. One commit, one feature: the set's add door in
`pkg/lang/codegen_ir_set.go`, and the two test files.

## Context

**ADR 0413** paid the alias's dict and set record and left three rows behind it. One of them looked cosmetic and was
not:

```
s = {1}   s.add(None)   print(s)     #  {None, 1}   reference
                                     #  {1, None}   this backend, at exit 0
s = {2}   s.add(None)   s.add(1)     #  {None, 1, 2}   reference
                                     #  {2, None, 1}   this backend
s = {2}   s.add(True)                #  {True, 2}   reference
                                     #  {2, True}   this backend
```

Both members were present and both carried their tag — the notebook and the pair rule of **ADR 0232**/**ADR 0410**
were doing their jobs. What was missing was the *order*: the reference's `set` is an open-addressed table whose bucket
is `hash(x) mod (table size)`, and the sequence every reader walks **is that table read in bucket order**. The void's
hash lands it in bucket 0, ahead of every small number, which is why the reference spells the set `{None, 1}`. An
order is not cosmetic in this language: `print` and `for x in s` are two doors over one array, and the reference's
answers — the record's `repr` included — are that array's sequence. Two of them disagreeing, or either disagreeing
with the record, is the same class of debt as a payload printed under the wrong label.

The route this row first tried was the printer, and **ADR 0258**'s one-printer-one-rule rule is exactly the reason to
reject it. Sorting at print time fixes `print(s)` and leaves `for x in s` walking the array, so the same set would
answer two different sequences to two doors written three lines apart; and every future reader of a set — `sorted`'s
input, a join, an equality that renders — would inherit the same choice to make again. The order is a property of the
container, so the container holds it.

## Decision

**The set's array is kept in the reference's bucket order by the door that inserts, and the bucket rule lives in one
helper that both halves of that door ask.**

1. `@rt_member_bucket(i32 %v, i32 %t)` answers where a pair sits: `0` for the void (its hash lands it in the first
   bucket of the reference's table), the payload's floor-mod 8 for an int or a verdict — an int hashes to itself, with
   CPython's documented remap of `-1` to `-2` so that `-1` never wears the value the C API reserves for an error — and
   `8` for anything the payload cannot answer for. `8` is the "not participating" answer rather than a bucket: a
   text's hash is salted per process (**PYTHONHASHSEED**), so no function of the payload can predict where the
   reference will put it, and a member like that keeps the position the insertion gave it, after the numbers. That is
   the honest answer to an unanswerable question, and it is why `s = {1, 2}` / `t.add("a")` still prints `{1, 2, 'a'}`
   as the record says it should.
2. `@rt_set_add_tagged` inserts instead of appending. It asks the incoming pair's bucket, walks the array to the first
   member whose bucket is strictly greater, and shifts the tail up one to make room — **payload and tag together**,
   **ADR 0410**'s pair rule, because a shift that moved the payload alone would leave every shifted member wearing
   the kind of the slot it landed on. Equal buckets keep insertion order, which is the order the reference's probe
   sequence leaves them in for the sets where no two members collide.
3. Nothing is sorted at print time. `rt_set_print`, `rt_set_print_mixed` and the member walk keep reading the array
   from 0 to `len`, and the loop door over a set reads the same array — so print and `for` cannot be given different
   answers about one set, which is the invariant this ADR exists to keep. The loop case is pinned for exactly that
   reason: `s = {2}` / `s.add(1)` / `s.add(None)` / `for x in s: print(x)` prints `None`, `1`, `2` in that order, the
   reference's order, in the same table as its `print(s)` twin.
4. Dedupe is unchanged and is still the pair question (`rt_slot_matches`), so the ordering did not add a second
   membership rule: `s = {1, 2}` / `s.add(1)` is still one member and `len` still says 2.

## Consequences

- 9 programs of the family moved to the reference's answer, 65 controls held, and **none moved away from the
  reference** (89 programs asked of the pinned CPython with a build of the parent commit beside this one; the full
  split is in the row's Evidence cell). The void's set, the verdict's set, the loop over them, and the alias spellings
  of all of them are the moved ones.
- `tools/recmerge` wrote **10** entries from the reference (record **9084 → 9094**) and refused none: the compiled leg
  agreed on every source, which is the condition the recorder insists on. The record's own counts were re-read from
  the file, not remembered, in `AGENTS.md`, `docs/operations.md` and the `roadmap.md` snapshot.
- The order is now a fact about the object rather than a fact about the call site, so a future `sorted`, join or set
  comparison inherits it instead of reinventing it — and inherits **Gap R.424**'s debt too, which is where it belongs.
- `integration/mixed_container_test.go` carried `{1, 'a', None}` under the label *"want CPython's"* for
  `s = {1, "a", None}` / `print(s)`, and its oracle half carried a comment excluding set printing "on purpose",
  because comparing a hash order to an insertion order was not a comparison. Both were true of the backend that
  inserted. The pin now reads `{None, 1, 'a'}` — what the reference prints with its salt pinned, which is how every
  reference leg of this repo runs it — and three set-print programs joined the oracle half, because the two orders
  are now one order and the reference's answer is reproducible. A wrong answer's pin is a debt, not a witness
  (**ADR 0166**), and a comment that forbids a comparison is worth re-asking once the reason for it is gone.
- The rule is stated twice in two languages — once in the IR helper, once as prose in this file — and never in a
  second Go map. The owed rows pin the boundary of what the rule covers, so the day the table grows past 8 buckets or
  a collision is walked properly, the pinned bytes move and the rows close rather than silently rot.
- No new language surface, so no `docs/language.md` claim changed beyond what the reference already answered; no new
  flag, so `docs/operations.md`'s interface section is untouched apart from the record's count.

## Alternatives rejected

- **Sort in the printer.** Fixes `print`, not `for`; makes the order a per-call-site decision every future reader has
  to re-make; and would have this commit's own loop program fail its table.
- **Void-first only.** A special case that reproduces the four bytes in the row's title and nothing else: `s.add(3)`
  after `s.add(1)` still came back `{3, 1}`. The row's bytes would have been paid while the rule stayed absent, which
  is how a wrong answer gets a second spelling.
- **Sort the literal road in Go at compile time.** Reaches the same bytes for literals, but puts a second copy of the
  rule in the compiler where the runtime copy already lives, and the set-literal road is the target of the
  dead-heap-object pass (**ADR 0143**), which recognises `rt_alloc` + `rt_set_elem` sequences. Filed as **Gap R.423**
  instead — the literal road needs the tag store as much as the order, and the two want one door, not one compile-time
  echo.
- **Simulate CPython's perturb probe (`j = (5*j + 1 + perturb) mod size`) and its resize/rehash now.** Correct for
  collisions and large tables, and it is a table implementation, not a rule one helper can hold; measured and filed as
  **Gap R.424** with the reference's bytes and this backend's beside them, both reachable from the pinned suite.
- **Compute a hash of the text at the add door.** Tempting and wrong: it would be *this* runtime's hash, not the
  reference's, and a second string-hash table in the arena to keep in step with `@str_tab`. The reference's hash is
  salted per process, which is exactly why the answer is 8 ("not participating") today and a row (**Gap R.425**)
  rather than an invention.

- **Reorder the array at print time only when the tags are all int.** Two orders per set, chosen by a kind test the
  printer has no business owning (**ADR 0232**'s lesson: the slot answers for itself).
