package main

import (
	"context"
	"database/sql"
	"fmt"
)

// Table is one row of list_tables' answer. The json tags are the field names Claude sees.
type Table struct {
	Name string `json:"name"`
	Rows int64  `json:"approx_rows"`
}

type ListTablesOutput struct {
	Tables []Table `json:"tables"`
}

// The jsonschema tag is the description Claude reads for this input.
type DescribeTableInput struct {
	Table string `json:"table" jsonschema:"name of the table, as returned by list_tables"`
}

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"`
}

type DescribeTableOutput struct {
	Columns []Column `json:"columns"`
}

// maxQueryRows caps run_query's answer, protecting both the database and Claude's context.
const maxQueryRows = 200

type RunQueryInput struct {
	SQL string `json:"sql" jsonschema:"one SELECT statement"`
}

type RunQueryOutput struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated" jsonschema:"true if there were more than 200 rows and only the first 200 are shown"`
}

func listTables(ctx context.Context, db *sql.DB) ([]Table, error) {
	// information_schema holds MySQL's own estimates; COUNT(*) would scan every row of every table.
	rows, err := db.QueryContext(ctx, `
		SELECT table_name, COALESCE(table_rows, 0) FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'
		ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := []Table{}
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Rows); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func describeTable(ctx context.Context, db *sql.DB, table string) ([]Column, error) {
	// The table name comes from the LLM, so it goes in as a ? parameter, never pasted into the SQL.
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, column_type, is_nullable FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := []Column{}
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("no table named %q", table)
	}
	return cols, nil
}

func runQuery(ctx context.Context, db *sql.DB, query string) (RunQueryOutput, error) {
	// A read-only transaction is a second lock on top of the read-only MySQL user.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RunQueryOutput{}, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return RunQueryOutput{}, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return RunQueryOutput{}, err
	}

	out := RunQueryOutput{Columns: columns, Rows: [][]any{}}
	for rows.Next() {
		if len(out.Rows) == maxQueryRows {
			out.Truncated = true
			break
		}

		// We don't know the columns in advance, so scan each value into an "any".
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return RunQueryOutput{}, err
		}

		// MySQL sends text as raw bytes, which JSON would turn into unreadable base64.
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		out.Rows = append(out.Rows, values)
	}
	return out, rows.Err()
}
