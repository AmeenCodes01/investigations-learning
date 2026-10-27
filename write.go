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

// dryRunWrite runs a statement inside a transaction, counts the rows it changes,
// and always rolls back, so nothing is saved.
func dryRunWrite(ctx context.Context, db *sql.DB, statement string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() // the whole point: a dry run never commits

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
	return n, nil
}
