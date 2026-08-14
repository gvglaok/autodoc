package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"path/filepath"
	"strings"
)

// 全局位置信息上下文，适配 Go 1.25 精准定位
var globalLineComments map[string]string // key: "filename:line" -> text
var fileSet *token.FileSet

// normalizeFilename 将路径统一转换为绝对路径格式
func normalizeFilename(filename string) string {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return filename
	}
	return abs
}

// normalizeTypeName 将接收者或类型名统一为纯名，便于 struct 与 method 关联
func normalizeTypeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "*")
	s = strings.TrimPrefix(s, "[]")
	if idx := strings.LastIndex(s, "."); idx != -1 {
		s = s[idx+1:]
	}
	if i := strings.Index(s, "["); i != -1 {
		s = s[:i]
	}
	return s
}

// buildAdvancedCommentIndex 核心修复：对文件名进行绝对路径规整化处理
func buildAdvancedCommentIndex(pkg *ast.Package) {
	globalLineComments = make(map[string]string)
	for _, file := range pkg.Files {
		for _, commentGroup := range file.Comments {
			for _, comment := range commentGroup.List {
				pos := fileSet.Position(comment.Slash)

				// 关键修复点
				normFile := normalizeFilename(pos.Filename)
				key := fmt.Sprintf("%s:%d", normFile, pos.Line)

				if strings.HasPrefix(comment.Text, "//") {
					cleanText := strings.TrimPrefix(comment.Text, "//")
					cleanText = cleanComment(cleanText)
					if existing, exists := globalLineComments[key]; exists {
						globalLineComments[key] = existing + " | " + cleanText
					} else {
						globalLineComments[key] = cleanText
					}
				}
			}
		}
	}
}

// extractPureASTComments 核心修复：使用绝对路径规整化 Key
func extractPureASTComments(name *ast.Ident, valSpec *ast.ValueSpec, decl *ast.GenDecl) string {
	var pieces []string

	// 1. 获取变量独占的物理顶部注释
	if valSpec.Doc != nil && len(valSpec.Doc.List) > 0 {
		pieces = append(pieces, cleanComment(valSpec.Doc.Text()))
	}

	// 2. 获取变量行自带的右侧单行行尾注释
	if valSpec.Comment != nil && len(valSpec.Comment.List) > 0 {
		pieces = append(pieces, cleanComment(valSpec.Comment.Text()))
	}

	// 3. 基于 Go 原始物理位置切入
	pos := fileSet.Position(name.Pos())
	normFile := normalizeFilename(pos.Filename)
	key := fmt.Sprintf("%s:%d", normFile, pos.Line)

	if sideComment, found := globalLineComments[key]; found {
		alreadyAdded := false
		for _, p := range pieces {
			if p == sideComment {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			pieces = append(pieces, sideComment)
		}
	}

	// 4. 块大框兜底
	if len(pieces) == 0 && decl.Doc != nil {
		pieces = append(pieces, cleanComment(decl.Doc.Text()))
	}

	if len(pieces) == 0 {
		return "_No description_"
	}

	return uniqueJoint(pieces)
}

func cleanComment(c string) string {
	c = strings.ReplaceAll(c, "\n", " ")
	c = strings.ReplaceAll(c, "\r", "")
	c = strings.ReplaceAll(c, "|", "\\|") // 防止破坏 Markdown 的表格管道符
	return strings.TrimSpace(c)
}

func uniqueJoint(arr []string) string {
	m := make(map[string]bool)
	var res []string
	for _, v := range arr {
		if !m[v] && v != "" {
			m[v] = true
			res = append(res, v)
		}
	}
	return strings.Join(res, " | ")
}

func getFieldTypeStr(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", getFieldTypeStr(t.X), t.Sel.Name)
	case *ast.StarExpr:
		return fmt.Sprintf("*%s", getFieldTypeStr(t.X))
	case *ast.ArrayType:
		return fmt.Sprintf("[]%s", getFieldTypeStr(t.Elt))
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", getFieldTypeStr(t.Key), getFieldTypeStr(t.Value))
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		// 简化：只标注为 func，签名复杂时可扩展
		return "func"
	case *ast.Ellipsis:
		return "..." + getFieldTypeStr(t.Elt)
	case *ast.ChanType:
		dir := ""
		if t.Dir == ast.SEND {
			dir = "chan<- "
		} else if t.Dir == ast.RECV {
			dir = "<-chan "
		} else {
			dir = "chan "
		}
		return dir + getFieldTypeStr(t.Value)
	case *ast.ParenExpr:
		return getFieldTypeStr(t.X)
	case *ast.IndexExpr:
		// 泛型实例化：Base[Index]
		return fmt.Sprintf("%s[%s]", getFieldTypeStr(t.X), getFieldTypeStr(t.Index))
	case *ast.IndexListExpr:
		// 多重泛型参数
		var parts []string
		for _, idx := range t.Indices {
			parts = append(parts, getFieldTypeStr(idx))
		}
		return fmt.Sprintf("%s[%s]", getFieldTypeStr(t.X), strings.Join(parts, ","))
	case *ast.BadExpr:
		return ""
	default:
		// 回退：使用 go/printer 将表达式打印为字符串，保证不会返回空导致匹配失败
		var buf bytes.Buffer
		if fileSet != nil {
			_ = printer.Fprint(&buf, fileSet, expr)
			return strings.TrimSpace(buf.String())
		}
		// 最后保底
		return fmt.Sprintf("%T", expr)
	}
}

func getExprStr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.BasicLit:
		return t.Value
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", getExprStr(t.X), t.Sel.Name)
	default:
		return "..."
	}
}

/* TEST is a simple test function */
func TEST(a string, b int) (err error) {
	// 这里可以放置一些测试代码，或者调试用的打印语句
	return nil
}
