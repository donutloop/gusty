package lang

import (
	"os"
	"path/filepath"
)

// StdlibDir is the root directory of the standard library. It is seeded from
// the GUSTY_STDLIB_DIR environment variable, otherwise discovered by walking
// up from the current working directory looking for a `stdlib` directory that
// contains .gy modules. Tests and the CLI may override it (see SetStdlibDir).
var StdlibDir = defaultStdlibDir()

// SetStdlibDir overrides the standard-library root (used by tests and the
// gustyc --stdlib flag). An empty value restores discovery.
func SetStdlibDir(dir string) {
	StdlibDir = dir
	if StdlibDir == "" {
		StdlibDir = defaultStdlibDir()
	}
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
func ResolveImportPath(mod string) string {
	if _, err := os.Stat(mod + ".gy"); err == nil {
		return mod + ".gy"
	}
	if StdlibDir != "" {
		p := filepath.Join(StdlibDir, mod+".gy")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
