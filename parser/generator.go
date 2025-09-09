package parser

import (
	"go/format"
	"go/token"
	"strings"
)

// Generator Go代码生成器
type Generator struct {
	fset           *token.FileSet
	importAnalyzer *ImportAnalyzer
}

// NewGenerator 创建新的生成器
func NewGenerator() *Generator {
	return &Generator{
		fset:           token.NewFileSet(),
		importAnalyzer: NewImportAnalyzer(),
	}
}

// GenerateFile 生成Go文件
func (g *Generator) GenerateFile(goxFile *GoxFile) ([]byte, error) {
	code := goxFile.GeneratedCode

	// 移除编译忽略指令（这些只应该在.gox.go文件中）
	code = g.removeBuildIgnore(code)

	// 使用 ImportAnalyzer 分析并添加必要的导入
	code = g.addNecessaryImports(code)

	// 使用go/format包格式化生成的代码
	formatted, err := format.Source([]byte(code))
	if err != nil {
		return nil, err
	}

	return formatted, nil
}

// addNecessaryImports 使用 ImportAnalyzer 添加必要的导入
func (g *Generator) addNecessaryImports(code string) string {
	// 分析代码中需要的导入
	neededImports, err := g.importAnalyzer.AnalyzeImports(code)
	if err != nil {
		// 如果分析失败，返回原始代码
		return code
	}

	if len(neededImports) == 0 {
		return code
	}

	// 获取现有的导入
	existingImports := g.extractExistingImports(code)

	// 合并导入
	allImports := g.importAnalyzer.MergeImports(existingImports, neededImports)

	// 生成新的导入块
	importBlock := g.importAnalyzer.GenerateImportBlock(allImports)

	// 替换现有的导入块或添加新的导入块
	return g.replaceOrAddImports(code, importBlock)
}

// extractExistingImports 提取现有代码中的导入
func (g *Generator) extractExistingImports(code string) map[string]string {
	imports := make(map[string]string)
	lines := strings.Split(code, "\n")
	var inImport bool
	var importBlock []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "import") {
			inImport = true
			importBlock = append(importBlock, line)
		} else if inImport {
			if strings.Contains(line, ")") {
				// import 块结束
				importBlock = append(importBlock, line)
				inImport = false

				// 解析导入块
				imports = g.parseImportBlock(strings.Join(importBlock, "\n"))
				break
			} else {
				importBlock = append(importBlock, line)
			}
		}
	}

	return imports
}

// parseImportBlock 解析导入块
func (g *Generator) parseImportBlock(importBlock string) map[string]string {
	imports := make(map[string]string)
	lines := strings.Split(importBlock, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
			// 提取导入路径
			importPath := strings.Trim(trimmed, `"`)
			imports[importPath] = ""
		}
	}

	return imports
}

// replaceOrAddImports 替换现有导入块或添加新的导入块
func (g *Generator) replaceOrAddImports(code, importBlock string) string {
	lines := strings.Split(code, "\n")
	var result []string
	var inImport bool
	var packageFound bool
	var importReplaced bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "import") {
			inImport = true
			// 跳过现有的 import 行，稍后替换
			continue
		} else if inImport {
			if strings.Contains(line, ")") {
				// import 块结束，添加新的导入块
				if !importReplaced {
					result = append(result, importBlock)
					importReplaced = true
				}
				inImport = false
			}
			// 跳过 import 块内的所有行
			continue
		} else if strings.HasPrefix(trimmed, "package ") {
			// 找到 package 声明
			if !packageFound {
				result = append(result, line)
				packageFound = true
				// 在 package 声明后添加导入块（如果没有现有导入块）
				if !importReplaced {
					result = append(result, "")
					result = append(result, importBlock)
					importReplaced = true
				}
			}
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// addFmtImport 自动添加fmt包导入
func (g *Generator) addFmtImport(code string) string {
	lines := strings.Split(code, "\n")
	var result []string
	var inImport bool
	var hasFmtImport bool

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 检查是否已经有fmt导入
		if strings.Contains(line, `"fmt"`) {
			hasFmtImport = true
		}

		// 检查import块
		if strings.HasPrefix(trimmed, "import") {
			inImport = true
			result = append(result, line)

			// 如果是单行导入，需要转换为多行
			if !strings.HasSuffix(trimmed, "(") {
				// 单行import，需要插入fmt导入
				if !hasFmtImport {
					result = append(result, `	"fmt"`)
				}
			}
		} else if inImport && strings.Contains(line, ")") {
			// import块结束，如果还没有添加fmt导入，现在添加
			if !hasFmtImport {
				result = append(result, `	"fmt"`)
			}
			result = append(result, line)
			inImport = false
		} else if inImport && strings.Contains(trimmed, `"`) {
			// 在import块中，添加fmt导入
			result = append(result, line)
			if !hasFmtImport && i+1 < len(lines) && strings.Contains(lines[i+1], ")") {
				result = append(result, `	"fmt"`)
				hasFmtImport = true
			}
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// addStringsImport 自动添加strings包导入
func (g *Generator) addStringsImport(code string) string {
	lines := strings.Split(code, "\n")
	var result []string
	var inImport bool
	var hasStringsImport bool

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 检查是否已经有strings导入
		if strings.Contains(line, `"strings"`) {
			hasStringsImport = true
		}

		// 检查import块
		if strings.HasPrefix(trimmed, "import") {
			inImport = true
			result = append(result, line)

			// 如果是单行导入，需要转换为多行
			if !strings.HasSuffix(trimmed, "(") {
				// 单行import，需要插入strings导入
				if !hasStringsImport {
					result = append(result, `	"strings"`)
				}
			}
		} else if inImport && strings.Contains(line, ")") {
			// import块结束，如果还没有添加strings导入，现在添加
			if !hasStringsImport {
				result = append(result, `	"strings"`)
			}
			result = append(result, line)
			inImport = false
		} else if inImport && strings.Contains(trimmed, `"`) {
			// 在import块中，添加strings导入
			result = append(result, line)
			if !hasStringsImport && i+1 < len(lines) && strings.Contains(lines[i+1], ")") {
				result = append(result, `	"strings"`)
				hasStringsImport = true
			}
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// removeBuildIgnore 移除编译忽略指令
func (g *Generator) removeBuildIgnore(code string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 跳过编译忽略指令
		if strings.HasPrefix(trimmed, "//go:build ignore") ||
			strings.HasPrefix(trimmed, "// +build ignore") {
			continue
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}
