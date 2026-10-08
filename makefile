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
# `make test` remains the plain serial command, and it is the one to use for the modes that write
# per-run artifacts (GUSTY_GOLDEN_UPDATE rewriting the drift ledger, GUSTY_GOLDEN_MISSING collecting
# sources the record does not cover): N shards would each write their own subset over the file.
#
# `test` runs with -count=1. Half the suite reads files outside its own package (docs/*.md, roadmap.md,
# integration/programs/*.gy) and those are not in `go test`'s cache key, so on a warm cache a doc edit can be
# certified by a run that never opened the file it changed — which is the opposite of what a guard is for.
testshards:
	go run ./tools/testshards -tags llvm20 ./pkg/... ./cmd/... ./tools/... ./integration/...

# The split, without running anything: which case lands in which shard.
showshards:
	go run ./tools/testshards -tags llvm20 -list ./pkg/... ./cmd/... ./tools/... ./integration/...

test:
	go test -tags=llvm20 -count=1 ./...

build:
	go build -tags=llvm20 ./...


LLC ?= llc-20

SOURCEDIR := ./integration/expected
SOURCES := $(wildcard $(SOURCEDIR)/*.ll)
OBJECTS := $(patsubst %.ll,%.o,$(SOURCES))
EXECUTABLES := $(patsubst %.ll,%,$(SOURCES))

.PHONY: all clean

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
