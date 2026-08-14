package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

type packageDoc struct {
	relPath       string
	pkgName       string
	constRows     []string
	varRows       []string
	structBlocks  []string
	methodMap     map[string][]string
	funcBlocks    []string
	callEdges     map[string]map[string]bool
	importAliases map[string]bool
	knownNodes    map[string]bool
}

func main() {
	targetDir := flag.String("dir", "", "Target directory to scan for Go files")
	outputFile := flag.String("out", "API.md", "Output Markdown file name")
	flag.Parse()

	if *targetDir == "" {
		fmt.Println("错误: 请使用 -dir 指定目标目录。例如: -dir '/path/to/scan'")
		flag.Usage()
		return
	}

	if err := run(*targetDir, *outputFile); err != nil {
		fmt.Printf("错误: %v\n", err)
	}
}

// 主处理方法
func run(targetDir, outputFile string) error {
	fileSet = token.NewFileSet()
	baseParent := filepath.Dir(filepath.Clean(targetDir))

	var mdBuilder strings.Builder
	fmt.Fprintln(&mdBuilder, "# 📖 API 结构与语义文档")
	fmt.Fprintln(&mdBuilder)
	fmt.Fprintln(&mdBuilder, "> 通过原生 Go 1.25 AST 引擎直接生成。")
	fmt.Fprintln(&mdBuilder)

	hasContent := false
	err := filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}

		if skipDir(path) {
			return filepath.SkipDir
		}
		if !dirHasGoFiles(path) {
			return nil
		}

		pkgs, err := loadPackages(path)
		if err != nil || len(pkgs) == 0 {
			return nil
		}

		relPath := path
		if rel, relErr := filepath.Rel(baseParent, path); relErr == nil {
			relPath = rel
		}

		for _, pkg := range pkgs {
			doc, err := buildPackageDoc(pkg, relPath)
			if err != nil {
				continue
			}
			if doc == nil {
				continue
			}
			hasContent = true
			mdBuilder.WriteString(doc.String())
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !hasContent {
		return fmt.Errorf("⚠️ 未找到文件")
	}

	outFilePath := filepath.Join(targetDir, outputFile)
	if err := os.WriteFile(outFilePath, []byte(mdBuilder.String()), 0644); err != nil {
		return err
	}
	absPath, _ := filepath.Abs(outFilePath)
	fmt.Printf("🎉 API 文档已导出到:\n%s\n", absPath)
	return nil
}

func skipDir(path string) bool {
	switch filepath.Base(path) {
	case "vendor", ".git", "node_modules":
		return true
	default:
		return false
	}
}

func dirHasGoFiles(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

func loadPackages(dir string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedFiles,
		Dir:  dir,
		Fset: fileSet,
	}
	return packages.Load(cfg, ".")
}

func buildPackageDoc(pkg *packages.Package, relPath string) (*packageDoc, error) {
	astPkg := newASTPackage(pkg)
	buildAdvancedCommentIndex(astPkg)

	doc := &packageDoc{
		relPath:       relPath,
		pkgName:       pkg.Name,
		methodMap:     map[string][]string{},
		constRows:     []string{},
		varRows:       []string{},
		structBlocks:  []string{},
		funcBlocks:    []string{},
		callEdges:     map[string]map[string]bool{},
		importAliases: map[string]bool{},
		knownNodes:    map[string]bool{},
	}

	for _, file := range astPkg.Files {
		collectFileImports(file, doc)
		processFileDecls(file, doc)
	}
	doc.fillMethodBlocks()
	return doc, nil
}

func collectFileImports(file *ast.File, doc *packageDoc) {
	for _, imp := range file.Imports {
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			pathValue := strings.Trim(imp.Path.Value, `"`)
			alias = filepath.Base(pathValue)
		}
		if alias == "_" || alias == "." || alias == "" {
			continue
		}
		doc.importAliases[alias] = true
	}
}

func newASTPackage(pkg *packages.Package) *ast.Package {
	astPkg := &ast.Package{
		Name:  pkg.Name,
		Files: map[string]*ast.File{},
	}
	for _, f := range pkg.Syntax {
		pos := pkg.Fset.Position(f.Pos())
		astPkg.Files[normalizeFilename(pos.Filename)] = f
	}
	return astPkg
}

func processFileDecls(file *ast.File, doc *packageDoc) {
	var funcs []*ast.FuncDecl
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			processGenDecl(d, doc)
		case *ast.FuncDecl:
			registerFuncDecl(d, doc)
			funcs = append(funcs, d)
		}
	}

	for _, d := range funcs {
		processFuncDecl(d, doc)
	}
}

func registerFuncDecl(d *ast.FuncDecl, doc *packageDoc) {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		doc.knownNodes[d.Name.Name] = true
		return
	}
	recvType := normalizeTypeName(getFieldTypeStr(d.Recv.List[0].Type))
	doc.knownNodes[recvType+"."+d.Name.Name] = true
	doc.knownNodes[d.Name.Name] = true
}

func processGenDecl(d *ast.GenDecl, doc *packageDoc) {
	switch d.Tok {
	case token.CONST, token.VAR:
		for _, spec := range d.Specs {
			if valSpec, ok := spec.(*ast.ValueSpec); ok {
				processValueSpec(valSpec, d, doc)
			}
		}
	case token.TYPE:
		for _, spec := range d.Specs {
			if typeSpec, ok := spec.(*ast.TypeSpec); ok {
				appendStructBlock(typeSpec, d, doc)
			}
		}
	}
}

func processValueSpec(valSpec *ast.ValueSpec, decl *ast.GenDecl, doc *packageDoc) {
	for i, name := range valSpec.Names {
		comment := extractPureASTComments(name, valSpec, decl)
		switch decl.Tok {
		case token.CONST:
			display := "iota"
			if valSpec.Type != nil {
				display = getFieldTypeStr(valSpec.Type)
			} else if i < len(valSpec.Values) {
				display = getExprStr(valSpec.Values[i])
			}
			doc.constRows = append(doc.constRows, fmt.Sprintf("| `%s` | `%s` | %s |", name.Name, display, comment))
		case token.VAR:
			typeStr := "auto"
			if valSpec.Type != nil {
				typeStr = getFieldTypeStr(valSpec.Type)
			}
			doc.varRows = append(doc.varRows, fmt.Sprintf("| `%s` | `%s` | %s |", name.Name, typeStr, comment))
		}
	}
}

func appendStructBlock(typeSpec *ast.TypeSpec, decl *ast.GenDecl, doc *packageDoc) {
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "### 🔷 Struct: `%s`\n\n", typeSpec.Name.Name)

	structDoc := ""
	if typeSpec.Doc != nil {
		structDoc = typeSpec.Doc.Text()
	} else if decl.Doc != nil {
		structDoc = decl.Doc.Text()
	}
	if structDoc != "" {
		fmt.Fprintf(&b, "**Summary:** %s\n\n", cleanComment(structDoc))
	}

	fmt.Fprintln(&b, "| Field | Type | Tag | Description |")
	fmt.Fprintln(&b, "| :--- | :--- | :--- | :--- |")

	if structType.Fields == nil || len(structType.Fields.List) == 0 {
		fmt.Fprintln(&b, "| - | - | - | No exposed fields |")
	} else {
		for _, field := range structType.Fields.List {
			fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | %s |\n",
				fieldName(field),
				getFieldTypeStr(field.Type),
				fieldTag(field),
				fieldDescription(field),
			)
		}
	}

	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "<!--METHODS_%s-->\n", normalizeTypeName(typeSpec.Name.Name))
	fmt.Fprintln(&b, "---")
	fmt.Fprintln(&b)

	doc.structBlocks = append(doc.structBlocks, b.String())
}

func processFuncDecl(d *ast.FuncDecl, doc *packageDoc) {
	var caller string
	if d.Recv == nil || len(d.Recv.List) == 0 {
		caller = d.Name.Name
		doc.funcBlocks = append(doc.funcBlocks, renderTopFunction(d))
		doc.knownNodes[caller] = true
	} else {
		recvType := normalizeTypeName(getFieldTypeStr(d.Recv.List[0].Type))
		caller = recvType + "." + d.Name.Name
		doc.methodMap[recvType] = append(doc.methodMap[recvType], renderMethod(d, recvType))
		doc.knownNodes[caller] = true
		doc.knownNodes[d.Name.Name] = true
	}

	collectCallsFromBody(d, caller, doc)
}

func collectCallsFromBody(d *ast.FuncDecl, caller string, doc *packageDoc) {
	if d == nil || d.Body == nil {
		return
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee := getCallName(call.Fun, doc)
		if callee == "" || !doc.knownNodes[callee] {
			return true
		}
		if doc.callEdges[caller] == nil {
			doc.callEdges[caller] = map[string]bool{}
		}
		doc.callEdges[caller][callee] = true
		return true
	})
}

func getCallName(expr ast.Expr, doc *packageDoc) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if xIdent, ok := t.X.(*ast.Ident); ok {
			if doc.importAliases[xIdent.Name] {
				return xIdent.Name + "." + t.Sel.Name
			}
			return t.Sel.Name
		}
		var sb strings.Builder
		_ = formatNode(&sb, t.X)
		return sb.String() + "." + t.Sel.Name
	default:
		var sb strings.Builder
		_ = formatNode(&sb, expr)
		return sb.String()
	}
}

func formatNode(sb *strings.Builder, n ast.Node) error {
	if n == nil {
		return nil
	}
	_, err := sb.WriteString(fmt.Sprintf("%T", n))
	return err
}

func renderTopFunction(d *ast.FuncDecl) string {
	var b strings.Builder
	fmt.Fprintf(&b, "* **`Func %s`**\n", d.Name.Name)
	if d.Doc != nil {
		for _, line := range strings.Split(cleanComment(d.Doc.Text()), "\n") {
			if t := strings.TrimSpace(line); t != "" {
				fmt.Fprintf(&b, "  - %s\n", t)
			}
		}
	}
	if d.Type != nil && d.Type.Params != nil && len(d.Type.Params.List) > 0 {
		fmt.Fprintln(&b, "  - **Inputs:**")
		for _, p := range d.Type.Params.List {
			fmt.Fprintf(&b, "    - `%s` (%s)\n", paramNames(p), getFieldTypeStr(p.Type))
			if c := simpleComment(p); c != "" {
				fmt.Fprintf(&b, "      - %s\n", c)
			}
		}
	}
	if d.Type != nil && d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		fmt.Fprintln(&b, "  - **Outputs:**")
		for i, r := range d.Type.Results.List {
			fmt.Fprintf(&b, "    - `%s` (%s)", resultName(r, i+1), getFieldTypeStr(r.Type))
			if c := simpleComment(r); c != "" {
				fmt.Fprintf(&b, " : *%s*", c)
			}
			fmt.Fprintln(&b)
		}
	}
	return b.String()
}

func (doc *packageDoc) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 📁 Directory: `%s` (Package: `%s`)\n\n", doc.relPath, doc.pkgName)

	// 包级函数
	if len(doc.funcBlocks) > 0 {
		fmt.Fprintln(&b, "### 🔸 Functions / 包级函数")
		fmt.Fprintln(&b)
		for _, f := range doc.funcBlocks {
			fmt.Fprintln(&b, f)
		}
		fmt.Fprintln(&b)
	}

	if len(doc.constRows) > 0 {
		fmt.Fprintln(&b, "### 📌 Constants / 公共常量")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "| Constant Name | Type/Value | Description |")
		fmt.Fprintln(&b, "| :--- | :--- | :--- |")
		for _, row := range doc.constRows {
			fmt.Fprintln(&b, row)
		}
		fmt.Fprintln(&b)
	}

	if len(doc.varRows) > 0 {
		fmt.Fprintln(&b, "### ⚙️ Variables / 公共参数")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "| Variable Name | Type | Description |")
		fmt.Fprintln(&b, "| :--- | :--- | :--- |")
		for _, row := range doc.varRows {
			fmt.Fprintln(&b, row)
		}
		fmt.Fprintln(&b)
	}

	for _, block := range doc.structBlocks {
		b.WriteString(block)
	}

	// 新增：调用流程图（Mermaid）
	if len(doc.callEdges) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "## 🔗 API 方法调用流程图")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "```mermaid")
		fmt.Fprintln(&b, "graph LR;")
		for caller, targets := range doc.callEdges {
			for callee := range targets {
				fmt.Fprintf(&b, "    %s-->%s;\n", sanitizeMermaidNode(caller), sanitizeMermaidNode(callee))
			}
		}
		fmt.Fprintln(&b, "```")
	}

	return b.String()
}

func sanitizeMermaidNode(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	if sb.Len() == 0 {
		return "node"
	}
	return sb.String()
}

func renderMethod(d *ast.FuncDecl, recvType string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "* **`Func (%s) %s`**\n", recvType, d.Name.Name)

	if d.Doc != nil {
		for _, line := range strings.Split(cleanComment(d.Doc.Text()), "\n") {
			if t := strings.TrimSpace(line); t != "" {
				fmt.Fprintf(&b, "  - %s\n", t)
			}
		}
	}

	if d.Type != nil && d.Type.Params != nil && len(d.Type.Params.List) > 0 {
		fmt.Fprintln(&b, "  - **Inputs:**")
		for _, param := range d.Type.Params.List {
			fmt.Fprintf(&b, "    - `%s` (%s)\n", paramNames(param), getFieldTypeStr(param.Type))
			if c := simpleComment(param); c != "" {
				fmt.Fprintf(&b, "      - %s\n", c)
			}
		}
	}

	if d.Type != nil && d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		fmt.Fprintln(&b, "  - **Outputs:**")
		resultIndex := 1
		for _, result := range d.Type.Results.List {
			fmt.Fprintf(&b, "    - `%s` (%s)",
				resultName(result, resultIndex),
				getFieldTypeStr(result.Type),
			)
			if c := simpleComment(result); c != "" {
				fmt.Fprintf(&b, " : *%s*", c)
			}
			fmt.Fprintln(&b)
			resultIndex++
		}
	}

	return b.String()
}

func paramNames(field *ast.Field) string {
	if len(field.Names) == 0 {
		return "_unnamed_"
	}
	names := make([]string, 0, len(field.Names))
	for _, n := range field.Names {
		names = append(names, n.Name)
	}
	return strings.Join(names, ", ")
}

func resultName(field *ast.Field, index int) string {
	if len(field.Names) > 0 {
		return field.Names[0].Name
	}
	return fmt.Sprintf("Result %d", index)
}

func simpleComment(field *ast.Field) string {
	if field.Comment != nil {
		return cleanComment(field.Comment.Text())
	}
	if field.Doc != nil {
		return cleanComment(field.Doc.Text())
	}
	return commentAtPosition(field.Pos())
}

func fieldDescription(field *ast.Field) string {
	if field.Comment != nil {
		return cleanComment(field.Comment.Text())
	}
	if field.Doc != nil {
		return cleanComment(field.Doc.Text())
	}
	if len(field.Names) > 0 {
		return commentAtPosition(field.Names[0].Pos())
	}
	return commentAtPosition(field.Type.Pos())
}

func fieldName(field *ast.Field) string {
	if len(field.Names) == 0 {
		return "_Anonymous/Embedded_"
	}
	names := make([]string, 0, len(field.Names))
	for _, n := range field.Names {
		names = append(names, n.Name)
	}
	return strings.Join(names, ", ")
}

func fieldTag(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	return strings.Trim(field.Tag.Value, "`")
}

func commentAtPosition(pos token.Pos) string {
	position := fileSet.Position(pos)
	return globalLineComments[fmt.Sprintf("%s:%d", normalizeFilename(position.Filename), position.Line)]
}

func (doc *packageDoc) fillMethodBlocks() {
	for idx, block := range doc.structBlocks {
		startIdx := strings.Index(block, "### 🔷 Struct: `")
		if startIdx < 0 {
			continue
		}
		rem := block[startIdx+len("### 🔷 Struct: `"):]
		name, _, ok := strings.Cut(rem, "`")
		if !ok {
			continue
		}

		norm := normalizeTypeName(name)
		methods, ok := doc.methodMap[norm]
		if !ok || len(methods) == 0 {
			continue
		}

		var mb strings.Builder
		fmt.Fprintln(&mb, "#### 🔶 Associated Methods")
		fmt.Fprintln(&mb)
		for _, m := range methods {
			mb.WriteString(m)
			mb.WriteString("\n")
		}
		doc.structBlocks[idx] = strings.ReplaceAll(block, fmt.Sprintf("<!--METHODS_%s-->", norm), mb.String())
	}
}
