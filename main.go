// Toolbox: an MCP server that gives AI agents safe tools to inspect and fix our database.
package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	"github.com/go-sql-driver/mysql"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	readDB := openDB("DATABASE_URL_RO")

	server := mcp.NewServer(&mcp.Implementation{Name: "toolbox", Version: "0.2.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tables",
		Description: "List the tables in the database with approximate row counts. Read-only. Use this first to see what data exists.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListTablesOutput, error) {
		tables, err := listTables(ctx, readDB)
		return nil, ListTablesOutput{Tables: tables}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_table",
		Description: "Show a table's columns and their types. Read-only. Use before writing a query against the table.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in DescribeTableInput) (*mcp.CallToolResult, DescribeTableOutput, error) {
		cols, err := describeTable(ctx, readDB, in.Table)
		return nil, DescribeTableOutput{Columns: cols}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "run_query",
		Description: "Run one read-only SQL SELECT and return the rows (at most 200). " +
			"Runs as a read-only database user with a 5 second limit; writes are refused. " +
			"Use describe_table first so you know the column names.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, in RunQueryInput) (*mcp.CallToolResult, RunQueryOutput, error) {
		out, err := runQuery(ctx, readDB, in.SQL)
		return nil, out, err
	})

	// stdout is the line to Claude, so all our own logging goes to stderr (log's default).
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// openDB connects with the MySQL login stored in the given environment variable,
// e.g. "toolbox_ro:password@tcp(localhost:3306)/shop". It exits if that fails.
func openDB(envVar string) *sql.DB {
	dsn := os.Getenv(envVar)
	if dsn == "" {
		log.Fatalf("%s is not set (see .env.example)", envVar)
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		log.Fatalf("%s is not a valid MySQL DSN: %v", envVar, err)
	}

	// Session settings MySQL applies to every connection we open.
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["information_schema_stats_expiry"] = "0" // otherwise row counts are cached for up to 24h
	cfg.Params["max_execution_time"] = "5000"           // MySQL stops any SELECT running over 5 seconds

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		log.Fatalf("%s: %v", envVar, err)
	}
	db := sql.OpenDB(connector)
	if err := db.Ping(); err != nil {
		log.Fatalf("can't reach database with %s: %v", envVar, err)
	}
	return db
}
