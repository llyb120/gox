package parser

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// ImportAnalyzer 导入分析器
type ImportAnalyzer struct {
	// 系统包映射：包名 -> 导入路径
	systemPackages map[string]string
}

// NewImportAnalyzer 创建新的导入分析器
func NewImportAnalyzer() *ImportAnalyzer {
	return &ImportAnalyzer{
		systemPackages: map[string]string{
			// 常用系统包
			"fmt":      "fmt",
			"strings":  "strings",
			"strconv":  "strconv",
			"time":     "time",
			"math":     "math",
			"os":       "os",
			"io":       "io",
			"bufio":    "bufio",
			"bytes":    "bytes",
			"encoding": "encoding",
			"json":     "encoding/json",
			"xml":      "encoding/xml",
			"base64":   "encoding/base64",
			"hex":      "encoding/hex",
			"url":      "net/url",
			"http":     "net/http",
			"sql":      "database/sql",
			"context":  "context",
			"reflect":  "reflect",
			"sort":     "sort",
			"regexp":   "regexp",
			"path":     "path",
			"filepath": "path/filepath",
			"log":      "log",
			"errors":   "errors",
			"runtime":  "runtime",
			"sync":     "sync",
			"atomic":   "sync/atomic",
			"unicode":  "unicode",
			"utf8":     "unicode/utf8",
		},
	}
}

// AnalyzeImports 分析代码并返回需要的导入
func (ia *ImportAnalyzer) AnalyzeImports(code string) (map[string]string, error) {
	// 创建文件集
	fset := token.NewFileSet()

	// 解析代码
	file, err := parser.ParseFile(fset, "", code, parser.ParseComments)
	if err != nil {
		// 如果解析失败，使用正则表达式进行简单分析
		return ia.analyzeWithRegex(code), nil
	}

	return ia.analyzeAST(file), nil
}

// analyzeAST 通过AST分析导入
func (ia *ImportAnalyzer) analyzeAST(file *ast.File) map[string]string {
	imports := make(map[string]string)

	// 遍历AST查找函数调用和标识符
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			// 检查函数调用
			if fun, ok := x.Fun.(*ast.SelectorExpr); ok {
				if ident, ok := fun.X.(*ast.Ident); ok {
					// 这是一个包.函数 的调用
					if importPath, exists := ia.systemPackages[ident.Name]; exists {
						imports[importPath] = ""
					}
				}
			}
		case *ast.SelectorExpr:
			// 检查选择器表达式（如 fmt.Sprintf）
			if ident, ok := x.X.(*ast.Ident); ok {
				if importPath, exists := ia.systemPackages[ident.Name]; exists {
					imports[importPath] = ""
				}
			}
		case *ast.TypeAssertExpr:
			// 检查类型断言
			if ident, ok := x.Type.(*ast.Ident); ok {
				if importPath, exists := ia.systemPackages[ident.Name]; exists {
					imports[importPath] = ""
				}
			}
		}
		return true
	})

	return imports
}

// analyzeWithRegex 使用正则表达式分析导入（当AST解析失败时使用）
func (ia *ImportAnalyzer) analyzeWithRegex(code string) map[string]string {
	imports := make(map[string]string)

	// 匹配常见的包使用模式
	patterns := []struct {
		pattern  string
		packages []string
	}{
		{`fmt\.`, []string{"fmt"}},
		{`strings\.`, []string{"strings"}},
		{`strconv\.`, []string{"strconv"}},
		{`time\.`, []string{"time"}},
		{`math\.`, []string{"math"}},
		{`os\.`, []string{"os"}},
		{`io\.`, []string{"io"}},
		{`bufio\.`, []string{"bufio"}},
		{`bytes\.`, []string{"bytes"}},
		{`encoding/json\.`, []string{"encoding/json"}},
		{`encoding/xml\.`, []string{"encoding/xml"}},
		{`encoding/base64\.`, []string{"encoding/base64"}},
		{`encoding/hex\.`, []string{"encoding/hex"}},
		{`net/url\.`, []string{"net/url"}},
		{`net/http\.`, []string{"net/http"}},
		{`database/sql\.`, []string{"database/sql"}},
		{`context\.`, []string{"context"}},
		{`reflect\.`, []string{"reflect"}},
		{`sort\.`, []string{"sort"}},
		{`regexp\.`, []string{"regexp"}},
		{`path\.`, []string{"path"}},
		{`path/filepath\.`, []string{"path/filepath"}},
		{`log\.`, []string{"log"}},
		{`errors\.`, []string{"errors"}},
		{`runtime\.`, []string{"runtime"}},
		{`sync\.`, []string{"sync"}},
		{`sync/atomic\.`, []string{"sync/atomic"}},
		{`unicode\.`, []string{"unicode"}},
		{`unicode/utf8\.`, []string{"unicode/utf8"}},
	}

	for _, p := range patterns {
		if regexp.MustCompile(p.pattern).MatchString(code) {
			for _, pkg := range p.packages {
				imports[pkg] = ""
			}
		}
	}

	return imports
}

// MergeImports 合并导入映射
func (ia *ImportAnalyzer) MergeImports(existing, new map[string]string) map[string]string {
	result := make(map[string]string)

	// 复制现有的导入
	for path, name := range existing {
		result[path] = name
	}

	// 添加新的导入
	for path, name := range new {
		if _, exists := result[path]; !exists {
			result[path] = name
		}
	}

	return result
}

// GenerateImportBlock 生成导入块代码
func (ia *ImportAnalyzer) GenerateImportBlock(imports map[string]string) string {
	if len(imports) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.WriteString("import (\n")

	// 按包名排序（简单实现）
	var paths []string
	for path := range imports {
		paths = append(paths, path)
	}

	// 简单的排序：标准库在前，第三方库在后
	var stdLibs, thirdParty []string
	for _, path := range paths {
		// 跳过重复的runtime包
		if path == "runtime" {
			continue
		}
		if strings.Contains(path, ".") && !strings.HasPrefix(path, "golang.org/") {
			thirdParty = append(thirdParty, path)
		} else {
			stdLibs = append(stdLibs, path)
		}
	}

	// 输出标准库
	for _, path := range stdLibs {
		buf.WriteString("\t\"")
		buf.WriteString(path)
		buf.WriteString("\"\n")
	}

	// 如果有第三方库，添加空行分隔
	if len(thirdParty) > 0 && len(stdLibs) > 0 {
		buf.WriteString("\n")
	}

	// 输出第三方库
	for _, path := range thirdParty {
		buf.WriteString("\t\"")
		buf.WriteString(path)
		buf.WriteString("\"\n")
	}

	buf.WriteString(")\n\n")
	return buf.String()
}
