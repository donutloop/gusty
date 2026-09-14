# gusty build & test targets.
#
# LLVM toolchain: LLVM 20. Opaque pointers are the default in LLVM 20, so
# llc-20 needs no -opaque-pointers flag.

test:
	go test -tags=llvm20 ./...

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
	$(LLC) -filetype=obj $< -o $@

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
