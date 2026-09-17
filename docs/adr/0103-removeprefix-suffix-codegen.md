# ADR 0103: codegen `removeprefix`/`removesuffix` folding

## Decision

Add the string methods `"s".removeprefix(p)` and `"s".removesuffix(s)` to
the AOT codegen **string-method dispatch** (method-call syntax, like
ljust/rjust/zfill in ADR 0101/0102): the receiver is resolved as a constant
string `v` and `c.Args[0]` is the constant prefix/suffix (via `g.stringVal`).

- `removeprefix` strips the given prefix (`strings.TrimPrefix`).
- `removesuffix` strips the given suffix (`strings.TrimSuffix`).
- No-op when unmatched.

The result is a string global via `strConst`, mirroring the interpreter.

## Motivation

Both were interpreter-only string methods; the codegen method dispatcher fell
through to "unsupported string method". They are pure prefix/suffix-stripping
folds over constant receiver + constant substring.

## Tests

- `TestIRRemoveprefixSuffixFolds` (ircheck): llc-verifies
  `"abcabc".removeprefix("abc")` / `"abcabc".removesuffix("abc")` fold to an
  `abc` string global; no-match cases verify cleanly.
- `TestStdlibRemoveprefixSuffix` (stdlib): interpreter behavior checks.

## Alternatives rejected

- Adding to the builtin Call switch: rejected — the parser represents string
  methods as method-call nodes (same rationale as ADR 0101/0102).
