# Roadmap — pointers

This is a pointer mirror. The living plan for **Pyre** (the `gusty` repo) is split in two,
and both halves are authoritative for what they own:

- [`../roadmap.md`](../roadmap.md) — **the tracker.** The single source of truth for state:
  the status vocabulary, the measured snapshot, the component map, the prioritized open
  queue, and one row per item and per gap (`ID · item · status · path · ADR · evidence ·
  record`). The agent loop reads the queue and writes one cell per finished item.
- [`roadmap-details.md`](roadmap-details.md) — **the record.** The narrative: how each gap
  was found, what the wrong answer looked like, which measurement settled it, what was
  rejected. Statuses never live here.

The rest of the canonical documentation lives beside this file:

- `docs/language.md` — language surface (single source of truth).
- `docs/operations.md` — CLI, agent operations, build/test pipeline (single source of truth).
- `docs/adr/` — architecture decision records (one per feature).
- `docs/agentic/` — agent-facing notes (`ast-ir-schema.md`).

Rule: keep `../roadmap.md`, `docs/language.md`, and `docs/operations.md` in sync; never let
them drift apart. A status change in the tracker and its story in the record belong in the
same commit as the code that earned them.
