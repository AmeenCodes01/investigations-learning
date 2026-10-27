package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// maxWriteRows caps how many rows a single write may change.
const maxWriteRows = 1000

// writeTimeout stops a write that runs too long, e.g. one stuck waiting for a lock.
// (MySQL's own 5 second limit only applies to SELECTs.)
const writeTimeout = 10 * time.Second

type DryRunWriteInput struct {
	SQL string `json:"sql" jsonschema:"one INSERT, UPDATE or DELETE statement"`
}

type DryRunWriteOutput struct {
	RowsAffected int64 `json:"rows_affected" jsonschema:"rows the statement would change; rows already holding the new values are not counted"`
}

type ExecuteWriteInput struct {
	SQL          string `json:"sql" jsonschema:"the exact statement that was dry-run"`
	ExpectedRows int64  `json:"expected_rows" jsonschema:"rows_affected from dry_run_write"`
}

type ExecuteWriteOutput struct {
	RowsAffected int64 `json:"rows_affected"`
}

// dryRunWrite reports how many rows a statement would change, and saves nothing.
func dryRunWrite(ctx context.Context, db *sql.DB, statement string) (int64, error) {
	return runWrite(ctx, db, statement, false, 0)
}

// executeWrite saves a statement's changes, but only if it changes exactly expectedRows rows.
func executeWrite(ctx context.Context, db *sql.DB, statement string, expectedRows int64) (int64, error) {
	return runWrite(ctx, db, statement, true, expectedRows)
}

// runWrite runs one statement inside a transaction and counts the rows it changed.
// It commits only when commit is true and the count equals expectedRows; otherwise it rolls back.
func runWrite(ctx context.Context, db *sql.DB, statement string, commit bool, expectedRows int64) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() // does nothing if we committed

	result, err := tx.ExecContext(ctx, statement)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n > maxWriteRows {
		return n, fmt.Errorf("this would change %d rows; the limit is %d per write", n, maxWriteRows)
	}
	if !commit {
		return n, nil
	}
	if n != expectedRows {
		// The data changed since the dry run, so this is no longer the change that was approved.
		return n, fmt.Errorf("rolled back: changed %d rows but %d were expected; dry-run it again", n, expectedRows)
	}
	return n, tx.Commit()
}
