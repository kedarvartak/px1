package docs

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Reference facts are read from source files as data. Nothing is compiled or
// run, so the result describes the tree on disk (a pull request's head), not
// the binary doing the reading.

// Command is one px1 subcommand as main dispatches it.
type Command struct {
	Name    string
	Summary string
	Files   []string
	Flags   []Flag
}

// Flag is one command-line flag.
type Flag struct {
	Name    string
	Type    string
	Default string
	Usage   string
}

// goPackage is the parsed non-test files of one directory.
type goPackage struct {
	fset   *token.FileSet
	files  map[string]*ast.File // by base name
	consts map[string]string    // top-level constants with literal values
}

func parseGoDir(dir string) (*goPackage, error) {
	pkg := &goPackage{fset: token.NewFileSet(), files: map[string]*ast.File{}, consts: map[string]string{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(pkg.fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		pkg.files[name] = file
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value := spec.(*ast.ValueSpec)
				for i, ident := range value.Names {
					if i < len(value.Values) {
						if lit, ok := value.Values[i].(*ast.BasicLit); ok {
							pkg.consts[ident.Name] = literal(lit)
						}
					}
				}
			}
		}
	}
	return pkg, nil
}

func (p *goPackage) names() []string {
	names := make([]string, 0, len(p.files))
	for name := range p.files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// literal is the value of a basic literal: strings unquoted, numbers as written.
func literal(lit *ast.BasicLit) string {
	if lit.Kind == token.STRING {
		if s, err := strconv.Unquote(lit.Value); err == nil {
			return s
		}
	}
	if lit.Kind == token.INT || lit.Kind == token.FLOAT {
		return strings.ReplaceAll(lit.Value, "_", "")
	}
	return lit.Value
}

// value renders an expression for a reader: a literal or a known constant as
// its value, anything else as source text marked as an expression.
func (p *goPackage) value(expr ast.Expr, locals map[string]string) (text string, exact bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return literal(e), true
	case *ast.Ident:
		if v, ok := locals[e.Name]; ok {
			return v, true
		}
		if v, ok := p.consts[e.Name]; ok {
			return v, true
		}
		if e.Name == "true" || e.Name == "false" {
			return e.Name, true
		}
	}
	var b strings.Builder
	_ = printer.Fprint(&b, p.fset, expr)
	return b.String(), false
}

// Commands reads the root package: the subcommands main dispatches, the doc
// comment of the function each one calls, and the flags it defines. A
// repository with no main.go has no commands.
func Commands(root string) ([]Command, error) {
	if _, err := os.Stat(filepath.Join(root, "main.go")); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	pkg, err := parseGoDir(root)
	if err != nil {
		return nil, err
	}

	funcs := map[string]*ast.FuncDecl{}
	funcFile := map[string]string{}
	for _, name := range pkg.names() {
		for _, decl := range pkg.files[name].Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				funcs[fn.Name.Name] = fn
				funcFile[fn.Name.Name] = name
			}
		}
	}

	// Flag sets, keyed by the command name given to flag.NewFlagSet.
	type flagSet struct {
		file  string
		flags []Flag
	}
	sets := map[string]flagSet{}
	for _, name := range pkg.names() {
		for _, decl := range pkg.files[name].Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			command, variable := "", ""
			var flags []Flag
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.AssignStmt:
					if len(node.Lhs) == 1 && len(node.Rhs) == 1 {
						if call, ok := node.Rhs[0].(*ast.CallExpr); ok && isSelector(call.Fun, "flag", "NewFlagSet") && len(call.Args) > 0 {
							if ident, ok := node.Lhs[0].(*ast.Ident); ok {
								variable = ident.Name
								command = commandName(call.Args[0])
							}
						}
					}
				case *ast.CallExpr:
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok || variable == "" {
						return true
					}
					if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != variable {
						return true
					}
					if f, ok := pkg.flagFromCall(sel.Sel.Name, node.Args); ok {
						flags = append(flags, f)
					}
				}
				return true
			})
			if command != "" {
				sets[command] = flagSet{file: name, flags: flags}
			}
		}
	}

	// Dispatch: if os.Args[1] == "name" { ... runX(...) ... } in main.
	mainFn := funcs["main"]
	if mainFn == nil || mainFn.Body == nil {
		return nil, errors.New("main.go has no func main")
	}
	var commands []Command
	for _, stmt := range mainFn.Body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok {
			continue
		}
		name := dispatchName(ifStmt.Cond)
		if name == "" {
			continue
		}
		command := Command{Name: name}
		files := map[string]bool{}
		ast.Inspect(ifStmt.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && strings.HasPrefix(ident.Name, "run") && funcs[ident.Name] != nil {
				command.Summary = summary(funcs[ident.Name])
				files[funcFile[ident.Name]] = true
			}
			return true
		})
		if len(files) == 0 {
			continue // not a subcommand: version and the like
		}
		if set, ok := sets[name]; ok {
			command.Flags = set.flags
			files[set.file] = true
		}
		for file := range files {
			command.Files = append(command.Files, file)
		}
		sort.Strings(command.Files)
		commands = append(commands, command)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands, nil
}

func isSelector(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg
}

// commandName is the first word of the name given to flag.NewFlagSet, so
// "docs "+args[0] belongs to the docs command.
func commandName(expr ast.Expr) string {
	if bin, ok := expr.(*ast.BinaryExpr); ok {
		expr = bin.X
	}
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return ""
	}
	fields := strings.Fields(literal(lit))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// dispatchName finds os.Args[1] == "name" in a condition. Names that start
// with "-" are options, not subcommands.
func dispatchName(cond ast.Expr) string {
	name := ""
	ast.Inspect(cond, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || bin.Op != token.EQL {
			return true
		}
		if _, ok := bin.X.(*ast.IndexExpr); !ok {
			return true
		}
		if lit, ok := bin.Y.(*ast.BasicLit); ok && lit.Kind == token.STRING && name == "" {
			if v := literal(lit); !strings.HasPrefix(v, "-") {
				name = v
			}
		}
		return true
	})
	return name
}

// summary turns "runThing does X. More." into "Does X. More."
func summary(fn *ast.FuncDecl) string {
	if fn.Doc == nil {
		return ""
	}
	text := strings.Join(strings.Fields(fn.Doc.Text()), " ")
	text = strings.TrimSpace(strings.TrimPrefix(text, fn.Name.Name))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

var flagTypes = map[string]string{
	"String": "string", "Int": "integer", "Int64": "integer", "Uint": "integer",
	"Bool": "boolean", "Float64": "number", "Duration": "duration",
}

// flagFromCall reads fs.String(name, default, usage), fs.StringVar(ptr, name,
// default, usage) and fs.Var(value, name, usage).
func (p *goPackage) flagFromCall(method string, args []ast.Expr) (Flag, bool) {
	if method == "Var" && len(args) == 3 {
		name, _ := p.value(args[1], nil)
		usage, _ := p.value(args[2], nil)
		return Flag{Name: name, Type: "value", Usage: usage}, true
	}
	base := strings.TrimSuffix(method, "Var")
	kind, ok := flagTypes[base]
	if !ok {
		return Flag{}, false
	}
	if base != method {
		args = args[min(1, len(args)):]
	}
	if len(args) != 3 {
		return Flag{}, false
	}
	name, _ := p.value(args[0], nil)
	usage, _ := p.value(args[2], nil)
	def, exact := p.value(args[1], nil)
	switch {
	case !exact:
		def = "computed: `" + def + "`"
	case def == "" || (kind == "boolean" && def == "false"):
		def = ""
	default:
		def = "`" + def + "`"
	}
	return Flag{Name: name, Type: kind, Default: def, Usage: usage}, true
}

// Tool is one tool the agentic reviewer may offer a model.
type Tool struct {
	Name        string
	Description string
	Arguments   []Field
}

// toolsFile declares the tools, in the Specs method.
const toolsFile = "internal/hosted/agent/tools.go"

// Tools reads the add(enabled, name, description, schema) calls in
// Toolbox.Specs. A repository without that file has no tools.
func Tools(root string) ([]Tool, error) {
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(toolsFile))); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	pkg, err := parseGoDir(filepath.Join(root, filepath.FromSlash(filepath.Dir(toolsFile))))
	if err != nil {
		return nil, err
	}
	var specs *ast.FuncDecl
	for _, decl := range pkg.files[filepath.Base(toolsFile)].Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "Specs" && fn.Recv != nil {
			specs = fn
		}
	}
	if specs == nil {
		return nil, fmt.Errorf("%s: no Specs method", toolsFile)
	}
	locals := map[string]string{}
	var tools []Tool
	var failure error
	ast.Inspect(specs.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			if len(node.Lhs) == 1 && len(node.Rhs) == 1 {
				ident, isIdent := node.Lhs[0].(*ast.Ident)
				lit, isLit := node.Rhs[0].(*ast.BasicLit)
				if isIdent && isLit {
					locals[ident.Name] = literal(lit)
				}
			}
		case *ast.CallExpr:
			ident, ok := node.Fun.(*ast.Ident)
			if !ok || ident.Name != "add" || len(node.Args) != 4 {
				return true
			}
			name, ok1 := pkg.value(node.Args[1], locals)
			description, ok2 := pkg.value(node.Args[2], locals)
			schema, ok3 := pkg.value(node.Args[3], locals)
			if !ok1 || !ok2 || !ok3 {
				failure = fmt.Errorf("%s: a tool's name, description or schema is not a literal or constant", toolsFile)
				return false
			}
			fields, err := SchemaFields([]byte(schema))
			if err != nil {
				failure = fmt.Errorf("%s: tool %s: %w", toolsFile, name, err)
				return false
			}
			tools = append(tools, Tool{Name: name, Description: description, Arguments: fields})
		}
		return true
	})
	if failure != nil {
		return nil, failure
	}
	if len(tools) == 0 {
		return nil, fmt.Errorf("%s: Specs declares no tools", toolsFile)
	}
	return tools, nil
}
