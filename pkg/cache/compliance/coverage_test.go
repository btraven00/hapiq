package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// cachePatternByDownloader documents which cache pattern each registered
// downloader uses. The test below cross-references this map against the set
// of constructor calls in cmd/download.go: if a new downloader is wired up
// without an entry here, the test fails so a reviewer must consciously
// decide which pattern it follows (and ideally add a behavioral test for it
// if the pattern is novel).
//
// Patterns:
//
//   - common.Fetch — delegates to pkg/downloaders/common.Fetch. Covered
//     transitively by TestCommonFetchCacheContract.
//   - inline       — re-implements the cache flow using cache.FromContext.
//     Each instance must have its own behavioral test (see geo, sra,
//     experimenthub).
//   - exception    — deliberately bypasses the cache. Must be allowlisted
//     in static_test.go with a justification.
//
// Map key: the package-level constructor name appearing in cmd/download.go
// (e.g. "NewGEODownloader"). The constructor name is the most stable handle
// available before the registry is populated at runtime.
var cachePatternByDownloader = map[string]string{
	"NewGEODownloader":           "inline",     // pkg/downloaders/geo
	"NewFigshareDownloader":      "common.Fetch",
	"NewZenodoDownloader":        "common.Fetch",
	"NewEnsemblDownloader":       "exception",  // FTP/multi-protocol, see static_test allowlist
	"NewSRADownloader":           "inline",     // pkg/downloaders/sra
	"NewVCPDownloader":           "common.Fetch",
	"NewHCADownloader":           "common.Fetch",
	"NewBioStudiesDownloader":    "common.Fetch",
	"NewScPerturbDownloader":     "common.Fetch",
	"NewExperimentHubDownloader": "inline",     // pkg/downloaders/experimenthub
	// scanpy and url use unconventional constructor names ("New").
	// Distinguished by package import alias used in cmd/download.go.
	"scanpy.New":         "common.Fetch",
	"urldownloader.New":  "common.Fetch",
}

// TestRegistryCoverage scans cmd/download.go for every constructor call that
// produces a value passed to downloaders.Register, and asserts each is
// documented in cachePatternByDownloader. The goal is to ensure that adding
// a new downloader requires explicit acknowledgement of its cache pattern.
func TestRegistryCoverage(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "cmd", "download.go")

	src, err := os.ReadFile(path) // #nosec G304 -- known repo file
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	registered := collectRegisteredConstructors(file)
	if len(registered) == 0 {
		t.Fatal("found no downloader registrations in cmd/download.go — has the file structure changed?")
	}

	var missing []string
	for _, name := range registered {
		if _, ok := cachePatternByDownloader[name]; !ok {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("the following downloader constructors are wired up in cmd/download.go but not documented in cachePatternByDownloader:\n  %s\n\nadd each to the map (pkg/cache/compliance/coverage_test.go) tagged with its cache pattern (common.Fetch, inline, or exception). if it uses a new pattern, add a behavioral test for it.", strings.Join(missing, "\n  "))
	}
}

// collectRegisteredConstructors returns the names of constructor calls whose
// return value is passed (possibly via a local variable) to
// downloaders.Register. Constructor names are normalised to either the bare
// function name (e.g. "NewGEODownloader") or "<pkg>.<fn>" when the bare name
// is too generic ("New").
func collectRegisteredConstructors(file *ast.File) []string {
	// Pass 1: walk variable declarations and assignments, mapping local
	// variable names to constructor calls of the form `pkg.NewXxx(...)`.
	varToCtor := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		ident, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		ctor, ok := constructorName(assign.Rhs[0])
		if !ok {
			return true
		}
		varToCtor[ident.Name] = ctor
		return true
	})

	// Pass 2: walk calls of the form `downloaders.Register(<ident>)` and
	// look up the constructor for <ident>.
	seen := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "downloaders" || sel.Sel.Name != "Register" {
			return true
		}
		if len(call.Args) != 1 {
			return true
		}
		argIdent, ok := call.Args[0].(*ast.Ident)
		if !ok {
			return true
		}
		if ctor, ok := varToCtor[argIdent.Name]; ok {
			seen[ctor] = true
		}
		return true
	})

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// constructorName extracts a canonical name for a constructor call expression.
// `geo.NewGEODownloader(...)` → "NewGEODownloader" (unique enough).
// `scanpy.New(...)` → "scanpy.New" (qualified because "New" alone is too generic).
func constructorName(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	fn := sel.Sel.Name
	if !strings.HasPrefix(fn, "New") {
		return "", false
	}
	if fn == "New" {
		return pkg.Name + "." + fn, true
	}
	return fn, true
}

// repoRoot walks up from the test's working directory to find go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", cwd)
		}
		dir = parent
	}
}
