package main

// These tests run against the local test database from setup.sql.
// Run them from the repo root with:  mysql -uroot < setup.sql && source .env && go test ./...

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

// countRows returns the result of a COUNT(*) query, for checking data before and after.
func countRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDryRunWriteCountsRowsButSavesNothing(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RW")
	const pending = "SELECT COUNT(*) FROM orders WHERE status = 'pending'"

	n, err := dryRunWrite(context.Background(), db, "UPDATE orders SET status = 'paid' WHERE status = 'pending'")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("rows affected = %d, want 3", n)
	}
	if got := countRows(t, db, pending); got != 3 {
		t.Errorf("pending orders after dry run = %d, want 3 (unchanged)", got)
	}
}

func TestDryRunWriteRefusesTooManyRows(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RW")
	const customers = "SELECT COUNT(*) FROM customers"
	before := countRows(t, db, customers)

	_, err := dryRunWrite(context.Background(), db,
		"INSERT INTO customers (name) WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 1001) SELECT 'x' FROM n")
	if err == nil {
		t.Fatal("a 1001-row write passed the dry run")
	}
	if after := countRows(t, db, customers); after != before {
		t.Errorf("customers went from %d to %d", before, after)
	}
}

func TestWriteUserCannotChangeSchema(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RW")

	// Schema changes commit immediately in MySQL, so a rollback couldn't undo them: they must be refused outright.
	_, err := dryRunWrite(context.Background(), db, "DROP TABLE orders")
	if err == nil || !strings.Contains(err.Error(), "command denied") {
		t.Fatalf("err = %v, want MySQL to deny DROP", err)
	}
}

const fixStuckOrders = "UPDATE orders SET status = 'paid' WHERE status = 'pending'"
const countPending = "SELECT COUNT(*) FROM orders WHERE status = 'pending'"

func TestExecuteWriteRollsBackOnUnexpectedRowCount(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RW")

	// Pretend the dry run saw 2 rows, but there are now 3.
	_, err := executeWrite(context.Background(), db, fixStuckOrders, 2)
	if err == nil {
		t.Fatal("write with the wrong row count was committed")
	}
	if got := countRows(t, db, countPending); got != 3 {
		t.Errorf("pending orders = %d, want 3 (rolled back)", got)
	}
}

func TestExecuteWriteCommitsWhenRowCountMatches(t *testing.T) {
	db := testDB(t, "DATABASE_URL_RW")
	t.Cleanup(func() { // put the planted bug back for the other tests
		db.Exec("UPDATE orders SET status = 'pending' WHERE id IN (7, 19, 33)")
	})

	n, err := executeWrite(context.Background(), db, fixStuckOrders, 3)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("rows affected = %d, want 3", n)
	}
	if got := countRows(t, db, countPending); got != 0 {
		t.Errorf("pending orders = %d, want 0 (committed)", got)
	}
}
