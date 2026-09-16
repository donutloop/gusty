# ADR 0079: String `.count(sub[, start[, end]])` optional start/end

## Decision

Extend `str.count` to accept optional `start` and `end` arguments — count
non-overlapping occurrences of `sub` within the sliced substring
`s[start:end]` — mirroring Python's `str.count(sub, start, end)`.

## Details

- **Interpreter**: the `callStrMethod` `count` case takes 1 to 3 arguments
  (`sub`; optionally `start`; optionally `end`). `start` defaults to `0` and
  `end` to `len(s)`. Both are clamped into `[0, len(s)]` (`start < 0` ->
  `0`, `end > len(s)` -> `len(s)`, and `start > end` -> `start = end`), then
  the count is `strings.Count(s[lo:hi], sub)`.
- Example: `"ababab".count("ab", 2)` is 2; `"ababab".count("ab", 2, 4)` is 1.

## Scope

- Interpreter path only: the AOT codegen constant-folding for `count`
  remains one-argument only (it rejects `count()` with more than 1 argument).

## Alternatives Rejected

- **Negative-index semantics** (Python treats negative `start`/`end`
  relative to the string end): rejected for simplicity — this language
  clamps to `[0, len(s)]`. Documented in `docs/language.md`.
