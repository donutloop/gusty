package lang

import (
	"os"
	"path/filepath"
	"sync"
)

// The standard-library root is a *search path*, and a search path is a fact about the program being
// compiled, not about the process that happens to be compiling it. It used to be one exported package
// variable that the CLI, `gustyc --stdlib`, and any test that wanted a stdlib of its own all wrote — which
// made "give this case a temp stdlib" an act of sabotage against every case running beside it. The
// variable is now the *default*, guarded, and a caller that wants its own root names it per run with
// IRGenOptions.StdlibDir / CompileOptions.StdlibDir (ADR 0343).
var (
	stdlibMu      sync.RWMutex
	stdlibRootVar = defaultStdlibDir()
)

// stdlibRoot is the process default: GUSTY_STDLIB_DIR, or the nearest ancestor `stdlib` with modules.
func stdlibRoot() string {
	stdlibMu.RLock()
	defer stdlibMu.RUnlock()
	return stdlibRootVar
}

// SetStdlibDir sets the process-wide default standard-library root (the CLI's `--stdlib`). An empty value
// restores discovery. A test that wants a stdlib of its own should not call this: it names its root on the
// options bundle of its own run, where a second concurrent case cannot trip over it.
func SetStdlibDir(dir string) {
	next := dir
	if next == "" {
		next = defaultStdlibDir()
	}
	stdlibMu.Lock()
	defer stdlibMu.Unlock()
	stdlibRootVar = next
}

// moduleSearch is where one run looks for an `import mod`: the program's own directory, and the
// standard-library root. Both are per-run values; the empty stdlib means "the process default", which is
// what a shell user means by `import config`.
type moduleSearch struct {
	dir    string
	stdlib string
}

// moduleSearchFor is the search path of an options bundle, including the bundle that was never built.
func moduleSearchFor(opts *IRGenOptions) moduleSearch {
	p := moduleSearch{dir: importDir(opts)}
	if opts != nil && opts.StdlibDir != "" {
		p.stdlib = opts.StdlibDir
	} else {
		p.stdlib = stdlibRoot()
	}
	return p
}

// stdlibSearchPath is the root a run asked for, falling back to the process default.
func stdlibSearchPath(run CompileRun) string {
	if run.StdlibDir != "" {
		return run.StdlibDir
	}
	return stdlibRoot()
}

// moduleSearchForRun is the search path of a compile that was asked for with a CompileRun rather than an
// options bundle — the same two facts, spelled for the caller who has the source text in hand.
func moduleSearchForRun(run CompileRun) moduleSearch {
	return moduleSearch{dir: run.Dir, stdlib: stdlibSearchPath(run)}
}

// defaultStdlibDir returns the GUSTY_STDLIB_DIR value when set, else the
// nearest ancestor `stdlib` directory that contains .gy modules.
func defaultStdlibDir() string {
	if d := os.Getenv("GUSTY_STDLIB_DIR"); d != "" {
		return d
	}
	if d := findStdlibDir("stdlib"); d != "" {
		return d
	}
	return ""
}

// findStdlibDir walks up from the working directory looking for an ancestor
// directory named `name` that contains .gy module files.
func findStdlibDir(name string) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		root := filepath.Join(dir, name)
		if info, err := os.Stat(root); err == nil && info.IsDir() && hasModules(root) {
			return root
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return ""
}

// hasModules reports whether dir contains at least one .gy module file.
func hasModules(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".gy" {
			return true
		}
	}
	return false
}

// ResolveImportPath resolves an `import mod` to an on-disk .gy file. Search
// order: the working directory (`mod.gy`), then the standard-library root
// (`<StdlibDir>/mod.gy`). It returns "" when the module is not found, in
// which case callers report the usual "cannot import" error.
//
// It asks the working directory because that is what a person typing `import config`
// means. A harness that must resolve against some *other* directory — a test case whose
// modules live in its own temp dir, which used to os.Chdir the whole process to say so —
// calls ResolveImportPathIn instead (ADR 0343: nothing about one case's run is process state).
func ResolveImportPath(mod string) string {
	return ResolveImportPathIn("", mod)
}

// ResolveImportPathIn is ResolveImportPath with the directory that the module's own name
// is looked up in spelled out. An empty dir means the working directory, exactly as
// ResolveImportPath means it.
func ResolveImportPathIn(dir, mod string) string {
	return resolveImportPathWith(moduleSearch{dir: dir, stdlib: stdlibRoot()}, mod)
}

// ResolveImportPathWith is ResolveImportPathIn with both halves of the search path named — the program's
// directory and the standard-library root it resolves against. This is the entry a per-run caller uses;
// the two-argument form above is the same lookup with the process default root.
func ResolveImportPathWith(paths moduleSearch, mod string) string {
	return resolveImportPathWith(paths, mod)
}

func resolveImportPathWith(paths moduleSearch, mod string) string {
	local := mod + ".gy"
	if paths.dir != "" {
		local = filepath.Join(paths.dir, local)
	}
	if _, err := os.Stat(local); err == nil {
		return local
	}
	if paths.stdlib != "" {
		p := filepath.Join(paths.stdlib, mod+".gy")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
