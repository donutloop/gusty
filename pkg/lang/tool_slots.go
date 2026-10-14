package lang

// The concurrency governor for every external process this toolchain starts.
//
// Nothing in the pipeline after codegen runs inside this program: `llc` lowers the module, `cc` links
// it, `opt` verifies it, `python3` answers the reference leg, and — since ADR 0343 — the compiled
// program itself runs as a child process too. That isolation is what lets one test case run beside
// another: a program that segfaults takes its own process down instead of the test binary, and no two
// runs share a file descriptor, a working directory or an environment.
//
// Isolation has a price, and it is the price every parallel harness pays. 20 cores' worth of
// concurrent cases no longer means 20 processes; it means 20 cases each starting an `llc`, a `cc` and a
// program — 60 runnables on a 20-core machine, and memory is the first thing to fall over. Before this
// file the count was unbounded, because no case was expected to have company: `go test` ran a package's
// cases one at a time, and the only concurrency in the suite was `tools/testshards`, which capped it by
// never oversubscribing a shard. Turning on `t.Parallel()` moves the cap from "how the harness is
// launched" to "how the toolchain behaves", which is the only place it can live once anyone can call
// `RunSource` from a goroutine.
//
// So one counting semaphore guards every external-process call site, and it is the toolchain's own
// business rather than the caller's: a shard, a REPL session and a parallel test case all get the same
// guarantee without any of them having to remember to ask. The limit is per process, which is what a
// test binary, a shard and a CLI invocation each are.
//
// One permit is held per external process and never two by the same goroutine — the helpers below wrap
// a leaf call and nothing else — so the semaphore cannot deadlock however callers nest their own work.
// `GUSTY_MAX_CONCURRENT_TOOLS` overrides the limit for a loaded runner the way `GUSTY_TOOL_TIMEOUT`
// overrides its budget (the rule that a harness which cannot be told "this box is slower than you
// assume" has to be recompiled to say so). Set it to 1 and the suite behaves exactly as the serial one
// did, which is how you prove a failure is a concurrency bug rather than a compiler bug.
//
// Roadmap Gap R.253, ADR 0343.

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// maxConcurrentTools is the limit in force, read once at load. Zero or less means "no limit", which is
// what GUSTY_MAX_CONCURRENT_TOOLS=off says.
var maxConcurrentTools = envToolSlots("GUSTY_MAX_CONCURRENT_TOOLS", runtime.NumCPU())

// envToolSlots reads the slot count. A value that does not parse is reported rather than quietly
// ignored: someone who set the variable and still sees 400 `llc` processes has to learn it never took
// effect (the rule envBudget follows for the same reason).
func envToolSlots(name string, def int) int {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return def
	}
	if s == "off" || s == "none" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %s=%q is not a number of concurrent tools; using the default %d\n", name, raw, def)
		return def
	}
	return n
}

var (
	slotOnce  sync.Once
	slots     chan struct{}
	slotMu    sync.Mutex
	slotInUse int // external processes in flight right now
	slotPeak  int // the most ever in flight at once, so "the limit bound us" is provable
	slotCount int // how many external processes this process has started
)

// acquireToolSlot takes permission to start one external process, blocking until the limit allows it.
// The returned function releases the slot and every caller defers it. A no-op release means no limit is
// in force.
func acquireToolSlot() func() {
	if maxConcurrentTools <= 0 {
		return func() {}
	}
	slotOnce.Do(func() { slots = make(chan struct{}, maxConcurrentTools) })
	slots <- struct{}{}
	slotMu.Lock()
	slotCount++
	slotInUse++
	if slotInUse > slotPeak {
		slotPeak = slotInUse
	}
	slotMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			slotMu.Lock()
			slotInUse--
			slotMu.Unlock()
			<-slots
		})
	}
}

// ToolSlotReport is what the governor did this run, for any surface that wants to say so: a run that
// never queued anything got nothing but throughput out of isolation, and a run whose peak equals its
// limit is queueing on the governor rather than on the cores.
type ToolSlotReport struct {
	Limit   int `json:"limit"`
	Peak    int `json:"peak"`
	Started int `json:"started"`
}

// ToolSlots reports the governor's state.
func ToolSlots() ToolSlotReport {
	slotMu.Lock()
	defer slotMu.Unlock()
	return ToolSlotReport{Limit: maxConcurrentTools, Peak: slotPeak, Started: slotCount}
}
