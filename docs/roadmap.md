# Roadmap — pointers

This is a pointer mirror. The living plan for **Pyre** (the `gusty` repo) is split in two,
and both halves are authoritative for what they own:

- [`../roadmap.md`](../roadmap.md) — **the tracker.** The single source of truth for state:
  the status vocabulary, the measured snapshot, the component map, the prioritized open
  queue, and one row per item and per gap
  (`ID · item · status · path · ADR · evidence · free text · record`). The `Free text` cell is
  the item's own pre-tabulation wording, so the tracker reads on its own; the agent loop
  reads the queue and writes one cell per finished item.
- [`roadmap-details.md`](roadmap-details.md) — **the record.** The narrative: how each gap
  was found, what the wrong answer looked like, which measurement settled it, what was
  rejected. Statuses never live here.

The plan has a spine of two phases. **Phase 11** settles the value model (one tagged word,
read by both backends) and everything representation-shaped waits on it. **Phase 12** settles
the Python-visible surface from a three-engine survey of 76 idiomatic programs: its rule is
that every construct is *implemented and CPython-equal*, *refused by a stable code with a
documented exit class*, or *absent from the surface manifest* — and never "parses, runs,
prints something". Its rows are `L12.1`–`L12.13`, and three of them are Phase 11's by root
cause, named as such in the tracker.

The rest of the canonical documentation lives beside this file:

- `docs/language.md` — language surface (single source of truth).
- `docs/operations.md` — CLI, agent operations, build/test pipeline (single source of truth).
- `docs/adr/` — architecture decision records (one per feature).
- `docs/agentic/` — agent-facing notes (`ast-ir-schema.md`).

Rule: keep `../roadmap.md`, `docs/language.md`, and `docs/operations.md` in sync; never let
them drift apart. A status change in the tracker and its story in the record belong in the
same commit as the code that earned them.
