# gusty build & test targets.
#
# LLVM toolchain: LLVM 20. Opaque pointers are the default in LLVM 20, so
# llc-20 needs no -opaque-pointers flag.

# The suite is subprocess-bound: almost every case in pkg/lang ends inside llc-20, cc, lli-20 or
# python3, so running its cases one at a time leaves three of the runner's four cores idle (ADR 0313).
# `testshards` runs the same cases in one process per core. It is not a different test suite — the
# partition comes from `go test -list`, every case runs exactly once, and each case asserts what it
# always asserted.
#
# Since ADR 0343 the cases themselves run beside each other: `make testparallel` is the whole suite with
# `t.Parallel()` honoured, about 1m40s on 20 cores against 11m43s serial. A compiled program runs as a child
# process, so two cases cannot print into each other's pipes or share a heap; `make parallelism` is the guard
# (tools/parify -check) that fails a case claiming parallelism while its body reaches process state, and the
# cases that legitimately own the process carry a `//parify:serial <reason>` marker.
#
# `make test` remains the plain serial command, and it is the one to use for the modes that write
# per-run artifacts (GUSTY_GOLDEN_UPDATE rewriting the drift ledger, GUSTY_GOLDEN_MISSING collecting
# sources the record does not cover): N shards would each write their own subset over the file.
#
# `test` runs with -count=1. Half the suite reads files outside its own package (docs/*.md, roadmap.md,
# integration/programs/*.gy) and those are not in `go test`'s cache key, so on a warm cache a doc edit can be
# certified by a run that never opened the file it changed — which is the opposite of what a guard is for.
# Every target here keeps the flag for the same reason.
testshards:
	go run ./tools/testshards -tags llvm20 ./pkg/... ./cmd/... ./tools/... ./integration/...

# The split, without running anything: which case lands in which shard.
showshards:
	go run ./tools/testshards -tags llvm20 -list ./pkg/... ./cmd/... ./tools/... ./integration/...

test:
	go test -tags=llvm20 -count=1 ./...

# The suite with its cases overlapping (ADR 0343). PARALLEL=N overrides the fan-out.
PARALLEL ?= $(shell nproc 2>/dev/null || echo 8)
testparallel:
	go test -tags=llvm20 -count=1 -parallel $(PARALLEL) -timeout 90m ./...

# The same suite with the race detector on. It is the check the analysis cannot replace: a shared cache that
# parify's walk did not see shows up here rather than being decided by a lucky schedule. The tree is clean on
# ./pkg/... ./cmd/... ./integration/... ./tools/... (ADR 0343).
testrace:
	go test -tags=llvm20 -race -count=1 -parallel $(PARALLEL) -timeout 90m ./...

# The parallelism guard, on its own: the same command the suite runs internally.
parallelism:
	go run ./tools/parify -check ./pkg/... ./cmd/... ./integration/... ./tools/...

build:
	go build -tags=llvm20 ./...


LLC ?= llc-20

SOURCEDIR := ./integration/expected
SOURCES := $(wildcard $(SOURCEDIR)/*.ll)
OBJECTS := $(patsubst %.ll,%.o,$(SOURCES))
EXECUTABLES := $(patsubst %.ll,%,$(SOURCES))

.PHONY: all clean test testparallel testrace testshards showshards parallelism build

all: $(EXECUTABLES)

$(SOURCEDIR)/%.o: $(SOURCEDIR)/%.ll
	$(LLC) -filetype=obj -relocation-model=pic $< -o $@

$(SOURCEDIR)/%: $(SOURCEDIR)/%.o
	cc $< -o $@

buildllvmcode: $(EXECUTABLES)
	@for executable in $(EXECUTABLES); do \
		echo "Running $$executable..."; \
		./$$executable; \
		exit_status=$$?; \
		if [ $$exit_status -ne 0 ]; then \
			echo "Error: $$executable exited with status $$exit_status"; \
		fi; \
	done

clean:
	rm -f $(OBJECTS) $(EXECUTABLES)

testllvmcode: buildllvmcode clean
