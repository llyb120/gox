package parser

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

// Parser GoX 解析器
type Parser struct {
	fset           *token.FileSet
	debugMode      bool
	smartScopeMode bool // 智能作用域模式开关
}

// NewParser 创建新的解析器
func NewParser() *Parser {
	return &Parser{
		fset:           token.NewFileSet(),
		debugMode:      false,
		smartScopeMode: false,
	}
}

// SetDebugMode 设置调试模式
func (p *Parser) SetDebugMode(debug bool) {
	p.debugMode = debug
}

// formatGoError 格式化Go解析错误，显示具体的错误位置和上下文
func (p *Parser) formatGoError(err error, filename string, src []byte) error {
	if err == nil {
		return nil
	}

	// 解析错误列表
	if list, ok := err.(scanner.ErrorList); ok {

		// list要反转过来
		for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
			list[i], list[j] = list[j], list[i]
		}

		var errorDetails strings.Builder
		errorDetails.WriteString(fmt.Sprintf("解析 Go 代码失败 (%s):\n", filename))

		lines := strings.Split(string(src), "\n")

		for i, e := range list {
			if i >= 10 { // 限制显示前10个错误
				errorDetails.WriteString(fmt.Sprintf("... 还有 %d 个错误\n", len(list)-i))
				break
			}

			// 获取错误位置
			position := e.Pos

			errorDetails.WriteString(fmt.Sprintf("\n错误 %d: %s\n", i+1, e.Msg))
			errorDetails.WriteString(fmt.Sprintf("位置: 第 %d 行，第 %d 列\n", position.Line, position.Column))

			// 显示错误行及其上下文
			if position.Line > 0 && position.Line <= len(lines) {
				startLine := position.Line - 3
				endLine := position.Line + 2

				if startLine < 1 {
					startLine = 1
				}
				if endLine > len(lines) {
					endLine = len(lines)
				}

				errorDetails.WriteString("代码上下文:\n")
				for lineNum := startLine; lineNum <= endLine; lineNum++ {
					lineContent := ""
					if lineNum <= len(lines) {
						lineContent = lines[lineNum-1]
					}

					marker := "  "
					if lineNum == position.Line {
						marker = "➤ " // 用箭头标记错误行
					}

					errorDetails.WriteString(fmt.Sprintf("%s%4d: %s\n", marker, lineNum, lineContent))

					// 在错误行下方显示错误位置指示器
					if lineNum == position.Line && position.Column > 0 {
						indicator := strings.Repeat(" ", 6+position.Column-1) + "^"
						errorDetails.WriteString(fmt.Sprintf("      %s\n", indicator))
					}
				}
			}
		}

		return fmt.Errorf(errorDetails.String())
	}

	// 如果不是scanner.ErrorList，回退到原始错误格式
	return fmt.Errorf("解析 Go 代码失败: %w", err)
}

// ParseFile 解析 .gox 文件
func (p *Parser) ParseFile(filename string, src []byte) (*GoxFile, error) {
	// 先预处理文件，替换 SQL 块为合法的 Go 代码
	processed, sqlBlocks, err := p.preprocessFile(src)
	if err != nil {
		return nil, fmt.Errorf("预处理失败: %w", err)
	}

	// 解析处理后的 Go 代码
	file, err := parser.ParseFile(p.fset, filename, processed, parser.ParseComments)
	if err != nil {
		if p.debugMode {
			// 调试模式：使用增强的错误格式化，并提供预处理后的代码
			enhancedErr := p.formatGoError(err, filename, processed)

			// 添加预处理后代码的调试信息
			fmt.Printf("\n=== 预处理后的代码 (用于调试) ===\n")
			lines := strings.Split(string(processed), "\n")
			for i, line := range lines {
				fmt.Printf("%4d: %s\n", i+1, line)
			}
			fmt.Printf("=== 预处理后的代码结束 ===\n\n")

			return nil, enhancedErr
		} else {
			// 非调试模式：使用简化的错误信息
			return nil, fmt.Errorf("解析 Go 代码失败: %w", err)
		}
	}

	return &GoxFile{
		File:          file,
		SQLBlocks:     sqlBlocks,
		GeneratedCode: string(processed),
	}, nil
}

// preprocessFile 预处理文件，提取 SQL 块并替换为 Go 代码
func (p *Parser) preprocessFile(src []byte) ([]byte, []*SQLBlock, error) {
	content := string(src)
	var sqlBlocks []*SQLBlock
	sqlCounter := 0

	// 检测文件头的 gox:smart_scope 注释
	p.smartScopeMode = strings.Contains(content, "gox:smart_scope")

	// 使用智能方法查找所有 SQL 块（支持嵌套）
	sqlBlockInfo := p.findSQLBlocks(content)

	// 从后往前替换，避免位置偏移问题
	for i := len(sqlBlockInfo) - 1; i >= 0; i-- {
		info := sqlBlockInfo[i]

		sqlContent := info.Content
		varName := fmt.Sprintf("__gox_sql_%d", sqlCounter)
		sqlCounter++

		// 解析 SQL 块内容
		sqlBlock, err := p.parseSQLBlock(sqlContent, varName)
		if err != nil {
			// 计算在原始文件中的行号
			beforeContent := content[:info.Start]
			lineNum := strings.Count(beforeContent, "\n") + 1

			if p.debugMode {
				fmt.Printf("调试: 解析SQL块失败，内容: %q, 错误: %v\n", sqlContent, err)
				return nil, nil, fmt.Errorf("解析 SQL 块失败 (第 %d 行附近): %w\n\nSQL块内容:\n%s", lineNum, err, sqlContent)
			} else {
				return nil, nil, fmt.Errorf("解析 SQL 块失败 (第 %d 行附近): %w", lineNum, err)
			}
		}

		// 添加到块列表的开头（因为我们是倒序处理的）
		sqlBlocks = append([]*SQLBlock{sqlBlock}, sqlBlocks...)

		// 替换为 Go 代码
		replacement := p.generateGoCodeForSQL(sqlBlock)
		content = content[:info.Start] + replacement + content[info.End:]
	}

	return []byte(content), sqlBlocks, nil
}

// ExpressionMatch 表示找到的表达式匹配
type ExpressionMatch struct {
	Start   int
	End     int
	Content string
	Type    SQLExpressionType
}

// SQLToken 表示SQL中的一个token
type SQLToken struct {
	Type    SQLTokenType
	Content string
	Start   int
	End     int
}

// SQLTokenType token类型
type SQLTokenType int

const (
	SQLTokenText          SQLTokenType = iota // 普通文本
	SQLTokenParam                             // #{expr} 参数化查询
	SQLTokenTextExpr                          // ${expr} 文本表达式
	SQLTokenAtBlock                           // @{...} 文本块
	SQLTokenAtLine                            // @xxx 简写形式，到行尾
	SQLTokenCodeBlock                         // {...} 代码块
	SQLTokenDoubleAtBlock                     // @@{...} 查询块，返回gox.Query
)

// parseSQLBlock 解析 SQL 块内容 - 使用栈式遍历方法
func (p *Parser) parseSQLBlock(sqlContent, varName string) (*SQLBlock, error) {
	// 使用栈式遍历解析SQL内容
	tokens := p.tokenizeSQLContent(sqlContent)
	nodes := p.tokensToNodes(tokens)

	return &SQLBlock{
		Content: nodes,
		VarName: varName,
	}, nil
}

// tokenizeSQLContent 使用栈式遍历将SQL内容token化
func (p *Parser) tokenizeSQLContent(content string) []SQLToken {
	var tokens []SQLToken

	i := 0
	textStart := 0

	for i < len(content) {
		// 检查各种表达式的开始
		if i < len(content)-1 {
			// 检查 #{expr}
			if content[i] == '#' && content[i+1] == '{' {
				// 添加前面的文本
				if i > textStart {
					text := content[textStart:i]
					if strings.TrimSpace(text) != "" {
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenText,
							Content: text,
							Start:   textStart,
							End:     i,
						})
					}
				}

				// 解析 #{expr}
				exprContent, end := p.findMatchingBrace(content, i+2)
				if end != -1 {
					tokens = append(tokens, SQLToken{
						Type:    SQLTokenParam,
						Content: exprContent,
						Start:   i,
						End:     end + 1,
					})
					i = end + 1
					textStart = i
					continue
				}
			}

			// 检查 ${expr}
			if content[i] == '$' && content[i+1] == '{' {
				// 添加前面的文本
				if i > textStart {
					text := content[textStart:i]
					if strings.TrimSpace(text) != "" {
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenText,
							Content: text,
							Start:   textStart,
							End:     i,
						})
					}
				}

				// 解析 ${expr}
				exprContent, end := p.findMatchingBrace(content, i+2)
				if end != -1 {
					tokens = append(tokens, SQLToken{
						Type:    SQLTokenTextExpr,
						Content: exprContent,
						Start:   i,
						End:     end + 1,
					})
					i = end + 1
					textStart = i
					continue
				}
			}

			// 检查 @@{...}、@{...} 和 @xxx 简写形式
			if content[i] == '@' {
				if i+2 < len(content) && content[i+1] == '@' && content[i+2] == '{' {
					// 处理 @@{...} 查询块语法
					// 添加前面的文本
					if i > textStart {
						text := content[textStart:i]
						if strings.TrimSpace(text) != "" {
							tokens = append(tokens, SQLToken{
								Type:    SQLTokenText,
								Content: text,
								Start:   textStart,
								End:     i,
							})
						}
					}

					// 解析 @@{...} 块
					blockContent, end := p.findMatchingBrace(content, i+3)
					if end != -1 {
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenDoubleAtBlock,
							Content: blockContent,
							Start:   i,
							End:     end + 1,
						})
						i = end + 1
						textStart = i
						continue
					}
				} else if i+1 < len(content) && content[i+1] == '{' {
					// 处理 @{...} 块语法
					// 添加前面的文本
					if i > textStart {
						text := content[textStart:i]
						if strings.TrimSpace(text) != "" {
							tokens = append(tokens, SQLToken{
								Type:    SQLTokenText,
								Content: text,
								Start:   textStart,
								End:     i,
							})
						}
					}

					// 解析 @{...} 块
					blockContent, end := p.findMatchingBrace(content, i+2)
					if end != -1 {
						// 存储 @{} 块内容，后续在代码生成时处理
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenAtBlock,
							Content: blockContent, // 存储原始内容用于生成代码
							Start:   i,
							End:     end + 1,
						})
						i = end + 1
						textStart = i
						continue
					}
				} else {
					// 处理 @xxx 简写形式（到行尾为止）
					// 添加前面的文本
					if i > textStart {
						text := content[textStart:i]
						if strings.TrimSpace(text) != "" {
							tokens = append(tokens, SQLToken{
								Type:    SQLTokenText,
								Content: text,
								Start:   textStart,
								End:     i,
							})
						}
					}

					// 查找行尾，同时记录第一个 { 的位置（表示进入Go代码块）
					lineEnd := i + 1
					originalBracePos := -1
					for lineEnd < len(content) && content[lineEnd] != '\n' && content[lineEnd] != '\r' {
						if content[lineEnd] == '{' && originalBracePos == -1 {
							// 仅当不是 #{、${、@{ 开头时，才认为是纯代码块的起始
							prev := lineEnd - 1
							if prev >= i+1 && content[prev] != '#' && content[prev] != '$' && content[prev] != '@' {
								originalBracePos = lineEnd
							}
						}
						lineEnd++
					}

					// 计算@xxx语句的结束位置（去除尾部空白）
					atEnd := lineEnd
					if originalBracePos != -1 {
						atEnd = originalBracePos
						for atEnd > i+1 && (content[atEnd-1] == ' ' || content[atEnd-1] == '\t') {
							atEnd--
						}
					}

					// 提取@后的内容到 atEnd（不包含尾部空格和后续的 {）
					atContent := content[i+1 : atEnd]

					// 尝试智能作用域处理
					smartResult := p.trySmartScopeProcessing(i, atContent, content[i+1:], content)
					if smartResult.ShouldHandle {
						// 将整个跨行内容作为一个智能作用域 AtBlock token
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenAtBlock,
							Content: strings.TrimSpace(smartResult.BlockContent),
							Start:   i,
							End:     smartResult.LineEndPos,
						})

						// 添加换行符
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenText,
							Content: "\n",
							Start:   smartResult.LineEndPos,
							End:     smartResult.LineEndPos,
						})

						i = smartResult.LineEndPos
						textStart = i
						continue
					}

					// 默认处理：单行@xxx语句
					atContent = strings.TrimSpace(atContent)
					if atContent != "" {
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenAtLine,
							Content: atContent,
							Start:   i,
							End:     atEnd,
						})
					}

					// 若同一行紧跟着 { 代码块，则不要吞掉 {，将扫描位置停在 { 处，但要补充换行符
					if originalBracePos != -1 {
						// 在@xxx语句后添加换行符，保证与后续代码块分离
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenText,
							Content: "\n",
							Start:   atEnd,
							End:     atEnd,
						})
						i = originalBracePos
						textStart = originalBracePos
					} else {
						// 普通单行，补充换行并跳到行尾
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenText,
							Content: "\n",
							Start:   lineEnd,
							End:     lineEnd,
						})
						i = lineEnd
						textStart = i
					}
					continue
				}
			}
		}

		// 检查普通代码块 {expr}
		if content[i] == '{' {
			// 确保不是其他语法的一部分
			if i == 0 || (content[i-1] != '#' && content[i-1] != '$' && content[i-1] != '@') {
				// 添加前面的文本
				if i > textStart {
					text := content[textStart:i]
					if strings.TrimSpace(text) != "" {
						tokens = append(tokens, SQLToken{
							Type:    SQLTokenText,
							Content: text,
							Start:   textStart,
							End:     i,
						})
					}
				}

				// 解析 {expr}
				exprContent, end := p.findMatchingBrace(content, i+1)
				if end != -1 {
					tokens = append(tokens, SQLToken{
						Type:    SQLTokenCodeBlock,
						Content: exprContent,
						Start:   i,
						End:     end + 1,
					})
					i = end + 1
					textStart = i
					continue
				}
			}
		}

		i++
	}

	// 添加最后的文本
	if textStart < len(content) {
		text := content[textStart:]
		if strings.TrimSpace(text) != "" {
			tokens = append(tokens, SQLToken{
				Type:    SQLTokenText,
				Content: text,
				Start:   textStart,
				End:     len(content),
			})
		}
	}

	return tokens
}

// tokensToNodes 将tokens转换为SQL节点
func (p *Parser) tokensToNodes(tokens []SQLToken) []SQLNode {
	var nodes []SQLNode

	for _, token := range tokens {
		switch token.Type {
		case SQLTokenText:
			nodes = append(nodes, &SQLText{
				Text: token.Content,
			})

		case SQLTokenParam:
			// #{expr} - 参数化查询
			nodes = append(nodes, &SQLExpression{
				Type:    SQLExprParam,
				Content: token.Content,
				Expr:    p.tryParseExpr(token.Content), // 尝试解析为简单表达式
			})

		case SQLTokenTextExpr:
			// ${expr} - 文本表达式
			nodes = append(nodes, &SQLExpression{
				Type:    SQLExprText,
				Content: token.Content,
				Expr:    p.tryParseExpr(token.Content), // 尝试解析为简单表达式
			})

		case SQLTokenAtBlock:
			// @{...} - 文本块，递归处理内部内容
			nodes = append(nodes, &SQLExpression{
				Type:    SQLExprAtText,
				Content: token.Content,
				Expr:    nil, // @{} 块总是复杂内容
			})

		case SQLTokenAtLine:
			// @xxx - 简写形式，直接输出到行尾的内容
			nodes = append(nodes, &SQLExpression{
				Type:    SQLExprAtText,
				Content: token.Content,
				Expr:    nil, // @xxx 简写形式当作文本处理
			})

		case SQLTokenDoubleAtBlock:
			// @@{...} - 查询块，返回gox.Query
			nodes = append(nodes, &SQLExpression{
				Type:    SQLExprDoubleAtQuery,
				Content: token.Content,
				Expr:    nil, // @@{} 块总是复杂内容
			})

		case SQLTokenCodeBlock:
			// {...} - 纯Go代码块
			nodes = append(nodes, &SQLExpression{
				Type:    SQLExprCode,
				Content: token.Content,
				Expr:    p.tryParseExpr(token.Content), // 尝试解析为简单表达式
			})
		}
	}

	return nodes
}

// tryParseExpr 尝试将内容解析为简单表达式，失败则返回nil
func (p *Parser) tryParseExpr(content string) ast.Expr {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	// 尝试解析为表达式
	expr, err := parser.ParseExpr(content)
	if err != nil {
		return nil // 解析失败，当作复杂代码块处理
	}

	return expr
}

// processCodeBlockExpressions 处理代码块中的 @@{}, @{}, #{}, ${} 表达式
func (p *Parser) processCodeBlockExpressions(codeContent string, builderName string) string {
	result := codeContent

	// 处理所有 @@{...} 表达式（独立查询，返回gox.Query）
	for {
		start := strings.Index(result, "@@{")
		if start == -1 {
			break
		}

		// 查找匹配的 }
		blockContent, end := p.findMatchingBrace(result, start+3)
		if end == -1 {
			break
		}

		// 解析@@{}块内容为独立查询
		varName := fmt.Sprintf("__double_at_query_%d", start)
		nestedBlock, err := p.parseSQLBlock(blockContent, varName)
		if err != nil {
			// 解析失败，跳过
			result = result[:start] + "/* @@{} parse error */" + result[end+1:]
			continue
		}

		// 生成独立查询代码
		queryCode := p.generateGoCodeForSQL(nestedBlock)

		// 将@@{}替换为函数调用，直接返回Query
		replacement := queryCode

		result = result[:start] + replacement + result[end+1:]
	}

	// 查找所有 @{...} 表达式（SQL文本块）
	for {
		start := strings.Index(result, "@{")
		if start == -1 {
			break
		}

		// 查找匹配的 }
		blockContent, end := p.findMatchingBrace(result, start+2)
		if end == -1 {
			break
		}

		// 处理 @{} 块内容中的 #{} 和 ${} 表达式
		processedSQL, paramCalls := p.processSQLPartForParams(blockContent, builderName)

		// 生成替换代码
		var replacement string
		if len(paramCalls) > 0 {
			// 有参数，生成多行代码
			var parts []string
			if processedSQL != "" {
				parts = append(parts, fmt.Sprintf(`%s.AddText(%s)`, builderName, strconv.Quote(processedSQL)))
			}
			for _, paramCall := range paramCalls {
				parts = append(parts, paramCall)
			}
			replacement = strings.Join(parts, "\n\t\t\t")
		} else {
			// 纯文本
			if processedSQL != "" {
				replacement = fmt.Sprintf(`%s.AddText(%s)`, builderName, strconv.Quote(processedSQL))
			} else {
				replacement = ""
			}
		}

		result = result[:start] + replacement + result[end+1:]
	}

	// 新增: 处理单行 @xxx 简写文本输出
	searchStart := 0
	for {
		idx := strings.Index(result[searchStart:], "@")
		if idx == -1 {
			break
		}
		idx += searchStart // 调整为绝对位置

		// 如果是 @@{ 或 @{，则跳过，由上面的逻辑处理
		if idx+2 < len(result) && result[idx+1] == '@' && result[idx+2] == '{' {
			// 跳过 @@{
			searchStart = idx + 3
			continue
		}
		if idx+1 < len(result) && result[idx+1] == '{' {
			// 跳过这个 @{
			searchStart = idx + 2
			continue
		}

		// 找到行尾，同时记录同一行内第一个 { 的位置（不包括 #{、${、@{ 这些前缀）
		lineEnd := idx
		originalBracePos := -1
		for lineEnd < len(result) && result[lineEnd] != '\n' && result[lineEnd] != '\r' {
			if result[lineEnd] == '{' && originalBracePos == -1 {
				prev := lineEnd - 1
				if prev >= idx+1 && result[prev] != '#' && result[prev] != '$' && result[prev] != '@' {
					originalBracePos = lineEnd
				}
			}
			lineEnd++
		}

		// 提取到行尾的全文（用于智能作用域判断）
		lineContent := result[idx+1 : lineEnd] // 保留原始空格，用于括号检测

		// 尝试智能作用域处理
		smartResult := p.trySmartScopeProcessing(idx, lineContent, result[idx+1:], result)
		if smartResult.ShouldHandle {
			var replacementParts []string
			// 为@xxx单行语句在前面先添加换行符
			replacementParts = append(replacementParts, fmt.Sprintf(`%s.AddText(%s)`, builderName, strconv.Quote("\n")))

			// 智能处理跨行内容：解析SQL文本和嵌套的Go代码块
			processed := p.processSmartScopeContent(smartResult.BlockContent, builderName)
			replacementParts = append(replacementParts, processed...)

			replacement := strings.Join(replacementParts, "\n\t\t\t")
			result = result[:idx] + replacement + result[smartResult.LineEndPos:]
			searchStart = idx + len(replacement)
			continue
		}

		// 默认处理：单行@xxx语句（将同一行遇到的 { 视为后续代码块起点，不吞掉它）
		// 计算 @xxx 的实际结束位置（若存在 {，则在其前去除尾部空白）
		atEnd := lineEnd
		if originalBracePos != -1 {
			atEnd = originalBracePos
			for atEnd > idx+1 && (result[atEnd-1] == ' ' || result[atEnd-1] == '\t') {
				atEnd--
			}
		}

		lineForDefault := strings.TrimSpace(result[idx+1 : atEnd])
		if lineForDefault != "" {
			processedSQL, paramCalls := p.processSQLPartForParams(lineForDefault, builderName)

			var replacementParts []string
			// 为@xxx单行语句在前面先添加换行符
			replacementParts = append(replacementParts, fmt.Sprintf(`%s.AddText(%s)`, builderName, strconv.Quote("\n")))
			if processedSQL != "" {
				replacementParts = append(replacementParts, fmt.Sprintf(`%s.AddText(%s)`, builderName, strconv.Quote(processedSQL)))
			}
			replacementParts = append(replacementParts, paramCalls...)

			// 如果后面紧跟代码块，还需要在@xxx语句后添加换行符
			if originalBracePos != -1 {
				replacementParts = append(replacementParts, fmt.Sprintf(`%s.AddText(%s)`, builderName, strconv.Quote("\n")))
			}

			replacement := strings.Join(replacementParts, "\n\t\t\t")

			if originalBracePos != -1 {
				// 不吞掉后续的 { ，仅替换到 atEnd，并确保与后续内容有正确的分隔
				result = result[:idx] + replacement + "\n\t\t\t" + result[atEnd:]
			} else {
				result = result[:idx] + replacement + result[lineEnd:]
			}
			searchStart = idx + len(replacement)
		} else {
			// 无内容，跳至 { 或行尾
			if originalBracePos != -1 {
				searchStart = originalBracePos
			} else {
				searchStart = lineEnd
			}
		}
	}

	// 处理独立的 #{...} 表达式（参数化查询）
	for {
		start := strings.Index(result, "#{")
		if start == -1 {
			break
		}

		// 查找匹配的 }
		braceCount := 1
		end := start + 2
		for end < len(result) && braceCount > 0 {
			if result[end] == '{' {
				braceCount++
			} else if result[end] == '}' {
				braceCount--
			}
			end++
		}

		if braceCount == 0 {
			// 提取参数表达式
			paramExpr := result[start+2 : end-1]
			paramExpr = strings.TrimSpace(paramExpr)

			// 生成 AddParam 调用
			replacement := fmt.Sprintf("%s.AddParam(%s)", builderName, paramExpr)

			// 替换表达式
			result = result[:start] + replacement + result[end:]
		} else {
			break
		}
	}

	// 处理独立的 ${...} 表达式（直接输出变量）
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}

		// 查找匹配的 }
		braceCount := 1
		end := start + 2
		for end < len(result) && braceCount > 0 {
			if result[end] == '{' {
				braceCount++
			} else if result[end] == '}' {
				braceCount--
			}
			end++
		}

		if braceCount == 0 {
			// 提取变量表达式
			varExpr := result[start+2 : end-1]
			varExpr = strings.TrimSpace(varExpr)

			// 生成 AddText 调用
			replacement := fmt.Sprintf("%s.AddText(%s)", builderName, varExpr)

			// 替换表达式
			result = result[:start] + replacement + result[end:]
		} else {
			break
		}
	}

	return result
}

// generateGoCodeForSQL 为 SQL 块生成对应的 Go 代码 - 使用新的栈式解析结果
func (p *Parser) generateGoCodeForSQL(block *SQLBlock) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("%s := gox.NewQueryBuilder()", block.VarName+"_builder"))

	for _, node := range block.Content {
		switch n := node.(type) {
		case *SQLText:
			text := n.Text
			if text == "" {
				break
			}

			// 按行处理文本，跳过注释行
			lines := strings.Split(text, "\n")
			for i, line := range lines {
				trimmedLine := strings.TrimSpace(line)

				// 跳过注释行（以 // 或 -- 开头）
				if strings.HasPrefix(trimmedLine, "//") || strings.HasPrefix(trimmedLine, "--") {
					continue
				}

				// 添加非注释行
				if line != "" || i < len(lines)-1 { // 保留空行，除非是最后一行
					parts = append(parts, fmt.Sprintf("%s.AddText(%s)", block.VarName+"_builder", strconv.Quote(line)))
					if i < len(lines)-1 { // 不是最后一行则添加换行符
						parts = append(parts, fmt.Sprintf("%s.AddText(%s)", block.VarName+"_builder", strconv.Quote("\n")))
					}
				}
			}
		case *SQLExpression:
			switch n.Type {
			case SQLExprText:
				// ${expr} 或 {expr} - 直接输出变量或表达式
				if n.Expr != nil {
					// 简单表达式
					parts = append(parts, fmt.Sprintf("%s.AddText(%s)",
						block.VarName+"_builder", p.exprToString(n.Expr)))
				} else {
					// 复杂代码块 - 处理其中的 @{}, #{}, ${} 表达式
					codeContent := strings.TrimSpace(n.Content)
					processedCode := p.processCodeBlockExpressions(codeContent, block.VarName+"_builder")
					parts = append(parts, processedCode)
				}
			case SQLExprAtText:
				// @{...} - SQL文本块，在智能作用域模式下需要智能处理
				sqlContent := strings.TrimSpace(n.Content)

				if p.smartScopeMode {
					// 智能作用域模式：区分SQL文本和Go代码块
					smartParts := p.processSmartScopeContent(sqlContent, block.VarName+"_builder")
					parts = append(parts, smartParts...)
				} else {
					// 传统模式：直接处理 @{} 块内容
					processedSQL, paramCalls := p.processSQLPartForParams(sqlContent, block.VarName+"_builder")
					if processedSQL != "" {
						parts = append(parts, fmt.Sprintf("%s.AddText(%s)",
							block.VarName+"_builder", strconv.Quote(processedSQL)))
					}
					// 添加参数调用
					for _, paramCall := range paramCalls {
						parts = append(parts, paramCall)
					}
				}
			case SQLExprParam:
				// #{expr} - 参数化表达式
				if n.Expr != nil {
					// 简单表达式
					parts = append(parts, fmt.Sprintf("%s.AddParam(%s)",
						block.VarName+"_builder", p.exprToString(n.Expr)))
				} else {
					// 复杂代码块 - 使用具名返回值包装
					codeContent := strings.TrimSpace(n.Content)
					parts = append(parts, fmt.Sprintf("if __result := func() interface{} {\n\t\t\t%s\n\t\t\treturn nil\n\t\t}(); __result != nil {\n\t\t\t%s.AddParam(__result)\n\t\t}",
						codeContent, block.VarName+"_builder"))
				}
			case SQLExprDoubleAtQuery:
				// @@{...} - 查询块，作为表达式直接返回gox.Query
				// 这种情况不应该在generateGoCodeForSQL中处理，因为@@{}应该在表达式上下文中使用
				// 如果出现在这里，说明用法有误，我们暂时跳过
				continue
			case SQLExprCode:
				// {...} - 纯Go代码块，直接执行，不生成AddText或AddParam
				codeContent := strings.TrimSpace(n.Content)
				processedCode := p.processCodeBlockExpressions(codeContent, block.VarName+"_builder")
				parts = append(parts, processedCode)
			}
		}
	}

	parts = append(parts, fmt.Sprintf("%s := %s.Build()",
		block.VarName, block.VarName+"_builder"))

	return "func()(__result gox.Query) {\n\t\t" + strings.Join(parts, "\n\t\t") + "\n\t\treturn " + block.VarName + "\n\t}()"
}

// exprToString 将表达式转换为字符串
func (p *Parser) exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.BasicLit:
		return e.Value
	case *ast.SelectorExpr:
		return p.exprToString(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		// 处理函数调用
		fun := p.exprToString(e.Fun)
		var args []string
		for _, arg := range e.Args {
			args = append(args, p.exprToString(arg))
		}
		return fun + "(" + strings.Join(args, ", ") + ")"
	case *ast.UnaryExpr:
		return e.Op.String() + p.exprToString(e.X)
	case *ast.BinaryExpr:
		return p.exprToString(e.X) + " " + e.Op.String() + " " + p.exprToString(e.Y)
	case *ast.ParenExpr:
		return "(" + p.exprToString(e.X) + ")"
	default:
		// 对于其他复杂表达式，使用 go/format 包
		var buf strings.Builder
		err := format.Node(&buf, p.fset, expr)
		if err != nil {
			return "/* parse_error */"
		}
		return buf.String()
	}
}

// GetFileSet 返回文件集
func (p *Parser) GetFileSet() *token.FileSet {
	return p.fset
}

// preprocessNestedSQL 预处理代码块中嵌套的SQL块
func (p *Parser) preprocessNestedSQL(content string) (string, error) {
	result := content
	sqlCount := 0

	// 使用智能方法查找所有嵌套的 Query() 调用
	nestedBlocks := p.findSQLBlocks(result)

	// 从后往前替换，避免位置偏移问题
	for i := len(nestedBlocks) - 1; i >= 0; i-- {
		block := nestedBlocks[i]

		sqlContent := block.Content
		sqlBlock, err := p.parseSQLBlock(sqlContent, fmt.Sprintf("__nested_sql_%d", sqlCount))
		if err != nil {
			return "", fmt.Errorf("解析嵌套SQL块失败: %v", err)
		}

		// 生成嵌套SQL块的代码
		nestedCode := p.generateGoCodeForSQL(sqlBlock)

		// 替换原始的Query()调用
		result = result[:block.Start] + nestedCode + result[block.End:]

		sqlCount++
	}

	return result, nil
}

// SQLBlockInfo 表示找到的SQL块信息
type SQLBlockInfo struct {
	Start   int    // 块的开始位置（包含Query函数调用）
	End     int    // 块的结束位置（包含右括号）
	Content string // SQL内容（不包含Query函数调用和引号）
}

// findSQLBlocks 智能查找所有SQL块，支持嵌套 - 新语法 Query(`...`) 和 Query('...')
func (p *Parser) findSQLBlocks(content string) []SQLBlockInfo {
	var blocks []SQLBlockInfo
	i := 0

	for i < len(content) {
		// 跳过普通字符串字面量（双引号）
		if content[i] == '"' {
			i = p.skipStringLiteral(content, i, '"')
			continue
		}

		// 查找 "gox.Sql(" 或 "runtime.Query(" 函数调用
		var funcLen int
		var isQueryCall bool

		if i+8 <= len(content) && content[i:i+8] == "gox.Sql(" {
			funcLen = 8
			isQueryCall = true
		} else if i+15 <= len(content) && content[i:i+15] == "runtime.Query(" {
			funcLen = 15
			isQueryCall = true
		}

		if isQueryCall {
			j := i + funcLen // 跳过 "Query(" 或 "Q("

			// 跳过空格
			for j < len(content) && (content[j] == ' ' || content[j] == '\t' || content[j] == '\n') {
				j++
			}

			// 先判断是否为支持的三种 SQL 包裹形式：反引号、单引号、注释块 /* */
			if j >= len(content) {
				i++
				continue
			}

			var (
				sqlContent string // 提取出的 SQL 文本
				endPos     int    // 当前 Query(...) 调用整体的结束位置（包含右括号）
			)

			// 情况 1：反引号或单引号包裹
			if content[j] == '`' || content[j] == '\'' {
				quoteChar := content[j]
				isRawString := quoteChar == '`'

				contentStart := j + 1 // 跳过开始引号
				sqlEnd := p.findMatchingQuote(content, contentStart, quoteChar, isRawString)
				if sqlEnd == -1 {
					i = j + 1
					continue
				}

				// 查找函数调用的结束括号
				closeParenPos := sqlEnd + 1 // 跳过结束引号
				for closeParenPos < len(content) && (content[closeParenPos] == ' ' || content[closeParenPos] == '\t' || content[closeParenPos] == '\n') {
					closeParenPos++
				}

				if closeParenPos >= len(content) || content[closeParenPos] != ')' {
					i = j + 1
					continue
				}

				sqlContent = content[contentStart:sqlEnd]
				endPos = closeParenPos + 1

				// 情况 2：/* ... */ 注释块包裹
			} else if content[j] == '/' && j+1 < len(content) && content[j+1] == '*' {
				commentStart := j + 2 // 跳过 "/*"

				// 兼容 /** 形式，跳过额外的 *
				for commentStart < len(content) && content[commentStart] == '*' {
					commentStart++
				}

				commentEnd := commentStart
				for commentEnd < len(content)-1 && !(content[commentEnd] == '*' && content[commentEnd+1] == '/') {
					commentEnd++
				}

				if commentEnd >= len(content)-1 {
					i = j + 1
					continue
				}

				sqlContent = content[commentStart:commentEnd]

				// 跳过 "*/"
				afterComment := commentEnd + 2

				// 查找函数调用的结束括号
				closeParenPos := afterComment
				for closeParenPos < len(content) && (content[closeParenPos] == ' ' || content[closeParenPos] == '\t' || content[closeParenPos] == '\n') {
					closeParenPos++
				}

				if closeParenPos >= len(content) || content[closeParenPos] != ')' {
					i = j + 1
					continue
				}

				endPos = closeParenPos + 1
			} else {
				// 既不是引号也不是注释块
				i = i + 5
				continue
			}

			// 记录 SQL 块信息
			blocks = append(blocks, SQLBlockInfo{
				Start:   i,
				End:     endPos,
				Content: sqlContent,
			})

			i = endPos
		} else {
			i++
		}
	}

	return blocks
}

// autoAddReturn 智能添加return语句
func (p *Parser) autoAddReturn(content string) string {
	lines := strings.Split(content, "\n")

	// 移除空行和注释，只保留实际代码行
	var codeLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			codeLines = append(codeLines, trimmed)
		}
	}

	// 如果只有一行代码，且是gox.Sql()调用，自动添加return
	if len(codeLines) == 1 {
		line := codeLines[0]
		if strings.HasPrefix(line, "gox.Sql(") &&
			!strings.HasPrefix(line, "return ") {
			return "return " + content
		}
	}

	return content
}

// processSQLPartForParams 处理SQL片段中的 #{} 、${}、@ 单行 以及嵌套 {} 代码块表达式，返回处理后的SQL和参数调用
func (p *Parser) processSQLPartForParams(sqlPart string, builderName string) (string, []string) {
	var calls []string
	var textBuf strings.Builder

	// flushText 会把当前累计的普通文本输出为 AddText 调用
	flushText := func() {
		if textBuf.Len() > 0 {
			calls = append(calls, fmt.Sprintf("%s.AddText(%s)", builderName, strconv.Quote(textBuf.String())))
			textBuf.Reset()
		}
	}

	i := 0
	for i < len(sqlPart) {
		// 1. 处理 #{ ... } 参数占位
		if i+1 < len(sqlPart) && sqlPart[i] == '#' && sqlPart[i+1] == '{' {
			if content, end := p.findMatchingBrace(sqlPart, i+2); end != -1 {
				flushText()
				calls = append(calls, fmt.Sprintf("%s.AddParam(%s)", builderName, strings.TrimSpace(content)))
				i = end + 1
				continue
			}
		}

		// 2. 处理 ${ ... } 文本表达式
		if i+1 < len(sqlPart) && sqlPart[i] == '$' && sqlPart[i+1] == '{' {
			if content, end := p.findMatchingBrace(sqlPart, i+2); end != -1 {
				flushText()
				calls = append(calls, fmt.Sprintf("%s.AddText(%s)", builderName, strings.TrimSpace(content)))
				i = end + 1
				continue
			}
		}

		// 3. 处理嵌套的纯 Go 代码块 { ... }
		if sqlPart[i] == '{' && (i == 0 || (sqlPart[i-1] != '#' && sqlPart[i-1] != '$' && sqlPart[i-1] != '@')) {
			if content, end := p.findMatchingBrace(sqlPart, i+1); end != -1 {
				flushText()
				processed := p.processCodeBlockExpressions(strings.TrimSpace(content), builderName)
				if processed != "" {
					calls = append(calls, processed)
				}
				i = end + 1
				continue
			}
		}

		// 4. 处理 @@{ ... } 查询块（允许出现在单行内，递归为独立查询生成代码）
		if i+2 < len(sqlPart) && sqlPart[i] == '@' && sqlPart[i+1] == '@' && sqlPart[i+2] == '{' {
			if blockContent, end := p.findMatchingBrace(sqlPart, i+3); end != -1 {
				flushText()
				varName := fmt.Sprintf("__double_at_query_%d", i)
				nestedBlock, err := p.parseSQLBlock(blockContent, varName)
				if err == nil {
					queryCode := p.generateGoCodeForSQL(nestedBlock)
					calls = append(calls, queryCode)
				}
				i = end + 1
				continue
			}
		}

		// 5. 处理 @{ ... } 文本块（允许出现在单行内，等价于单行语法糖的递归处理）
		if i+1 < len(sqlPart) && sqlPart[i] == '@' && sqlPart[i+1] == '{' {
			if blockContent, end := p.findMatchingBrace(sqlPart, i+2); end != -1 {
				flushText()
				processedSQL, paramCalls := p.processSQLPartForParams(blockContent, builderName)
				if processedSQL != "" {
					calls = append(calls, fmt.Sprintf("%s.AddText(%s)", builderName, strconv.Quote(processedSQL)))
				}
				calls = append(calls, paramCalls...)
				i = end + 1
				continue
			}
		}

		// 6. 处理 @xxx 单行快捷文本（自动追加换行）
		if sqlPart[i] == '@' {
			// 跳过 @@{ 或 @{ 的块语法
			if !(i+1 < len(sqlPart) && (sqlPart[i+1] == '{' || (sqlPart[i+1] == '@' && i+2 < len(sqlPart) && sqlPart[i+2] == '{'))) {
				flushText()

				// 检查是否启用了智能作用域模式
				if p.smartScopeMode {
					// 在智能作用域模式下，检查 @ 语句中是否包含 (
					lineEnd := i + 1
					for lineEnd < len(sqlPart) && sqlPart[lineEnd] != '\n' && sqlPart[lineEnd] != '\r' {
						lineEnd++
					}
					lineContent := sqlPart[i+1 : lineEnd] // 保留原始空格

					// 尝试智能作用域处理
					smartResult := p.trySmartScopeProcessing(i, lineContent, sqlPart[i+1:], sqlPart)
					if smartResult.ShouldHandle {
						// 在智能作用域模式下，直接将跨行内容作为文本处理，不进行递归解析
						calls = append(calls, fmt.Sprintf("%s.AddText(%s)", builderName, strconv.Quote(strings.TrimSpace(smartResult.BlockContent))))

						// 添加换行符
						calls = append(calls, fmt.Sprintf("%s.AddText(%s)", builderName, strconv.Quote("\n")))
						i = smartResult.LineEndPos
						continue
					}
				}

				// 默认处理 @xxx 单行快捷文本（自动追加换行）
				lineEnd := i + 1
				for lineEnd < len(sqlPart) && sqlPart[lineEnd] != '\n' && sqlPart[lineEnd] != '\r' {
					lineEnd++
				}
				lineContent := strings.TrimSpace(sqlPart[i+1 : lineEnd])
				if lineContent != "" {
					_, subCalls := p.processSQLPartForParams(lineContent, builderName) // 递归处理可能出现的 #{} / ${}
					calls = append(calls, subCalls...)
				}
				// 添加换行符
				calls = append(calls, fmt.Sprintf("%s.AddText(%s)", builderName, strconv.Quote("\n")))
				i = lineEnd
				continue
			}
		}

		// 默认处理普通字符
		textBuf.WriteByte(sqlPart[i])
		i++
	}

	flushText()
	return "", calls
}

// findMatchingQuote 查找匹配的引号，支持转义
func (p *Parser) findMatchingQuote(content string, start int, quoteChar byte, isRawString bool) int {
	i := start

	for i < len(content) {
		if content[i] == quoteChar {
			// 找到匹配的结束引号
			return i
		}

		// 如果不是原始字符串，处理转义字符
		if !isRawString && content[i] == '\\' && i+1 < len(content) {
			i++ // 跳过转义字符
		}
		i++
	}

	return -1 // 没有找到匹配的引号
}

// findMatchingBrace 找到匹配的右大括号，并返回内容和结束位置
func (p *Parser) findMatchingBrace(content string, start int) (string, int) {
	braceCount := 1
	i := start

	for i < len(content) && braceCount > 0 {
		switch content[i] {
		case '{':
			braceCount++
		case '}':
			braceCount--
		case '"', '\'', '`':
			// 跳过字符串字面量
			i = p.skipStringLiteral(content, i, content[i]) - 1 // -1 因为for循环会+1
		}
		i++
	}

	if braceCount == 0 {
		return content[start : i-1], i - 1 // 返回内容和结束位置（不包含}）
	}

	return "", -1 // 没有找到匹配的大括号
}

// findMatchingParen 找到匹配的右圆括号，并返回内容和结束位置
func (p *Parser) findMatchingParen(content string, start int) (string, int) {
	parenCount := 1
	i := start

	for i < len(content) && parenCount > 0 {
		switch content[i] {
		case '(':
			parenCount++
		case ')':
			parenCount--
		case '"', '\'', '`':
			// 跳过字符串字面量
			i = p.skipStringLiteral(content, i, content[i]) - 1 // -1 因为for循环会+1
		}
		i++
	}

	if parenCount == 0 {
		return content[start : i-1], i - 1 // 返回内容和结束位置（不包含)）
	}

	return "", -1 // 没有找到匹配的圆括号
}

// findControlStructureParen 找到控制结构的左括号（跳过函数调用的括号）
func (p *Parser) findControlStructureParen(content string) int {
	i := 0
	lastParen := -1

	for i < len(content) {
		switch content[i] {
		case '(':
			// 检查这个左括号前面是否紧跟着标识符（函数名）
			// 如果是，则跳过这个函数调用
			if p.isFollowingIdentifier(content, i) {
				// 跳过整个函数调用
				parenCount := 1
				i++
				for i < len(content) && parenCount > 0 {
					if content[i] == '(' {
						parenCount++
					} else if content[i] == ')' {
						parenCount--
					} else if content[i] == '"' || content[i] == '\'' || content[i] == '`' {
						// 跳过字符串字面量
						i = p.skipStringLiteral(content, i, content[i]) - 1 // -1 因为外层循环会+1
					}
					i++
				}
				continue
			} else {
				// 这是控制结构的左括号
				lastParen = i
			}
		case '"', '\'', '`':
			// 跳过字符串字面量
			i = p.skipStringLiteral(content, i, content[i]) - 1 // -1 因为for循环会+1
		}
		i++
	}

	return lastParen
}

// skipStringLiteral 跳过字符串字面量，返回跳过后的位置
func (p *Parser) skipStringLiteral(content string, pos int, quote byte) int {
	if pos >= len(content) || content[pos] != quote {
		return pos
	}

	i := pos + 1 // 跳过开始引号
	for i < len(content) && content[i] != quote {
		if content[i] == '\\' && i+1 < len(content) && quote != '`' {
			i++ // 跳过转义字符（原始字符串不需要转义）
		}
		i++
	}

	if i < len(content) {
		i++ // 跳过结束引号
	}

	return i
}

// isFollowingIdentifier 检查指定位置的左括号前面是否紧跟着标识符（无空格）
func (p *Parser) isFollowingIdentifier(content string, parenPos int) bool {
	if parenPos == 0 {
		return false
	}

	// 检查紧邻的前一个字符是否是标识符字符（不跳过空格）
	prevChar := content[parenPos-1]

	// 只有当左括号紧跟标识符字符时才认为是函数调用
	if (prevChar >= 'a' && prevChar <= 'z') ||
		(prevChar >= 'A' && prevChar <= 'Z') ||
		(prevChar >= '0' && prevChar <= '9') ||
		prevChar == '_' {
		return true
	}

	return false
}

// SmartScopeResult 智能作用域处理结果
type SmartScopeResult struct {
	ShouldHandle bool   // 是否应该进行智能作用域处理
	EndPos       int    // 结束位置（绝对位置）
	LineEndPos   int    // 行尾位置（绝对位置）
	BlockContent string // 块内容
}

// trySmartScopeProcessing 尝试智能作用域处理
// basePos: @ 符号的位置
// lineContent: 当前行内容（从@后开始）
// fullContent: 完整内容（从@后开始）
// totalContent: 完整内容（用于计算行尾）
func (p *Parser) trySmartScopeProcessing(basePos int, lineContent string, fullContent string, totalContent string) SmartScopeResult {
	result := SmartScopeResult{ShouldHandle: false}

	// 检查智能作用域模式：如果包含 ( 但 ) 不在同一行
	if !p.smartScopeMode || !strings.Contains(lineContent, "(") {
		return result
	}

	// 找到控制结构的左括号（跳过函数调用）
	parenIndex := p.findControlStructureParen(lineContent)
	if parenIndex == -1 {
		return result
	}

	// 在完整内容中查找匹配的 )
	_, endParen := p.findMatchingParen(fullContent, parenIndex+1)
	if endParen == -1 {
		return result
	}

	// 检查 ) 是否在同一行
	if endParen < len(lineContent) {
		return result // 在同一行，不需要智能作用域处理
	}

	// ) 不在同一行，应用智能作用域
	actualEndPos := basePos + 1 + endParen // 计算到)的绝对位置

	// 找到 ) 所在行的结尾
	blockEndLine := actualEndPos + 1 // 从)后的位置开始查找行尾
	for blockEndLine < len(totalContent) && totalContent[blockEndLine] != '\n' && totalContent[blockEndLine] != '\r' {
		blockEndLine++
	}

	// 提取块内容
	blockContent := totalContent[basePos+1 : blockEndLine]

	result.ShouldHandle = true
	result.EndPos = actualEndPos
	result.LineEndPos = blockEndLine
	result.BlockContent = blockContent

	return result
}

// processSmartScopeContent 智能处理跨行作用域内容，区分SQL文本和Go代码块
func (p *Parser) processSmartScopeContent(content string, builderName string) []string {
	var parts []string
	i := 0

	for i < len(content) {
		// 跳过空白字符
		for i < len(content) && (content[i] == ' ' || content[i] == '\t' || content[i] == '\n' || content[i] == '\r') {
			i++
		}

		if i >= len(content) {
			break
		}

		// 检查特殊表达式：#{...}、${...}、@{...}、@@{...}
		if i+1 < len(content) {
			// 处理 #{...} 表达式
			if content[i] == '#' && content[i+1] == '{' {
				if exprContent, end := p.findMatchingBrace(content, i+2); end != -1 {
					parts = append(parts, fmt.Sprintf("%s.AddParam(%s)", builderName, strings.TrimSpace(exprContent)))
					i = end + 1
					continue
				}
			}

			// 处理 ${...} 表达式
			if content[i] == '$' && content[i+1] == '{' {
				if exprContent, end := p.findMatchingBrace(content, i+2); end != -1 {
					parts = append(parts, fmt.Sprintf("%s.AddText(%s)", builderName, strings.TrimSpace(exprContent)))
					i = end + 1
					continue
				}
			}

			// 处理 @@{...} 表达式
			if i+2 < len(content) && content[i] == '@' && content[i+1] == '@' && content[i+2] == '{' {
				if blockContent, end := p.findMatchingBrace(content, i+3); end != -1 {
					varName := fmt.Sprintf("__double_at_query_%d", i)
					nestedBlock, err := p.parseSQLBlock(blockContent, varName)
					if err == nil {
						queryCode := p.generateGoCodeForSQL(nestedBlock)
						parts = append(parts, queryCode)
					}
					i = end + 1
					continue
				}
			}

			// 处理 @{...} 表达式
			if content[i] == '@' && content[i+1] == '{' {
				if blockContent, end := p.findMatchingBrace(content, i+2); end != -1 {
					processedSQL, paramCalls := p.processSQLPartForParams(blockContent, builderName)
					if processedSQL != "" {
						parts = append(parts, fmt.Sprintf("%s.AddText(%s)", builderName, strconv.Quote(processedSQL)))
					}
					parts = append(parts, paramCalls...)
					i = end + 1
					continue
				}
			}
		}

		// 检查是否是普通Go代码块 {
		if content[i] == '{' {
			// 确保不是其他表达式的一部分
			if i == 0 || (content[i-1] != '#' && content[i-1] != '$' && content[i-1] != '@') {
				// 找到匹配的 }
				blockContent, endPos := p.findMatchingBrace(content, i+1)
				if endPos != -1 {
					// 递归处理Go代码块内容
					processedCode := p.processCodeBlockExpressions(blockContent, builderName)
					if processedCode != "" {
						parts = append(parts, processedCode)
					}
					i = endPos + 1
					continue
				}
			}
		}

		// 处理SQL文本部分 - 找到下一个特殊字符或内容结尾
		textStart := i
		for i < len(content) {
			// 遇到特殊字符就停止
			if content[i] == '{' || content[i] == '#' || content[i] == '$' || content[i] == '@' {
				// 但是要确保不是在字符串中间
				break
			}
			i++
		}

		if i > textStart {
			sqlText := strings.TrimSpace(content[textStart:i])
			if sqlText != "" {
				// 对于纯SQL文本，直接添加为AddText调用
				parts = append(parts, fmt.Sprintf("%s.AddText(%s)", builderName, strconv.Quote(sqlText)))
			}
		}
	}

	return parts
}
