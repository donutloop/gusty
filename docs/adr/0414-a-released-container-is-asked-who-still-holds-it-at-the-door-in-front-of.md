# A released container is asked who still holds it, at the door in front of the free list

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.420** (a rebinding releases a container another name still holds, and
the allocator hands that slot to the next object) and, with it, **roadmap Gap R.257** (rebinding one alias frees the
object the other still names) — the latter's pinned program was dead with `KeyError: 1` at exit 3 on the tagged parent
and prints the reference's `7 / 7 / 2` here, so it was promoted out of the debt table into the answered table, the
record and the corpus, which is what that table's own instruction says to do.

It files two rows it measured beside itself and paid nothing to: **Gap R.427** (a value that lives only in another
container's *slot* is still released, because the door asks the root table and a slot is not a root — the boundary of
this decision, stated here rather than smoothed over) and **Gap R.426** (`sorted` over a dict sorts the entry array's
first `len` words instead of its keys, found while writing this row's battery and byte-identical on the parent).

Numbering note: **ADR 0415** left this number unissued on purpose, saying the in-flight release-door change owns it.
This is that change. `ADR 0413`'s other residue — **Gap R.422**, **Gap R.417**, **Gap R.146**, **Gap R.174** — is
untouched. One commit, one feature: the release road (`emitFree` in `pkg/lang/codegen_gc.go`, `@rt_release` in
`pkg/lang/codegen_ir_arena_globals.go`) and the test files that witness it.

## Context

Three names, one dict, and the most ordinary statement in the language between them:

```
d = {"a": 1}   e = d   d = {"b": 2}
print(d)                #  {'b': 2}      both answer this
print(e)                #  {'b': 2}      this backend, at exit 0
                        #  {'a': 1}      the reference
print(e["a"])           #  KeyError: 'a' this backend — over a dict that never lost its "a"
```

`e = d` copies the handle: two names, one object (**ADR 0412**/**ADR 0413**'s point, paid at the notebook). The
rebinding `d = {"b": 2}` then did this, in this order, in the emitted module:

```
%f2 = load i32, i32* %_d          ; the object d currently names
call void @rt_free(i32 %f2)       ; push it on the free list — asks nothing
call void @rt_root_clear(i32* %_d)
%t7 = call i32 @rt_alloc(i32 2)   ; takes the slot straight back
store i32 %h3, i32* %_d
```

`rt_free` is a free-list push. It has no notion of who else is reading, so the next allocation took that slot and wrote
the second dict into it — and `e`'s handle, which is a slot *number*, now denoted the new object. Nothing was raised,
nothing refused, and the arena was perfectly consistent: the two programs differ only in which name the print reads, and
one of them was wrong. The list and the set answered both programs correctly all along, because the list road builds
the new object **before** releasing the old; the dict's road releases first, which is what localises the defect in the
release and not in the notebook.

The collector could not undo it, and the reason is worth writing down because it rules out the obvious fix (below).
`rt_gc` clears the marks, then walks the **free list** and marks every slot it finds dead (`2`) **before** it marks from
the roots — and `rt_gc_mark` only marks a slot whose mark is `0`. So an object that had been pushed stayed pushable
whatever a root said about it. The free list is not a suggestion.

## Decision

**A handle reaches the free list only through a door that first asks the collector's own root table.**

1. `emitFree` — the one road a program body uses to release a container — emits `call void @rt_release(i32 %h)` behind
   the same `icmp ne i32 %h, 0` diamond it always had (0 is slot 0, not an absence: the discipline
   `TestEmptyBindingNeverFreesHandleZero` exists for, asked now at the door that exists).
2. `@rt_release(i32 %h)` walks `gc.roots` / `gc.kinds` up to `gc_roots_used` — **the same globals, the same bound and
   the same kind byte `rt_gc` walks** — and returns without freeing if any entry holds `%h`. ADR 0273's rule: a fact
   asked twice is decided once, and "who is holding this" is one question with one table.
3. The kind byte is the gate, not the payload. A root whose slot holds an int — whose word happens to be `3`, which is
   a slot index — must not veto the release of slot 3. That is ADR 0181's precision, asked at the door as well as at
   the sweep, and it is tested by padding a program with twelve int roots whose words are the indices `0..12` and
   requiring the arena's own `headroom=` to be unchanged.
4. `emitFreeOld` clears the **rebinding's own** root entry first (`rt_root_clear %_d`, which tags the entry dead) and
   only then hands the handle to the door. Without that step the release vetoes itself: the slot still names the handle
   at that instruction. With it, the question is precisely "does any OTHER name hold this?"
5. A declined release is not a leak and not a lie. It costs the program one occupancy until the next sweep, and the
   sweep — which marks from the roots and frees what nothing reached — is the road allowed to decide liveness (ADR
   0181's precision, ADR 0350's growth). The churn cases make the claim observable rather than promised: 500 and 2000
   iterations each vend one container and hold another, and the collector's report has to say `exhausted=0` with
   `total_freed` non-zero, or the case fails.
6. Nothing else in the module may push. The collector's sweep pushes `free_next` inline (it always did), so after this
   change a compiled module contains **exactly one** `call void @rt_free(`, and it stands inside `@rt_release`. That is
   an IR-shape claim the suite reads off the `define` blocks, in both legs, including through `gustyc --emit-llvm`.

## Consequences

- The instruction order is now a claim the suite holds rather than a hope. `pkg/lang/releasing_a_shared_handle_test.go`
  pins the six positions from the module — `load %_d` → `rt_root_clear %_d` → `icmp ne` → `rt_release` → `rt_alloc` →
  `store %_d` — and each adjacent pair is load-bearing: clear-before-load releases an untested word, release-after-store
  frees the NEW object, alloc-before-release hands the freed slot straight to the new dict.
- `heap_live` is the number that absorbs a declined release, so `--gc-stats`'s `headroom=` is the witness for the cost
  side, and the report line is unchanged: no new field, no new flag, no schema change. The machine path is the same
  `gc` member of an `--eval`/`--file` `--json` payload (ADR 0179's tool channel).
- **Gap R.257** closes on the same door, and its `Blocked on` cell named a different mechanism — a refcount carried by
  the object. What paid it is a question asked of the names, because the names are what the root table holds. That
  difference is not cosmetic: it is exactly why an object held only by another object's slot is still released, which is
  filed as **Gap R.427** with the reference's bytes beside this backend's and both builds named.
- The refusals that were never this row's stay refusals: a container METHOD over a NAME (`t.remove(1)`, `e.items()`) is
  **Gap R.174**'s standing, and a text `discard`ed from a set of numbers is **Gap R.422**'s wrong answer. Both are still
  pinned as owed, in both files, with their bytes.
- The released-then-reused shape that made this defect *silent* in some programs (the alias read back the new dict
  without any raise) is why the row's DoD demanded an IR pin: two runs of the same source can differ depending on
  whether the freed slot is up for reuse, and an answer that depends on that is an answer a test suite cannot certify by
  output alone.

## Alternatives rejected

- **Let `rt_gc`'s root mark override the free list's dead mark** (i.e. fix it in the collector, where the inconsistency
  literally lives). Rejected: the slot would be marked live AND stay on the free list, and the next `rt_alloc` would
  hand a live object to a fresh container — trading a wrong answer for a corrupted heap. The marks are one pass's
  business; the free list is a queue of slots for reuse, and a slot cannot be both.
- **Rebuild the dict road to allocate before it releases**, the way the list road does. It reaches this row's bytes and
  leaves the disease: a rebinding that frees an object another name holds is wrong whichever order the two calls sit
  in, and the same release road serves eleven binding sites (`codegen_assign.go` × 6, `bindTaggedVar`, the `for` loop's
  variable arms). Fixing one caller's order while the door still asks nothing is the ADR 0249 mistake — a gate (or a
  road) and a door that disagree.
- **A refcount per heap slot, bumped on copy.** The right long-term answer (**Gap R.257**'s `Blocked on` names it, and
  **Gap R.427** wants it) and not this commit: it needs an increment at every door that stores a handle into anything —
  the tagged list/dict/set stores, the tag door, the instance-field road, the pair-binding road — and a decrement at
  every door that overwrites one, or the count is a new way to be wrong. The root table already exists, is already the
  collector's authority, and answers the shape every row in this family was measured in.
- **Skip the eager release entirely for containers** (let the sweep do all freeing). Honest and simple, and it gives up
  ADR 0350's reason for existing: a program that churns containers between safe points would grow the window to the
  1024-slot wall and report `exhausted=1` for a program with one live list. The declined release is the exception, not
  the rule — 1998 frees in the 500-iteration loop, identical to the numbers this commit's parent reports.
- **Scan the arena's live slots for the handle at the door.** Correct about containment and `O(slots × elements)` per
  release — a 1024 × 256 walk on every rebinding of every name. Filed as **Gap R.427**'s second option instead, where
  it belongs next to the shape it fixes.
- **Ask the payload instead of the kind byte** ("does any root's *word* equal this handle?"). That is the collector's
  conservative failure, which ADR 0181 exists to end: an `int` variable holding `3` would veto the release of slot 3,
  and every program with a small integer in scope would leak.
