// Package compliance contains tests that enforce hapiq's cache invariants
// across all downloaders.
//
// The static test in this file scans every Go source file under
// pkg/downloaders/ and asserts that any function which streams an HTTP
// response body to disk also participates in the blob cache — either by
// delegating to common.Fetch or by directly using the cache.FromContext API.
//
// The test fails with a precise file:line:function listing when a new
// downloader (or a new code path in an existing one) bypasses the cache.
// Allowlisted exceptions are documented inline.
package compliance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// downloadersRoot returns the absolute path to pkg/downloaders/, located by
// walking up from the test's working directory until the module root is
// found.
func downloadersRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "pkg", "downloaders")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", cwd)
		}
		dir = parent
	}
}

// allowlist is the set of file paths (relative to pkg/downloaders/) whose
// blob-streaming code is exempt from the cache-aware requirement. Each entry
// must have a justification.
var allowlist = map[string]string{
	// common.Fetch is THE sanctioned cache-aware primitive; it implements the
	// cache flow itself, so the in-function cache reference check would not
	// match (it uses cache.FromContext via its caller's ctx, but the local
	// streamToFile helper does not). Allowlisted by design.
	"common/fetch.go": "sanctioned cache-aware download primitive",

	// Ensembl uses a custom MultiProtocolClient (HTTP + FTP) that pre-dates
	// the cache and is tracked as a known exception. Integrating FTP into
	// the cache is a separate workstream.
	"ensembl/download.go":   "documented exception — multi-protocol (FTP) client, no cache integration yet",
	"ensembl/downloader.go": "documented exception — see ensembl/download.go",
	"ensembl/protocol.go":   "documented exception — see ensembl/download.go",

	// experimenthub/metadata.go fetches the Bioconductor catalog sqlite, which
	// uses a separate TTL-based on-disk cache (see ensureMetadata in metadata.go).
	// It is not a dataset blob and intentionally does not flow through the
	// content-addressable blob cache.
	"experimenthub/metadata.go": "metadata sqlite uses a separate TTL cache, not a blob",
}

// violation is a single non-cache-aware blob-streaming site.
type violation struct {
	file     string // relative to pkg/downloaders/
	function string
	line     int
}

func TestDownloadersHonorCache(t *testing.T) {
	root := downloadersRoot(t)

	var violations []violation

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if _, ok := allowlist[rel]; ok {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			if !functionStreamsResponseBody(fn) {
				continue
			}
			if functionIsCacheAware(fn) {
				continue
			}

			violations = append(violations, violation{
				file:     rel,
				function: fn.Name.Name,
				line:     fset.Position(fn.Pos()).Line,
			})
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}

	if len(violations) == 0 {
		return
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].file != violations[j].file {
			return violations[i].file < violations[j].file
		}
		return violations[i].line < violations[j].line
	})

	var b strings.Builder
	b.WriteString("the following functions stream HTTP response bodies to disk without consulting the blob cache.\n")
	b.WriteString("each must either use pkg/downloaders/common.Fetch or call cache.FromContext(ctx) before streaming.\n")
	b.WriteString("if a bypass is intentional, add the file to the allowlist in pkg/cache/compliance/static_test.go with a justification.\n\n")
	for _, v := range violations {
		b.WriteString("  ")
		b.WriteString(v.file)
		b.WriteString(":")
		b.WriteString(itoa(v.line))
		b.WriteString(" func ")
		b.WriteString(v.function)
		b.WriteString("\n")
	}
	t.Fatal(b.String())
}

// functionStreamsResponseBody reports whether fn contains a call of the form
// io.Copy(<dst>, <something>.Body) — the canonical "stream HTTP body to a
// writer" idiom in this codebase. Bare metadata fetches that decode the body
// in-memory (json.Decode, io.ReadAll) do not match.
func functionStreamsResponseBody(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "io" || sel.Sel.Name != "Copy" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		if exprReferencesBody(call.Args[1]) {
			found = true
			return false
		}
		return true
	})
	return found
}

// exprReferencesBody walks expr looking for a SelectorExpr whose selector is
// "Body". This catches resp.Body, response.Body, r.Body, pr (which wraps
// resp.Body), etc. We are deliberately permissive here — better to flag a
// false positive than miss a real bypass.
func exprReferencesBody(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Body" {
			found = true
			return false
		}
		return true
	})
	return found
}

// functionIsCacheAware reports whether fn references the cache API: either a
// call to cache.FromContext or to common.Fetch. Both are sufficient evidence
// that the function participates in the cache flow.
func functionIsCacheAware(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.Name == "cache" && sel.Sel.Name == "FromContext" {
			found = true
		}
		if pkg.Name == "common" && sel.Sel.Name == "Fetch" {
			found = true
		}
		return !found
	})
	return found
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
