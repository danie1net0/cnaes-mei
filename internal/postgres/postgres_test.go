package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"gitlab.com/danie1net0/cnaes-mei/internal/mei"
)

// uriEnv names the environment variable holding the URI of the database the
// tests write to. Without it the tests that need PostgreSQL are skipped, so
// that `go test ./...` works on a machine with no server.
const uriEnv = "TEST_POSTGRES_URL"

var sample = []mei.Activity{
	{CNAE: 111301, Occupation: "CULTIVO DE ARROZ", Letter: "C"},
	{CNAE: 4722901, Occupation: "AÇOUGUEIRO(A) INDEPENDENTE", Letter: "A"},
}

func TestTableName(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"empty is the default", "", DefaultTable},
		{"blanks are the default", "   ", DefaultTable},
		{"plain name", "cnae_mei", "cnae_mei"},
		{"underscore first", "_scratch", "_scratch"},
		{"with digits", "cnae_mei_2026", "cnae_mei_2026"},
		{"qualified by a schema", "mei.cnae", "mei.cnae"},
		{"surrounded by blanks", " cnae_mei ", "cnae_mei"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tableName(tc.in)
			if err != nil {
				t.Fatalf("tableName(%q) returned an error: %s", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("tableName(%q) got %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTableNameRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"quote", `cnae"; DROP TABLE empresa; --`},
		{"space", "cnae mei"},
		{"uppercase", "CnaeMei"},
		{"starts with a digit", "2026_cnae"},
		{"three parts", "db.mei.cnae"},
		{"trailing dot", "mei."},
		{"hyphen", "cnae-mei"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tableName(tc.in)

			if err == nil {
				t.Fatalf("tableName(%q) got %q, want an error", tc.in, got)
			}
			if !strings.Contains(err.Error(), "invalid table name") {
				t.Errorf("error is %q, want it to say the name is invalid", err)
			}
		})
	}
}

func TestIdentifierOf(t *testing.T) {
	if got, want := identifierOf("cnae_mei").Sanitize(), `"cnae_mei"`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got, want := identifierOf("mei.cnae").Sanitize(), `"mei"."cnae"`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestConnectWithAnInvalidURI(t *testing.T) {
	_, err := Connect(t.Context(), "not a uri")

	if err == nil {
		t.Fatal("got no error, want the invalid URI")
	}
	if !strings.Contains(err.Error(), "could not connect") {
		t.Errorf("error is %q, want it to report the connection", err)
	}
}

func TestReplace(t *testing.T) {
	db, table := setUp(t)

	if err := db.Replace(t.Context(), table, sample); err != nil {
		t.Fatalf("Replace returned an error: %s", err)
	}

	got := read(t, db, table)
	want := []mei.Activity{sample[0], sample[1]} // read back ordered by CNAE
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReplaceRunsTwiceWithoutPilingUp(t *testing.T) {
	db, table := setUp(t)
	ctx := t.Context()

	if err := db.Replace(ctx, table, sample); err != nil {
		t.Fatalf("the first Replace returned an error: %s", err)
	}
	if err := db.Replace(ctx, table, sample[:1]); err != nil {
		t.Fatalf("the second Replace returned an error: %s", err)
	}

	got := read(t, db, table)
	if len(got) != 1 || got[0] != sample[0] {
		t.Errorf("got %+v, want only %+v", got, sample[:1])
	}
}

func TestReplaceWithNothing(t *testing.T) {
	db, table := setUp(t)

	if err := db.Replace(t.Context(), table, nil); err != nil {
		t.Fatalf("Replace returned an error: %s", err)
	}

	if got := read(t, db, table); len(got) != 0 {
		t.Errorf("got %+v, want an empty table", got)
	}
}

func TestReplaceWithAnInvalidTableName(t *testing.T) {
	db, _ := setUp(t)

	err := db.Replace(t.Context(), "cnae mei", sample)

	if err == nil {
		t.Fatal("got no error, want the invalid name")
	}
	if !strings.Contains(err.Error(), "invalid table name") {
		t.Errorf("error is %q, want it to say the name is invalid", err)
	}
}

func TestReplaceWhenTheTableDoesNotFitTheData(t *testing.T) {
	db, table := setUp(t)
	ctx := t.Context()

	// A table of the right name but the wrong shape is what an older version
	// of this tool, or a hand-made table, leaves behind.
	if _, err := db.conn.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (outra_coluna int)", table)); err != nil {
		t.Fatalf("could not create the conflicting table: %s", err)
	}

	err := db.Replace(ctx, table, sample)

	if err == nil {
		t.Fatal("got no error, want the mismatch")
	}
	if !strings.Contains(err.Error(), "could not write to the table") {
		t.Errorf("error is %q, want it to report the write", err)
	}
	if rows := countRows(t, db, table); rows != 0 {
		t.Errorf("the table has %d rows, want the transaction to have been rolled back", rows)
	}
}

func TestReplaceOnAClosedConnection(t *testing.T) {
	db, table := setUp(t)
	if err := db.Close(t.Context()); err != nil {
		t.Fatalf("Close returned an error: %s", err)
	}

	err := db.Replace(t.Context(), table, sample)

	if err == nil {
		t.Fatal("got no error, want the closed connection")
	}
	if !strings.Contains(err.Error(), "could not start the transaction") {
		t.Errorf("error is %q, want it to report the transaction", err)
	}
}

// setUp connects to the test database and returns a table name of its own,
// dropped at the end of the test.
func setUp(t *testing.T) (*DB, string) {
	t.Helper()

	uri := os.Getenv(uriEnv)
	if uri == "" {
		t.Skipf("%s is not set, skipping the tests that need PostgreSQL", uriEnv)
	}

	db, err := Connect(t.Context(), uri)
	if err != nil {
		t.Fatalf("could not connect: %s", err)
	}

	table := testTable(t)
	if _, err := db.conn.Exec(t.Context(), "DROP TABLE IF EXISTS "+table); err != nil {
		t.Fatalf("could not clean up %s before the test: %s", table, err)
	}

	t.Cleanup(func() {
		// The test may have closed the connection on purpose, so the cleanup
		// opens one of its own and never fails the test over the connection.
		ctx := context.WithoutCancel(t.Context())
		db.Close(ctx) //nolint:errcheck // it may already be closed

		cleaner, err := Connect(ctx, uri)
		if err != nil {
			t.Errorf("could not connect to drop %s: %s", table, err)
			return
		}
		defer cleaner.Close(ctx) //nolint:errcheck // nothing left to do with it

		if _, err := cleaner.conn.Exec(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Errorf("could not drop %s: %s", table, err)
		}
	})

	return db, table
}

// testTable is a table name of this test's own, so that tests running one
// after another never collide.
func testTable(t *testing.T) string {
	t.Helper()

	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return '_'
	}, t.Name())
	return fmt.Sprintf("cnae_mei_%s_%d", name, os.Getpid())
}

// read returns the rows of the table, ordered by CNAE.
func read(t *testing.T, db *DB, table string) []mei.Activity {
	t.Helper()

	rows, err := db.conn.Query(t.Context(), "SELECT cnae, ocupacao, letra FROM "+table+" ORDER BY cnae")
	if err != nil {
		t.Fatalf("could not read %s: %s", table, err)
	}
	activities, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (mei.Activity, error) {
		var a mei.Activity
		err := row.Scan(&a.CNAE, &a.Occupation, &a.Letter)
		return a, err
	})
	if err != nil {
		t.Fatalf("could not read the rows of %s: %s", table, err)
	}
	return activities
}

// countRows returns how many rows the table has, or zero when it does not exist.
func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()

	var rows int
	if err := db.conn.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
		t.Fatalf("could not count the rows of %s: %s", table, err)
	}
	return rows
}

func TestReplaceWhenTheTableCannotBeCreated(t *testing.T) {
	db, _ := setUp(t)

	err := db.Replace(t.Context(), "esquema_que_nao_existe.cnae_mei", sample)

	if err == nil {
		t.Fatal("got no error, want the missing schema")
	}
	if !strings.Contains(err.Error(), "could not create the table") {
		t.Errorf("error is %q, want it to report the creation", err)
	}
}

func TestReplaceWhenTheNameIsNotATable(t *testing.T) {
	db, table := setUp(t)
	ctx := t.Context()

	if _, err := db.conn.Exec(ctx, fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS cnae", table)); err != nil {
		t.Fatalf("could not create the view: %s", err)
	}
	t.Cleanup(func() {
		if _, err := db.conn.Exec(context.WithoutCancel(ctx), "DROP VIEW IF EXISTS "+table); err != nil {
			t.Errorf("could not drop the view: %s", err)
		}
	})

	err := db.Replace(ctx, table, sample)

	if err == nil {
		t.Fatal("got no error, want the view in the way")
	}
	if !strings.Contains(err.Error(), "could not empty the table") {
		t.Errorf("error is %q, want it to report emptying the table", err)
	}
}

func TestReplaceWhenTheCommitFails(t *testing.T) {
	db, table := setUp(t)
	ctx := t.Context()

	// A deferred primary key is only checked at COMMIT, which is how a
	// failure at that last step can be provoked on purpose.
	create := fmt.Sprintf(`CREATE TABLE %s (
		cnae int NOT NULL,
		ocupacao text NOT NULL,
		letra char(1) NOT NULL,
		PRIMARY KEY (cnae, ocupacao) DEFERRABLE INITIALLY DEFERRED
	)`, table)
	if _, err := db.conn.Exec(ctx, create); err != nil {
		t.Fatalf("could not create the table: %s", err)
	}

	duplicated := []mei.Activity{sample[0], sample[0]}
	err := db.Replace(ctx, table, duplicated)

	if err == nil {
		t.Fatal("got no error, want the failed commit")
	}
	if !strings.Contains(err.Error(), "could not commit") {
		t.Errorf("error is %q, want it to report the commit", err)
	}
}
