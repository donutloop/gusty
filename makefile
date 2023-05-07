test:
	go test -v -tags=llvm15 ./...

build:
	go build -v -tags=llvm15 ./...


SOURCEDIR := ./integration/expected
SOURCES := $(wildcard $(SOURCEDIR)/*.ll)
OBJECTS := $(patsubst %.ll,%.o,$(SOURCES))
EXECUTABLES := $(patsubst %.ll,%,$(SOURCES))

.PHONY: all clean

all: $(EXECUTABLES)

$(SOURCEDIR)/%.o: $(SOURCEDIR)/%.ll
	llc -opaque-pointers -filetype=obj $< -o $@

$(SOURCEDIR)/%: $(SOURCEDIR)/%.o
	gcc $< -o $@

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