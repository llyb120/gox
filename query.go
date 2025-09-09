package gox

import (
	"fmt"
	"reflect"
	"strings"
)

// Query 表示一个 SQL 查询和其参数
type Query struct {
	sql  string
	args []interface{}
}

// NewQuery 创建一个新的查询实例
func NewQuery(sql string, args ...interface{}) *Query {
	return &Query{
		sql:  sql,
		args: args,
	}
}

// String 返回 SQL 查询字符串
func (q *Query) String() string {
	return q.sql
}

// Args 返回查询参数
func (q *Query) Args() []interface{} {
	return q.args
}

// SQL 返回格式化的 SQL 字符串（仅用于调试）
func (q *Query) SQL() string {
	return q.sql
}

// AddArg 添加一个参数
func (q *Query) AddArg(arg interface{}) {
	q.args = append(q.args, arg)
}

// QueryBuilder 用于构建动态查询
type QueryBuilder struct {
	parts strings.Builder
	args  []interface{}
}

// NewQueryBuilder 创建一个新的查询构建器
func NewQueryBuilder() QueryBuilder {
	return QueryBuilder{
		args: make([]interface{}, 0),
	}
}

// AddText 添加文本片段
func (qb *QueryBuilder) AddText(text any) *QueryBuilder {
	switch text := text.(type) {
	case string:
		//text = strings.TrimSpace(text)
		//text = " " + text + "\n"
		qb.parts.WriteString(text)
		return qb
	case Query:
		qb.parts.WriteString(text.sql)
		qb.args = append(qb.args, text.args...)
		return qb

	default:
		if text == nil {
			return qb
		}
		return qb.AddText(fmt.Sprintf("%v", text))
	}
	return qb
}

// AddParam 添加参数化查询片段
func (qb *QueryBuilder) AddParam(arg interface{}) *QueryBuilder {
	if reflect.TypeOf(arg).Kind() == reflect.Slice {
		s := reflect.ValueOf(arg)
		var sb strings.Builder
		for i := 0; i < s.Len(); i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("?")
			qb.args = append(qb.args, s.Index(i).Interface())
		}
		qb.parts.WriteString(sb.String())
		return qb
	}
	qb.parts.WriteString("?")
	qb.args = append(qb.args, arg)
	return qb
}

// Build 构建最终的查询
func (qb *QueryBuilder) Build() Query {
	sql := qb.parts.String()
	return Query{
		sql:  sql,
		args: qb.args,
	}
}

func Sql(...any) Query {
	panic("我不应该被调用")
}
