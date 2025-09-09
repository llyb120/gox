package parser

import (
	"go/ast"
	"go/token"
)

// SQLBlock 表示一个 SQL 代码块
type SQLBlock struct {
	Start   token.Pos
	End     token.Pos
	Content []SQLNode
	VarName string // 生成的变量名
}

// SQLNode 接口表示 SQL 块中的节点
type SQLNode interface {
	Pos() token.Pos
	End() token.Pos
	String() string
}

// SQLText 表示纯文本 SQL
type SQLText struct {
	StartPos token.Pos
	EndPos   token.Pos
	Text     string
}

func (t *SQLText) Pos() token.Pos { return t.StartPos }
func (t *SQLText) End() token.Pos { return t.EndPos }
func (t *SQLText) String() string { return t.Text }

// SQLExpressionType 表示表达式的类型
type SQLExpressionType int

const (
	// SQLExprText 文本插入表达式 ${expr}
	SQLExprText SQLExpressionType = iota
	// SQLExprParam 参数化表达式 #{expr}
	SQLExprParam
	// SQLExprAtText @ 文本块表达式 @{expr}
	SQLExprAtText
	// SQLExprCode 纯Go代码块 {expr}
	SQLExprCode
	// SQLExprDoubleAtQuery @@{} 查询表达式，返回gox.Query
	SQLExprDoubleAtQuery
)

// SQLExpression 表示嵌入的 Go 表达式
type SQLExpression struct {
	StartPos token.Pos
	EndPos   token.Pos
	Type     SQLExpressionType
	Content  string   // 原始表达式内容（可能是代码块）
	Expr     ast.Expr // 解析后的表达式（简单表达式）或nil（复杂代码块）
}

func (e *SQLExpression) Pos() token.Pos { return e.StartPos }
func (e *SQLExpression) End() token.Pos { return e.EndPos }
func (e *SQLExpression) String() string {
	if e.Type == SQLExprParam {
		return "#{expr}"
	}
	return "{expr}"
}

// GoxFile 表示整个 .gox 文件的 AST
type GoxFile struct {
	*ast.File
	SQLBlocks     []*SQLBlock
	GeneratedCode string // 生成的Go代码
}

// ParserState 表示解析器状态
type ParserState struct {
	file       *token.File
	src        []byte
	pos        int
	sqlCounter int
}

// 位置信息
type Position struct {
	Line   int
	Column int
	Offset int
}
