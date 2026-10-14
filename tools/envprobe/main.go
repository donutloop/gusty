// Command envprobe reports the toolchain and oracle knobs THIS process resolved from its environment.
//
// The knobs are process-wide by design — `GUSTY_TOOL_TIMEOUT`, `GUSTY_ORACLE_TIMEOUT`, `GUSTY_PYTHON` are
// meant to be set from a shell — so the only honest way to ask "did the setting take effect?" about the real
// entry points is to ask a process that was started with it. Doing it from inside a test would mean
// `t.Setenv`, which Go forbids once the case is parallel, and which would have taken the environment away
// from every other case running beside it (ADR 0343).
//
// So the suite runs THIS, with the environment it wants:
//
//	envprobe [key...]
//
// with no key it prints every knob as JSON; with keys it prints `key=value` lines for those knobs. Known
// keys: `python`, `tool-budget`, `oracle-budget`, `tool-slots`. An unknown key is reported on stderr and
// exits 2, so a test that typos a knob name fails loudly instead of asserting nothing.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/donutloop/gusty/pkg/lang"
	"os"
)

func main() {
	knobs := map[string]string{
		"python":        lang.PythonBinary(),
		"tool-budget":   lang.ToolBudget.String(),
		"oracle-budget": lang.OracleBudget.String(),
		"tool-slots":    fmt.Sprintf("limit=%d peak=%d started=%d", lang.ToolSlots().Limit, lang.ToolSlots().Peak, lang.ToolSlots().Started),
		"llvm-pinned":   lang.PinnedLLVMVersion,
		"oracle-min":    lang.OracleMinPython,
	}
	keys := os.Args[1:]
	if len(keys) == 0 {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(knobs); err != nil {
			fmt.Fprintln(os.Stderr, "envprobe:", err)
			os.Exit(1)
		}
		return
	}
	for _, k := range keys {
		v, ok := knobs[k]
		if !ok {
			fmt.Fprintf(os.Stderr, "envprobe: no knob named %q (known: python, tool-budget, oracle-budget, tool-slots, llvm-pinned, oracle-min)\n", k)
			os.Exit(2)
		}
		fmt.Printf("%s=%s\n", k, v)
	}
}
