// Toolbox: an MCP server that gives AI agents safe tools to inspect our database.
package main

import (
	"context"
	"database/sql"
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
