package mysql

// Framework code is generated by the generator.

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/pingcap/tidb/parser/ast"
	"github.com/pkg/errors"

	"github.com/bytebase/bytebase/plugin/advisor"
	"github.com/bytebase/bytebase/plugin/advisor/db"
)

var (
	_ advisor.Advisor = (*InsertRowLimitAdvisor)(nil)
	_ ast.Visitor     = (*insertRowLimitChecker)(nil)
)

func init() {
	advisor.Register(db.MySQL, advisor.MySQLInsertRowLimit, &InsertRowLimitAdvisor{})
}

// InsertRowLimitAdvisor is the advisor checking for to limit INSERT rows.
type InsertRowLimitAdvisor struct {
}

// Check checks for to limit INSERT rows.
func (*InsertRowLimitAdvisor) Check(ctx advisor.Context, statement string) ([]advisor.Advice, error) {
	stmtList, errAdvice := parseStatement(statement, ctx.Charset, ctx.Collation)
	if errAdvice != nil {
		return errAdvice, nil
	}

	level, err := advisor.NewStatusBySQLReviewRuleLevel(ctx.Rule.Level)
	if err != nil {
		return nil, err
	}
	payload, err := advisor.UnmarshalNumberTypeRulePayload(ctx.Rule.Payload)
	if err != nil {
		return nil, err
	}
	checker := &insertRowLimitChecker{
		level:  level,
		title:  string(ctx.Rule.Type),
		maxRow: payload.Number,
		driver: ctx.Driver,
		ctx:    ctx.Context,
	}

	if payload.Number > 0 {
		for _, stmt := range stmtList {
			checker.text = stmt.Text()
			checker.line = stmt.OriginTextPosition()
			(stmt).Accept(checker)
		}
	}

	if len(checker.adviceList) == 0 {
		checker.adviceList = append(checker.adviceList, advisor.Advice{
			Status:  advisor.Success,
			Code:    advisor.Ok,
			Title:   "OK",
			Content: "",
		})
	}
	return checker.adviceList, nil
}

type insertRowLimitChecker struct {
	adviceList []advisor.Advice
	level      advisor.Status
	title      string
	text       string
	line       int
	maxRow     int
	driver     *sql.DB
	ctx        context.Context
}

// Enter implements the ast.Visitor interface.
func (checker *insertRowLimitChecker) Enter(in ast.Node) (ast.Node, bool) {
	if node, ok := in.(*ast.InsertStmt); ok {
		if node.Select == nil {
			if len(node.Lists) > checker.maxRow {
				checker.adviceList = append(checker.adviceList, advisor.Advice{
					Status:  checker.level,
					Code:    advisor.InsertTooManyRows,
					Title:   checker.title,
					Content: fmt.Sprintf("\"%s\" inserts %d rows. The count exceeds %d.", checker.text, len(node.Lists), checker.maxRow),
					Line:    checker.line,
				})
			}
		} else if checker.driver != nil {
			res, err := advisor.Query(checker.ctx, checker.driver, fmt.Sprintf("EXPLAIN %s", node.Text()))
			if err != nil {
				checker.adviceList = append(checker.adviceList, advisor.Advice{
					Status:  checker.level,
					Code:    advisor.InsertTooManyRows,
					Title:   checker.title,
					Content: fmt.Sprintf("\"%s\" dry runs failed: %s", checker.text, err.Error()),
					Line:    checker.line,
				})
			} else {
				rowCount, err := getInsertRows(res)
				if err != nil {
					checker.adviceList = append(checker.adviceList, advisor.Advice{
						Status:  checker.level,
						Code:    advisor.Internal,
						Title:   checker.title,
						Content: fmt.Sprintf("failed to get row count for \"%s\": %s", checker.text, err.Error()),
						Line:    checker.line,
					})
				} else if rowCount > int64(checker.maxRow) {
					checker.adviceList = append(checker.adviceList, advisor.Advice{
						Status:  checker.level,
						Code:    advisor.InsertTooManyRows,
						Title:   checker.title,
						Content: fmt.Sprintf("\"%s\" inserts %d rows. The count exceeds %d.", checker.text, rowCount, checker.maxRow),
						Line:    checker.line,
					})
				}
			}
		}
	}

	return in, false
}

// Leave implements the ast.Visitor interface.
func (*insertRowLimitChecker) Leave(in ast.Node) (ast.Node, bool) {
	return in, true
}

func getInsertRows(res []interface{}) (int64, error) {
	// the res struct is []interface{}{columnName, columnTable, rowDataList}
	if len(res) != 3 {
		return 0, errors.Errorf("expected 3 but got %d", len(res))
	}
	rowList, ok := res[2].([]interface{})
	if !ok {
		return 0, errors.Errorf("expected []interface{} but got %t", res[2])
	}
	// mysql> explain insert into td select * from td;
	// +----+-------------+-------+------------+------+---------------+------+---------+------+------+----------+-----------------+
	// | id | select_type | table | partitions | type | possible_keys | key  | key_len | ref  | rows | filtered | Extra           |
	// +----+-------------+-------+------------+------+---------------+------+---------+------+------+----------+-----------------+
	// |  1 | INSERT      | td    | NULL       | ALL  | NULL          | NULL | NULL    | NULL | NULL |     NULL | NULL            |
	// |  1 | SIMPLE      | td    | NULL       | ALL  | NULL          | NULL | NULL    | NULL |    1 |   100.00 | Using temporary |
	// +----+-------------+-------+------------+------+---------------+------+---------+------+------+----------+-----------------+
	if len(rowList) < 2 {
		return 0, errors.Errorf("not found any data")
	}
	// We need the row 2.
	rowTwo, ok := rowList[1].([]interface{})
	if !ok {
		return 0, errors.Errorf("expected []interface{} but got %t", rowList[0])
	}
	// MySQL EXPLAIN statement result has 12 columns.
	if len(rowTwo) != 12 {
		return 0, errors.Errorf("expected 12 but got %d", len(rowTwo))
	}
	// the column 9 is the data 'rows'.
	switch rows := rowTwo[9].(type) {
	case int:
		return int64(rows), nil
	case int64:
		return rows, nil
	case string:
		v, err := strconv.ParseInt(rows, 10, 64)
		if err != nil {
			return 0, errors.Errorf("expected int or int64 but got string(%s)", rows)
		}
		return v, nil
	default:
		return 0, errors.Errorf("expected int or in64 but got %t", rowTwo[9])
	}
}
