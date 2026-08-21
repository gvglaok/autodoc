# Autodoc

![logo](./logo.png)
## 项目说明

AutoDoc 是一个 CLI 工具，使用 Go 标准库 `go/ast` 扫描 Go 源码目录，生成 API 结构文档（Markdown）和 方法调用图 mermaid（特殊方法字段 可能引起图渲染错误），描述包的常量、变量、结构体和方法及其签名与注释。可配合 AI 工具使用 。

## 运行

```bash
go run . -dir ./path/to/scan -out API.md
```

- `-dir` (必填): 递归扫描的目标目录
- `-out` (默认 `API.md`): 输出 Markdown 文件路径

输出写入 `$targetDir/$outputFile`，项目已包含示例 `API.md`。

## 重要规范

- **所有信息输出必须使用中文**
- **方法、结构体、参数必须写中文注释**
- **每个代码文件行数不得超过 600 行**
- **所有命令的执行必须加超时时间**

## 代码结构

```
main.go        # CLI 入口、AST 遍历器、MD 输出构建器（336 行）
parse.go       # AST 注释提取、辅助工具函数（149 行）
```

## 架构（数据流）

1. **`main.main()`** — 解析命令行参数，对目标目录执行 `filepath.Walk`
2. **`parser.ParseDir()`** — 解析每个目录下的所有 `.go` 文件为 `*ast.Package`
3. **`buildAdvancedCommentIndex()`**（parse.go）— 遍历注释节点，构建 `globalLineComments` 索引，键为 `"文件名：行号"`（归一化为绝对路径）
4. **AST 遍历**（main.go，第 67–276 行）— 迭代 `file.Decls`，按类型分发：
   - `*ast.GenDecl`（CONST/VAR/TYPE）— 提取常量、变量、结构体字段（类型、结构体标签、合并注释）
   - `*ast.FuncDecl` — 提取方法、参数、返回值，支持命名返回值
5. **注释合并** — `ast.Doc` 注释、同行内联注释、基于行索引的侧写注释合并去重（`uniqueJoint()`）
6. **输出** — 结构体及其关联方法通过 `<!--METHODS_<name>-->` 占位符拼接后写入磁盘


## En
> This is an automated documentation generation tool for Go projects  
> designed to generate API documentation suitable for use by AI tools.   
> The documentation is produced in Markdown format.  
> It includes descriptions of parameters, structs, and methods within packages;   
> items lacking comments will appear as blank entries.  
> It also maps out method invocation sequences using Mermaid syntax;   
> installing a Mermaid Markdown preview plugin allows you to visualize these call graphs (note that the Mermaid parser may encounter errors with certain special characters).  

## Usage
```bash
go 1.26 build this package  
run cmd:  
autodoc -dir="/you/want/generate/doc/package/path"
```
at the path you will see API.md     
