// Toolbox: an MCP server that gives AI agents safe tools to inspect our database.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql" // registers the "mysql" driver with database/sql
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func main() {
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// stats_expiry=0: MySQL 8 otherwise caches row counts for up to 24h.
		dsn = "dev:dev@tcp(localhost:3306)/shop?information_schema_stats_expiry=0"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("bad DATABASE_URL: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("can't reach database: %v", err)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "toolbox", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tables",
		Description: "List the tables in the database with approximate row counts. Read-only. Use this first to see what data exists.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListTablesOutput, error) {
		tables, err := listTables(ctx, db)
		return nil, ListTablesOutput{Tables: tables}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_table",
		Description: "Show a table's columns and their types. Read-only. Use before writing a query against the table.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in DescribeTableInput) (*mcp.CallToolResult, DescribeTableOutput, error) {
		cols, err := describeTable(ctx, db, in.Table)
		return nil, DescribeTableOutput{Columns: cols}, err
	})

	// stdout is the line to Claude, so all our own logging goes to stderr (log's default).
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
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
