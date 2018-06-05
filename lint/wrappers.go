package lint

import (
	"go/ast"
)

type paramListChecker interface {
	PerFuncInit(*ast.FuncDecl) bool
	CheckParamList([]*ast.Field)
}

type baseParamListChecker struct {
	ctx *context
}

func (c baseParamListChecker) PerFuncInit(*ast.FuncDecl) bool {
	return true
}

// wrapParamListChecker returns a check function that visits every
// top-level function declaration parameters.
//
// CheckParamList first called for receiver list (if it's not nil),
// then for input parameters list,
// then for output (results) parameters list (if it's not nil).
//
// Does not inspect nested functions (closures).
func wrapParamListChecker(c paramListChecker) func(*ast.File) {
	return func(f *ast.File) {
		for _, decl := range f.Decls {
			decl, ok := decl.(*ast.FuncDecl)
			if !ok || !c.PerFuncInit(decl) {
				continue
			}
			if decl.Recv != nil {
				c.CheckParamList(decl.Recv.List)
			}
			c.CheckParamList(decl.Type.Params.List)
			if decl.Type.Results != nil {
				c.CheckParamList(decl.Type.Results.List)
			}
		}
	}
}

type funcDeclChecker interface {
	CheckFuncDecl(*ast.FuncDecl)
}

type baseFuncDeclChecker struct {
	ctx *context
}

// wrapFuncDeclChecker returns a check function that visits every
// top-level function declaration.
//
// CheckLocalExpr is called on every function declaration.
func wrapFuncDeclChecker(c funcDeclChecker) func(*ast.File) {
	return func(f *ast.File) {
		for _, decl := range f.Decls {
			if decl, ok := decl.(*ast.FuncDecl); ok {
				c.CheckFuncDecl(decl)
			}
		}
	}
}

type localExprChecker interface {
	PerFuncInit(*ast.FuncDecl) bool
	CheckLocalExpr(ast.Expr)
}

type baseLocalExprChecker struct {
	ctx *context
}

// wrapLocalExprChecher returns a check function that visits every
// function local expression. That is, it does not visit top-level
// expressions that define constants and global variables.
func wrapLocalExprChecker(c localExprChecker) func(*ast.File) {
	return func(f *ast.File) {
		for _, decl := range f.Decls {
			decl, ok := decl.(*ast.FuncDecl)
			if !ok || !c.PerFuncInit(decl) {
				continue
			}
			ast.Inspect(decl.Body, func(x ast.Node) bool {
				if expr, ok := x.(ast.Expr); ok {
					c.CheckLocalExpr(expr)
				}
				return true
			})
		}
	}
}

func (c baseLocalExprChecker) PerFuncInit(fn *ast.FuncDecl) bool {
	return fn.Body != nil
}

type stmtChecker interface {
	PerFuncInit(*ast.FuncDecl) bool
	CheckStmt(ast.Stmt)
}

type baseStmtChecker struct {
	ctx *context
}

func (c baseStmtChecker) PerFuncInit(fn *ast.FuncDecl) bool {
	return fn.Body != nil
}

// wrapStmtChecker returns a check function that visits every statement
// node inside file, including ones in nested functions.
func wrapStmtChecker(c stmtChecker) func(*ast.File) {
	return func(f *ast.File) {
		for _, decl := range f.Decls {
			// Only functions can contain statements.
			decl, ok := decl.(*ast.FuncDecl)
			if !ok || !c.PerFuncInit(decl) {
				continue
			}
			ast.Inspect(decl.Body, func(x ast.Node) bool {
				if stmt, ok := x.(ast.Stmt); ok {
					c.CheckStmt(stmt)
				}
				return true
			})
		}
	}
}

type typeExprChecker interface {
	CheckTypeExpr(ast.Expr)
}

type baseTypeExprChecker struct {
	ctx *context
}

// wrapTypeExprChecker returns a check function that visits every type
// expression, both top-level and local ones.
//
// It also traverses struct types and interface types to run
// checker over their fields/method signatures.
func wrapTypeExprChecker(c typeExprChecker) func(*ast.File) {
	var checkExpr func(x ast.Expr)

	checkStructType := func(x *ast.StructType) {
		for _, field := range x.Fields.List {
			checkExpr(field.Type)
		}
	}
	checkFieldList := func(xs []*ast.Field) {
		for _, x := range xs {
			checkExpr(x.Type)
		}
	}
	checkFuncType := func(x *ast.FuncType) {
		checkFieldList(x.Params.List)
		if x.Results != nil {
			checkFieldList(x.Results.List)
		}
	}

	checkExpr = func(x ast.Expr) {
		switch x := x.(type) {
		case *ast.StructType:
			checkStructType(x)
		case *ast.InterfaceType:
			checkFieldList(x.Methods.List)
		case *ast.FuncType:
			checkFuncType(x)
		case *ast.ArrayType:
			// TODO: handle slices separately.
			checkExpr(x.Elt)
		default:
			c.CheckTypeExpr(x)
		}
	}

	checkGenDecl := func(x *ast.GenDecl) {
		for _, spec := range x.Specs {
			switch spec := spec.(type) {
			case *ast.ValueSpec:
				if spec.Type != nil {
					checkExpr(spec.Type)
				}
			case *ast.TypeSpec:
				checkExpr(spec.Type)
			default:
				// Do nothing for *ast.ImportSpec.
			}
		}
	}

	return func(f *ast.File) {
		for _, decl := range f.Decls {
			if decl, ok := decl.(*ast.GenDecl); ok {
				checkGenDecl(decl)
				continue
			}

			// Must be a func decl.
			decl := decl.(*ast.FuncDecl)
			if decl.Recv != nil {
				checkExpr(decl.Recv.List[0].Type)
			}
			checkFuncType(decl.Type)
			if decl.Body == nil {
				continue
			}
			for _, stmt := range decl.Body.List {
				// TODO: need to look inside expressions to detect
				// calls like make(T, ...), where T is an expression
				// we want to check.
				switch stmt := stmt.(type) {
				case *ast.DeclStmt:
					// Function-local declaration of var/const/type.
					checkGenDecl(stmt.Decl.(*ast.GenDecl))
				}
			}
		}
	}
}