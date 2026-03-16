package index

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// GoParser extracts symbols from Go source files using the standard library's
// go/parser and go/ast packages. This provides production-quality parsing
// for Go files without requiring CGO or tree-sitter.
//
// For other languages (JS/TS, Python, Rust), tree-sitter backends can be
// registered via the Parser interface.
type GoParser struct{}

// NewGoParser creates a new Go language parser.
func NewGoParser() *GoParser {
	return &GoParser{}
}

func (p *GoParser) SupportedExtensions() []string {
	return []string{".go"}
}

// Parse extracts all symbols and imports from a Go source file.
// Extracts: functions, types/structs (with fields), interfaces,
// constants, variables, and imports.
func (p *GoParser) Parse(filePath string) ([]Symbol, []Import, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", filePath, err)
	}

	var symbols []Symbol
	var imports []Import
	pkgPath := node.Name.Name

	// Extract imports.
	for _, imp := range node.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		imports = append(imports, Import{
			ImportPath: impPath,
			Alias:      alias,
		})
	}

	// Walk declarations.
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := p.extractFunction(fset, d, pkgPath)
			symbols = append(symbols, sym)

		case *ast.GenDecl:
			syms := p.extractGenDecl(fset, d, pkgPath)
			symbols = append(symbols, syms...)
		}
	}

	return symbols, imports, nil
}

// extractFunction extracts a function or method symbol.
func (p *GoParser) extractFunction(fset *token.FileSet, fn *ast.FuncDecl, pkgPath string) Symbol {
	sym := Symbol{
		Name:        fn.Name.Name,
		Kind:        KindFunction,
		Line:        fset.Position(fn.Pos()).Line,
		EndLine:     fset.Position(fn.End()).Line,
		Exported:    isExported(fn.Name.Name),
		PackagePath: pkgPath,
	}

	// Build signature.
	var sig strings.Builder
	sig.WriteString("func ")

	// Method receiver.
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recv := fn.Recv.List[0]
		recvType := typeString(recv.Type)
		sig.WriteString("(" + recvType + ") ")
		sym.ParentName = strings.TrimPrefix(strings.TrimPrefix(recvType, "*"), " ")
	}

	sig.WriteString(fn.Name.Name)
	sig.WriteString("(")

	// Parameters.
	if fn.Type.Params != nil {
		var params []string
		for _, field := range fn.Type.Params.List {
			typeName := typeString(field.Type)
			if len(field.Names) == 0 {
				params = append(params, typeName)
			} else {
				for _, name := range field.Names {
					params = append(params, name.Name+" "+typeName)
				}
			}
		}
		sig.WriteString(strings.Join(params, ", "))
	}
	sig.WriteString(")")

	// Return type.
	if fn.Type.Results != nil {
		var returns []string
		for _, field := range fn.Type.Results.List {
			returns = append(returns, typeString(field.Type))
		}
		if len(returns) == 1 {
			sig.WriteString(" " + returns[0])
			sym.ReturnType = returns[0]
		} else if len(returns) > 1 {
			ret := "(" + strings.Join(returns, ", ") + ")"
			sig.WriteString(" " + ret)
			sym.ReturnType = ret
		}
	}

	sym.Signature = sig.String()
	return sym
}

// extractGenDecl extracts type, const, and var declarations.
func (p *GoParser) extractGenDecl(fset *token.FileSet, decl *ast.GenDecl, pkgPath string) []Symbol {
	var symbols []Symbol

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			sym := p.extractTypeSpec(fset, s, pkgPath)
			symbols = append(symbols, sym)

		case *ast.ValueSpec:
			syms := p.extractValueSpec(fset, s, decl.Tok, pkgPath)
			symbols = append(symbols, syms...)
		}
	}
	return symbols
}

// extractTypeSpec extracts a type declaration (struct, interface, or alias).
func (p *GoParser) extractTypeSpec(fset *token.FileSet, spec *ast.TypeSpec, pkgPath string) Symbol {
	sym := Symbol{
		Name:        spec.Name.Name,
		Line:        fset.Position(spec.Pos()).Line,
		Exported:    isExported(spec.Name.Name),
		PackagePath: pkgPath,
	}

	if spec.Type != nil {
		sym.EndLine = fset.Position(spec.Type.End()).Line
	}

	switch t := spec.Type.(type) {
	case *ast.StructType:
		sym.Kind = KindType
		sym.Signature = "type " + spec.Name.Name + " struct"
		if t.Fields != nil {
			for _, field := range t.Fields.List {
				typeName := typeString(field.Type)
				if len(field.Names) == 0 {
					// Embedded field.
					sym.Fields = append(sym.Fields, Field{
						Name:     typeName,
						TypeName: typeName,
						Exported: isExported(typeName),
					})
				} else {
					for _, name := range field.Names {
						sym.Fields = append(sym.Fields, Field{
							Name:     name.Name,
							TypeName: typeName,
							Exported: isExported(name.Name),
						})
					}
				}
			}
		}

	case *ast.InterfaceType:
		sym.Kind = KindInterface
		sym.Signature = "type " + spec.Name.Name + " interface"
		if t.Methods != nil {
			for _, method := range t.Methods.List {
				if len(method.Names) > 0 {
					sym.Fields = append(sym.Fields, Field{
						Name:     method.Names[0].Name,
						TypeName: typeString(method.Type),
						Exported: isExported(method.Names[0].Name),
					})
				}
			}
		}

	default:
		sym.Kind = KindType
		sym.Signature = "type " + spec.Name.Name + " " + typeString(spec.Type)
	}

	return sym
}

// extractValueSpec extracts const or var declarations.
func (p *GoParser) extractValueSpec(fset *token.FileSet, spec *ast.ValueSpec, tok token.Token, pkgPath string) []Symbol {
	var symbols []Symbol
	kind := KindVariable
	if tok == token.CONST {
		kind = KindConstant
	}

	for _, name := range spec.Names {
		typeName := ""
		if spec.Type != nil {
			typeName = typeString(spec.Type)
		}
		sym := Symbol{
			Name:        name.Name,
			Kind:        kind,
			Line:        fset.Position(name.Pos()).Line,
			Exported:    isExported(name.Name),
			PackagePath: pkgPath,
			ReturnType:  typeName,
		}
		if kind == KindConstant {
			sym.Signature = "const " + name.Name
		} else {
			sym.Signature = "var " + name.Name
		}
		if typeName != "" {
			sym.Signature += " " + typeName
		}
		symbols = append(symbols, sym)
	}
	return symbols
}

// typeString converts an AST type expression to a string representation.
func typeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.ArrayType:
		return "[]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		return "chan " + typeString(t.Value)
	case *ast.Ellipsis:
		return "..." + typeString(t.Elt)
	default:
		return "unknown"
	}
}
