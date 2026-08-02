package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestStrictGateCentralized prevents a future command from bypassing
// openVaultFor by calling the raw vault opener directly.
func TestStrictGateCentralized(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0) //nolint:staticcheck // SA1019: build-tag precision is irrelevant for a same-package source scan; go/packages would drag in a heavyweight dependency
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"main.go": true, "strictgate.go": true}
	directLoadAllowed := map[string]bool{
		"main.go":              true,
		"commands_lockdown.go": true, // human-only lockdown off
		"commands_strict.go":   true, // human-only strict off
		"commands_transfer.go": true, // merge path has an owner preflight
	}
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				ident, ok := call.Fun.(*ast.Ident)
				if ok && ident.Name == "openVault" && !allowed[base] {
					t.Errorf("%s calls raw openVault; use an access-checked wrapper", base)
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Load" {
					if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "vault" && !directLoadAllowed[base] {
						t.Errorf("%s calls vault.Load directly without a reviewed strict-mode path", base)
					}
				}
				return true
			})
		}
	}
}
