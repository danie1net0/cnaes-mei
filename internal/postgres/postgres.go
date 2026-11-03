// Package postgres stores the collected activities in a PostgreSQL table, so
// that a CNPJ database can join its establishments against the CNAEs an MEI is
// allowed to use.
package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"gitlab.com/danie1net0/cnaes-mei/internal/mei"
)

// DefaultTable is where the activities are stored when no other name is given.
const DefaultTable = "cnae_mei"

// identifier is a conservative spelling for a table name: lowercase, no quoting
// needed, optionally qualified by a schema. Anything else is rejected instead of
// being quoted, because a table name that needs quoting in this tool is far more
// likely to be a mistake than a deliberate choice.
var identifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*(\.[a-z_][a-z0-9_]*)?$`)

// DB is a connection to the database.
type DB struct {
	conn *pgx.Conn
}

// Connect opens a connection with the given URI.
func Connect(ctx context.Context, uri string) (*DB, error) {
	conn, err := pgx.Connect(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("could not connect to PostgreSQL: %w", err)
	}
	return &DB{conn: conn}, nil
}

// Close ends the connection.
func (db *DB) Close(ctx context.Context) error {
	return db.conn.Close(ctx)
}

// Replace makes the table hold exactly the given activities.
//
// Everything happens in one transaction: readers either see the previous
// contents or the new ones, never an empty table halfway through. The table is
// created when missing, so the first run needs no migration.
func (db *DB) Replace(ctx context.Context, table string, activities []mei.Activity) error {
	name, err := tableName(table)
	if err != nil {
		return err
	}

	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("could not start the transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a rollback after a commit is a no-op

	if _, err := tx.Exec(ctx, createTable(name)); err != nil {
		return fmt.Errorf("could not create the table %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, "TRUNCATE TABLE "+name); err != nil {
		return fmt.Errorf("could not empty the table %s: %w", name, err)
	}

	rows := make([][]any, 0, len(activities))
	for _, a := range activities {
		rows = append(rows, []any{a.CNAE, a.Occupation, a.Letter})
	}
	if _, err := tx.CopyFrom(ctx, identifierOf(name), []string{"cnae", "ocupacao", "letra"}, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("could not write to the table %s: %w", name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("could not commit: %w", err)
	}
	return nil
}

// createTable is the DDL for the table, written so that an existing one is left
// alone.
func createTable(name string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	cnae     int NOT NULL,
	ocupacao text NOT NULL,
	letra    char(1) NOT NULL,
	PRIMARY KEY (cnae, ocupacao)
)`, name)
}

// tableName validates the table name given by the caller. It is not a value in
// a query, so it cannot be a parameter, and it is checked instead of quoted.
func tableName(table string) (string, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return DefaultTable, nil
	}
	if !identifier.MatchString(table) {
		return "", fmt.Errorf("invalid table name %q: use lowercase letters, digits and underscores, optionally as schema.table", table)
	}
	return table, nil
}

// identifierOf splits a validated name into the form CopyFrom expects.
func identifierOf(name string) pgx.Identifier {
	return pgx.Identifier(strings.Split(name, "."))
}
