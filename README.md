# Autodoc
> 这是一个Golang 项目 自动化文档生成工具，用于生成 项目api文档， 
> 此文档可以给AI工具调用，文档格式是markdown，
> 内容包含 包内参数、结构、方法 说明，未写注释的 说明会 留空，
> 还包含每个包内 方法调用顺序，格式为 mermaid， 安装mermaid markdown 预览插件 可以看到调用图（这个mermaid 遇到一些特殊字段会解析错误）
## 使用方法
golang 1.26 编译下  
运行 autodoc -dir="/you/want/generate/doc/package/path"  

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
