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

func TestTableName(test *testing.T) {
	for _, testCase := range []struct {
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
		test.Run(testCase.name, func(test *testing.T) {
			got, err := tableName(testCase.in)
			if err != nil {
				test.Fatalf("tableName(%q) returned an error: %s", testCase.in, err)
			}
			if got != testCase.want {
				test.Errorf("tableName(%q) got %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestTableNameRejected(test *testing.T) {
	for _, testCase := range []struct {
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
		test.Run(testCase.name, func(test *testing.T) {
			got, err := tableName(testCase.in)

			if err == nil {
				test.Fatalf("tableName(%q) got %q, want an error", testCase.in, got)
			}
			if !strings.Contains(err.Error(), "invalid table name") {
				test.Errorf("error is %q, want it to say the name is invalid", err)
			}
		})
	}
}

func TestIdentifierOf(test *testing.T) {
	if got, want := identifierOf("cnae_mei").Sanitize(), `"cnae_mei"`; got != want {
		test.Errorf("got %s, want %s", got, want)
	}
	if got, want := identifierOf("mei.cnae").Sanitize(), `"mei"."cnae"`; got != want {
		test.Errorf("got %s, want %s", got, want)
	}
}

func TestConnectWithAnInvalidURI(test *testing.T) {
	_, err := Connect(test.Context(), "not a uri")

	if err == nil {
		test.Fatal("got no error, want the invalid URI")
	}
	if !strings.Contains(err.Error(), "could not connect") {
		test.Errorf("error is %q, want it to report the connection", err)
	}
}

func TestReplace(test *testing.T) {
	database, table := setUp(test)

	if err := database.Replace(test.Context(), table, sample); err != nil {
		test.Fatalf("Replace returned an error: %s", err)
	}

	got := read(test, database, table)
	want := []mei.Activity{sample[0], sample[1]} // read back ordered by CNAE
	if len(got) != len(want) {
		test.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			test.Errorf("row %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReplaceRunsTwiceWithoutPilingUp(test *testing.T) {
	database, table := setUp(test)
	ctx := test.Context()

	if err := database.Replace(ctx, table, sample); err != nil {
		test.Fatalf("the first Replace returned an error: %s", err)
	}
	if err := database.Replace(ctx, table, sample[:1]); err != nil {
		test.Fatalf("the second Replace returned an error: %s", err)
	}

	got := read(test, database, table)
	if len(got) != 1 || got[0] != sample[0] {
		test.Errorf("got %+v, want only %+v", got, sample[:1])
	}
}

func TestReplaceWithNothing(test *testing.T) {
	database, table := setUp(test)

	if err := database.Replace(test.Context(), table, nil); err != nil {
		test.Fatalf("Replace returned an error: %s", err)
	}

	if got := read(test, database, table); len(got) != 0 {
		test.Errorf("got %+v, want an empty table", got)
	}
}

func TestReplaceWithAnInvalidTableName(test *testing.T) {
	database, _ := setUp(test)

	err := database.Replace(test.Context(), "cnae mei", sample)

	if err == nil {
		test.Fatal("got no error, want the invalid name")
	}
	if !strings.Contains(err.Error(), "invalid table name") {
		test.Errorf("error is %q, want it to say the name is invalid", err)
	}
}

func TestReplaceWhenTheTableDoesNotFitTheData(test *testing.T) {
	database, table := setUp(test)
	ctx := test.Context()

	// A table of the right name but the wrong shape is what an older version
	// of this tool, or a hand-made table, leaves behind.
	if _, err := database.connection.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (outra_coluna int)", table)); err != nil {
		test.Fatalf("could not create the conflicting table: %s", err)
	}

	err := database.Replace(ctx, table, sample)

	if err == nil {
		test.Fatal("got no error, want the mismatch")
	}
	if !strings.Contains(err.Error(), "could not write to the table") {
		test.Errorf("error is %q, want it to report the write", err)
	}
	if rows := countRows(test, database, table); rows != 0 {
		test.Errorf("the table has %d rows, want the transaction to have been rolled back", rows)
	}
}

func TestReplaceOnAClosedConnection(test *testing.T) {
	database, table := setUp(test)
	if err := database.Close(test.Context()); err != nil {
		test.Fatalf("Close returned an error: %s", err)
	}

	err := database.Replace(test.Context(), table, sample)

	if err == nil {
		test.Fatal("got no error, want the closed connection")
	}
	if !strings.Contains(err.Error(), "could not start the transaction") {
		test.Errorf("error is %q, want it to report the transaction", err)
	}
}

// setUp connects to the test database and returns a table name of its own,
// dropped at the end of the test.
func setUp(test *testing.T) (*DB, string) {
	test.Helper()

	uri := os.Getenv(uriEnv)
	if uri == "" {
		test.Skipf("%s is not set, skipping the tests that need PostgreSQL", uriEnv)
	}

	database, err := Connect(test.Context(), uri)
	if err != nil {
		test.Fatalf("could not connect: %s", err)
	}

	table := testTable(test)
	if _, err := database.connection.Exec(test.Context(), "DROP TABLE IF EXISTS "+table); err != nil {
		test.Fatalf("could not clean up %s before the test: %s", table, err)
	}

	test.Cleanup(func() {
		// The test may have closed the connection on purpose, so the cleanup
		// opens one of its own and never fails the test over the connection.
		ctx := context.WithoutCancel(test.Context())
		database.Close(ctx) //nolint:errcheck // it may already be closed

		cleaner, err := Connect(ctx, uri)
		if err != nil {
			test.Errorf("could not connect to drop %s: %s", table, err)
			return
		}
		defer cleaner.Close(ctx) //nolint:errcheck // nothing left to do with it

		if _, err := cleaner.connection.Exec(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			test.Errorf("could not drop %s: %s", table, err)
		}
	})

	return database, table
}

// testTable is a table name of this test's own, so that tests running one
// after another never collide.
func testTable(test *testing.T) string {
	test.Helper()

	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return '_'
	}, test.Name())
	return fmt.Sprintf("cnae_mei_%s_%d", name, os.Getpid())
}

// read returns the rows of the table, ordered by CNAE.
func read(test *testing.T, database *DB, table string) []mei.Activity {
	test.Helper()

	rows, err := database.connection.Query(test.Context(), "SELECT cnae, ocupacao, letra FROM "+table+" ORDER BY cnae")
	if err != nil {
		test.Fatalf("could not read %s: %s", table, err)
	}
	activities, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (mei.Activity, error) {
		var a mei.Activity
		err := row.Scan(&a.CNAE, &a.Occupation, &a.Letter)
		return a, err
	})
	if err != nil {
		test.Fatalf("could not read the rows of %s: %s", table, err)
	}
	return activities
}

// countRows returns how many rows the table has, or zero when it does not exist.
func countRows(test *testing.T, database *DB, table string) int {
	test.Helper()

	var rows int
	if err := database.connection.QueryRow(test.Context(), "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
		test.Fatalf("could not count the rows of %s: %s", table, err)
	}
	return rows
}

func TestReplaceWhenTheTableCannotBeCreated(test *testing.T) {
	database, _ := setUp(test)

	err := database.Replace(test.Context(), "esquema_que_nao_existe.cnae_mei", sample)

	if err == nil {
		test.Fatal("got no error, want the missing schema")
	}
	if !strings.Contains(err.Error(), "could not create the table") {
		test.Errorf("error is %q, want it to report the creation", err)
	}
}

func TestReplaceWhenTheNameIsNotATable(test *testing.T) {
	database, table := setUp(test)
	ctx := test.Context()

	if _, err := database.connection.Exec(ctx, fmt.Sprintf("CREATE VIEW %s AS SELECT 1 AS cnae", table)); err != nil {
		test.Fatalf("could not create the view: %s", err)
	}
	test.Cleanup(func() {
		if _, err := database.connection.Exec(context.WithoutCancel(ctx), "DROP VIEW IF EXISTS "+table); err != nil {
			test.Errorf("could not drop the view: %s", err)
		}
	})

	err := database.Replace(ctx, table, sample)

	if err == nil {
		test.Fatal("got no error, want the view in the way")
	}
	if !strings.Contains(err.Error(), "could not empty the table") {
		test.Errorf("error is %q, want it to report emptying the table", err)
	}
}

func TestReplaceWhenTheCommitFails(test *testing.T) {
	database, table := setUp(test)
	ctx := test.Context()

	// A deferred primary key is only checked at COMMIT, which is how a
	// failure at that last step can be provoked on purpose.
	create := fmt.Sprintf(`CREATE TABLE %s (
		cnae int NOT NULL,
		ocupacao text NOT NULL,
		letra char(1) NOT NULL,
		PRIMARY KEY (cnae, ocupacao) DEFERRABLE INITIALLY DEFERRED
	)`, table)
	if _, err := database.connection.Exec(ctx, create); err != nil {
		test.Fatalf("could not create the table: %s", err)
	}

	duplicated := []mei.Activity{sample[0], sample[0]}
	err := database.Replace(ctx, table, duplicated)

	if err == nil {
		test.Fatal("got no error, want the failed commit")
	}
	if !strings.Contains(err.Error(), "could not commit") {
		test.Errorf("error is %q, want it to report the commit", err)
	}
}
