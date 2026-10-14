
## A slot created for the root stack was read before its binding ran — found only because cases overlapped (Gap R.258, ADR 0343)

- **The symptom was load-order, the cause was an uninitialised word.** `x = sorted([3, 1, 2], reverse=True)`
  followed by `x` died by SIGSEGV in `@rt_alloc`; run on its own it answered `[3, 2, 1]`. `hoistAllocas` (ADR 0181)
  moves every `%_x = alloca i32` to the top of its function so a variable has *one* slot per call — that is what
  lets the root stack identify a variable by address — and "one slot" was mistaken for "a defined slot". The
  release-the-old-binding prologue loads the slot to decide whether to `rt_free` it. Alone, the frame word happened
  to be 0 and `emitFreeOld` skips 0; beside another case's compile, it was someone else's bits, and a garbage
  handle in the free list is how an allocator dies on its *next* call rather than its last.
- **Fix at the place the resource is born, not at the forty places it is requested.** Every road that allocates a
  slot would have needed the same one line, and the next road would forget it again; the pass that moves the slots
  now also defines them. The three properties the fix had to preserve each produced a test that failed while it was
  broken: stores *after* the whole alloca run (rooting needs every allocation before the first instruction),
  idempotence (a road that already cleared its slot is left alone), and the store carrying the slot's own `!dbg`
  (the line table promises a location for every instruction of a program function — an unlabelled store reads as a
  hole in the source correspondence, and two DWARF cases said so before I did).
- **Correct the record when the record is wrong.** The roadmap row and ADR 0343 as filed both said the slot was
  written with the *tag* half of a pair answer. Nothing emits such a store; the number `0xffff` was inferred from
  the faulting store, not observed in a trace. Both now say what was measured, and say that the earlier claim was
  wrong — an ADR is the artifact future agents reconstruct decisions from, and a confident wrong root cause is
  worse than an open question, because it stops them asking.
- **Deleting a ledger row is a different act from editing one.** The drift ledger's own failure text — *PAID debt
  still on the ledger… Remove the row* — is what told me the divergence was gone rather than merely changed; the
  count then rippled into two `measured, not remembered` cells. That is the ratchet doing its job from the other
  side: a row may not disappear silently, and it may not stay once it has been paid.
- **The suite is the instrument.** Every one of these findings (two table rows sharing a source file, a ledger row
  pinned to unstable bytes, the power-reader race, the toolchain as package state, this) came from the same act —
  letting cases run beside each other — and none was visible when only one could run at a time.
