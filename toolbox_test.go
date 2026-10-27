package main

// These tests run against the local test database from setup.sql.
// Run them with:  mysql -uroot < setup.sql && source .env && go test ./...

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
)

// testDB connects with the login in envVar, or skips the test if it isn't set.
func testDB(t *testing.T, envVar string) *sql.DB {
	t.Helper()
	if os.Getenv(envVar) == "" {
		t.Skipf("%s not set; run `source .env` first", envVar)
	}
	return openDB(envVar)
}

func TestRunQueryReturnsRows(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RO")

	out, err := runQuery(context.Background(), db, "SELECT id FROM orders WHERE status = 'pending' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(out.Rows); got != "[[7] [19] [33]]" {
		t.Errorf("rows = %s, want the 3 planted pending orders", got)
	}
	if out.Truncated {
		t.Error("3 rows should not be truncated")
	}
}

func TestRunQueryRefusesWrites(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RO")

	_, err := runQuery(context.Background(), db, "UPDATE orders SET status = 'cancelled'")
	if err == nil {
		t.Fatal("UPDATE succeeded through run_query")
	}
}

func TestRunQueryRefusesMultipleStatements(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RO")

	_, err := runQuery(context.Background(), db, "SELECT 1; SELECT 2")
	if err == nil {
		t.Fatal("two statements in one call were accepted")
	}
}

func TestRunQueryTruncatesLargeResults(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RO")

	out, err := runQuery(context.Background(), db,
		"WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 300) SELECT i FROM n")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != maxQueryRows || !out.Truncated {
		t.Errorf("got %d rows, truncated=%v; want %d rows, truncated=true", len(out.Rows), out.Truncated, maxQueryRows)
	}
}

func TestRunQueryStopsSlowQueries(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RO")

	// A three-way cross join of a big system table takes far longer than 5 seconds.
	_, err := runQuery(context.Background(), db,
		"SELECT COUNT(*) FROM information_schema.columns a, information_schema.columns b, information_schema.columns c")
	if err == nil || !strings.Contains(err.Error(), "maximum statement execution time exceeded") {
		t.Fatalf("err = %v, want MySQL's execution time limit", err)
	}
}
