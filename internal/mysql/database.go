package mysql

import (
	"context"
	"fmt"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
)

// DatabaseState is the schema observed on one server.
type DatabaseState struct {
	Exists    bool
	Charset   string
	Collation string
}

// EnsureDatabase creates the schema when it is missing.
// An existing schema is left untouched, including its character set.
func EnsureDatabase(ctx context.Context, db DB, name, charset, collation string) (DatabaseState, error) {
	if !ValidIdent(name, 64) {
		return DatabaseState{}, fmt.Errorf("invalid database name %q", name)
	}
	if charset == "" {
		charset = mysqlv1alpha1.DefaultCharacterSet
	}
	if collation == "" {
		collation = mysqlv1alpha1.DefaultCollation
	}
	if !ValidCharset(charset) || !ValidCharset(collation) {
		return DatabaseState{}, fmt.Errorf("invalid character set or collation")
	}
	quoted, err := QuoteIdent(name)
	if err != nil {
		return DatabaseState{}, err
	}
	state, err := readDatabase(ctx, db, name)
	if err != nil {
		return DatabaseState{}, err
	}
	if state.Exists {
		return state, nil
	}
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s CHARACTER SET %s COLLATE %s", quoted, charset, collation)
	if err := exec(ctx, db, query); err != nil {
		return DatabaseState{}, err
	}
	return readDatabase(ctx, db, name)
}

func readDatabase(ctx context.Context, db DB, name string) (DatabaseState, error) {
	query := "SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = " + QuoteLiteral(name)
	rows, err := db.Query(ctx, query)
	if err != nil {
		return DatabaseState{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return DatabaseState{}, rows.Err()
	}
	var state DatabaseState
	if err := rows.Scan(&state.Charset, &state.Collation); err != nil {
		return DatabaseState{}, err
	}
	state.Exists = true
	return state, rows.Err()
}

// ListSchemas returns schema names from information_schema.
func ListSchemas(ctx context.Context, db DB) ([]string, error) {
	rows, err := db.Query(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
