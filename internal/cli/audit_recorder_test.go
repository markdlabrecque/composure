package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestServePassesOpenedStoreAsAuditRecorder(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "cli.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var serverCall *ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		packageName, packageOK := selector.X.(*ast.Ident)
		if packageOK && packageName.Name == "web" && selector.Sel.Name == "Server" {
			serverCall = call
		}
		return true
	})
	if serverCall == nil {
		t.Fatal("serve does not construct the web server")
	}
	if len(serverCall.Args) != 3 {
		t.Fatalf("web.Server receives %d arguments, want repository, audit recorder, and listener", len(serverCall.Args))
	}
	repository, repositoryOK := serverCall.Args[0].(*ast.Ident)
	recorder, recorderOK := serverCall.Args[1].(*ast.Ident)
	if !repositoryOK || !recorderOK || repository.Name != "repository" || recorder.Name != repository.Name {
		t.Fatal("serve must pass the opened Store as the web server's audit recorder")
	}
}
