// Package iptablescheck provides a linter to detect iptables comment flags that are incompatible with gVisor.
//
// This linter was added to address compatibility issues in gVisor environments where iptables
// comment module (-m comment, --comment flags) is not supported. The linter helps prevent
// runtime failures by catching these incompatible flags at build time.
//
// See: https://github.com/istio/istio/issues/57678
package iptablescheck

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "iptablescheck",
	Doc:  "checks for iptables comment flags that are incompatible with gVisor",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				checkIptablesCall(pass, call)
			}
			return true
		})
	}
	return nil, nil
}

func checkIptablesCall(pass *analysis.Pass, call *ast.CallExpr) {
	if !isIptablesFunction(call) {
		return
	}

	for _, arg := range call.Args {
		if hasCommentFlags(pass, arg) {
			pass.Reportf(call.Pos(), "iptables comment flags (-m comment, --comment) are not supported in gVisor environments")
			return
		}
	}
}

func isIptablesFunction(call *ast.CallExpr) bool {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name == "executeXTables" || strings.Contains(sel.Sel.Name, "iptables")
	}
	if ident, ok := call.Fun.(*ast.Ident); ok {
		return strings.Contains(ident.Name, "iptables") || strings.Contains(ident.Name, "executeXTables")
	}
	return false
}

func hasCommentFlags(pass *analysis.Pass, arg ast.Expr) bool {
	switch arg := arg.(type) {
	case *ast.BasicLit:
		if arg.Kind == token.STRING {
			value := strings.Trim(arg.Value, `"`)
			return containsCommentFlag(value)
		}
	case *ast.CompositeLit:
		for _, elt := range arg.Elts {
			if hasCommentFlags(pass, elt) {
				return true
			}
		}
	case *ast.Ident:
		if arg.Obj != nil && arg.Obj.Kind == ast.Var {
			if assign, ok := arg.Obj.Decl.(*ast.AssignStmt); ok {
				for _, rhs := range assign.Rhs {
					if hasCommentFlags(pass, rhs) {
						return true
					}
				}
			}
		}
	}
	return false
}

func containsCommentFlag(s string) bool {
	fields := strings.Fields(s)
	for i, field := range fields {
		if field == "-m" && i+1 < len(fields) && fields[i+1] == "comment" {
			return true
		}
		if field == "--comment" {
			return true
		}
	}
	return false
}