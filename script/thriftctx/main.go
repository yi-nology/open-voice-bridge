// Command thriftctx rewrites hz/thriftgo-generated model code from the
// pre-context TProtocol shape to the context-first API that
// github.com/apache/thrift >= 0.16 requires. It is the post-step of
// `make idl`: hz still emits the old shape, so without this the generated
// file cannot compile against a maintained thrift release (OPS-14).
//
// The rewrite is mechanical and safe precisely because the input is
// machine-generated:
//
//  1. every func with a thrift.TProtocol parameter gains a leading
//     ctx context.Context parameter;
//  2. every call that passes an iprot/oprot identifier, or calls a method
//     named Read/Write, or calls one of the functions from (1), gains a
//     leading ctx argument.
//
// Usage: go run ./script/thriftctx <generated.go> [...]
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: thriftctx <generated.go> [...]")
		os.Exit(2)
	}
	for _, path := range os.Args[1:] {
		if err := rewrite(path); err != nil {
			fmt.Fprintf(os.Stderr, "thriftctx: %s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

func rewrite(path string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	changed := false
	ctxFuncs := map[string]bool{}

	// Pass 1: which funcs take a thrift.TProtocol?
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			continue
		}
		for _, field := range fn.Type.Params.List {
			if isProtocolType(field.Type) {
				ctxFuncs[fn.Name.Name] = true
				break
			}
		}
	}

	// Pass 2: add the ctx parameter.
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !ctxFuncs[fn.Name.Name] {
			continue
		}
		if hasCtxParam(fn) {
			continue
		}
		ctxField := &ast.Field{
			Names: []*ast.Ident{ast.NewIdent("ctx")},
			Type:  ctxTypeExpr(),
		}
		fn.Type.Params.List = append([]*ast.Field{ctxField}, fn.Type.Params.List...)
		changed = true
	}

	// Pass 3: thread ctx through every call.
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if hasCtxArg(call) {
			return true
		}
		if needsCtx(call, ctxFuncs) {
			call.Args = append([]ast.Expr{ast.NewIdent("ctx")}, call.Args...)
			changed = true
		}
		return true
	})

	// Pass 4: thrift >= 0.25 TClient.Call returns (ResponseMeta, error);
	// the generated code assigns one value. Discard the meta.
	ast.Inspect(file, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Call" || !hasCtxArg(call) {
			return true
		}
		as.Lhs = []ast.Expr{ast.NewIdent("_"), as.Lhs[0]}
		changed = true
		return true
	})

	// Pass 5: thrift >= 0.25 TException carries TExceptionType(); the
	// generated processors only shuttle plain errors, so widen their result
	// type to error (the processors are unused by the HTTP edge).
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Type.Results == nil {
			return true
		}
		for _, field := range fn.Type.Results.List {
			if isTExceptionType(field.Type) {
				field.Type = ast.NewIdent("error")
				changed = true
			}
		}
		return true
	})

	// Pass 6: thrift.TProcessorFunction still returns TException, so the
	// widened processors are adapted at their single registration site.
	wrapped := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "AddToProcessorMap" || len(call.Args) != 2 {
			return true
		}
		call.Args[1] = &ast.CompositeLit{
			Type: ast.NewIdent("thriftCtxProcessorAdapter"),
			Elts: []ast.Expr{&ast.KeyValueExpr{
				Key:   ast.NewIdent("inner"),
				Value: call.Args[1],
			}},
		}
		wrapped = true
		changed = true
		return true
	})

	if !changed {
		return nil
	}
	if !importsContext(file) {
		file.Imports = append(file.Imports, &ast.ImportSpec{
			Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote("context")},
		})
		// Insert the import into the first import declaration block.
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if ok && gen.Tok == token.IMPORT {
				gen.Specs = append(gen.Specs, file.Imports[len(file.Imports)-1])
				break
			}
		}
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return err
	}
	if wrapped {
		buf.WriteString(processorGlue)
	}
	out, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// processorGlue adapts the widened processors to thrift.TProcessorFunction,
// whose Process still returns thrift.TException.
const processorGlue = `

// thriftCtxProcessorAdapter: generated by script/thriftctx — do not edit.
type thriftCtxProcessorAdapter struct {
	inner interface {
		Process(ctx context.Context, seqId int32, iprot, oprot thrift.TProtocol) (bool, error)
	}
}

func (a thriftCtxProcessorAdapter) Process(ctx context.Context, seqId int32, iprot, oprot thrift.TProtocol) (bool, thrift.TException) {
	ok, err := a.inner.Process(ctx, seqId, iprot, oprot)
	if err != nil {
		return ok, thrift.NewTApplicationException(thrift.INTERNAL_ERROR, err.Error())
	}
	return ok, nil
}
`

func isProtocolType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "thrift" && sel.Sel.Name == "TProtocol"
}

func isTExceptionType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "thrift" && sel.Sel.Name == "TException"
}

func ctxTypeExpr() ast.Expr {
	return &ast.SelectorExpr{X: ast.NewIdent("context"), Sel: ast.NewIdent("Context")}
}

func hasCtxParam(fn *ast.FuncDecl) bool {
	for _, field := range fn.Type.Params.List {
		if sel, ok := field.Type.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "context" && sel.Sel.Name == "Context" {
				return true
			}
		}
	}
	return false
}

func hasCtxArg(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	id, ok := call.Args[0].(*ast.Ident)
	return ok && id.Name == "ctx"
}

func needsCtx(call *ast.CallExpr, ctxFuncs map[string]bool) bool {
	switch callee := call.Fun.(type) {
	case *ast.SelectorExpr:
		if callee.Sel.Name == "Read" || callee.Sel.Name == "Write" {
			return true
		}
		if id, ok := callee.X.(*ast.Ident); ok && (id.Name == "iprot" || id.Name == "oprot") {
			return true
		}
		if ctxFuncs[callee.Sel.Name] {
			return true
		}
	case *ast.Ident:
		if ctxFuncs[callee.Name] {
			return true
		}
	}
	return false
}

func importsContext(file *ast.File) bool {
	for _, spec := range file.Imports {
		if spec.Path != nil && spec.Path.Value == `"context"` {
			return true
		}
	}
	return false
}
