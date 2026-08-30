package httpapi

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brilliantkid87/rop/internal/testutil"
)

// TestCoreImportsNoHTTP enforces invariant I-17 (transport bindings do not
// define Core semantics): ROP Core packages must not import any HTTP
// package. Only internal/httpapi (this package), the demo provider, and the
// commands may touch the network surface.
func TestCoreImportsNoHTTP(t *testing.T) {
	corePackages := []string{
		"internal/action",
		"internal/authz",
		"internal/clock",
		"internal/operation",
		"internal/planner",
		"internal/reversal",
		"internal/roperr",
		"internal/store",
		"internal/testutil",
		"internal/verification",
		"pkg/rop",
	}
	banned := map[string]bool{
		"net/http":          true,
		"net/http/httptest": true,
		"net/http/cgi":      true,
		"net/http/httputil": true,
		"net/url":           true,
	}
	root := testutil.RepoRoot()
	fset := token.NewFileSet()
	for _, pkg := range corePackages {
		dir := filepath.Join(root, filepath.FromSlash(pkg))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", pkg, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s/%s: %v", pkg, e.Name(), err)
			}
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if banned[path] {
					t.Errorf("Core package %s (%s) imports %s — transport leak (invariant I-17)", pkg, e.Name(), path)
				}
			}
		}
	}
}
