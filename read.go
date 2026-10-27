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
