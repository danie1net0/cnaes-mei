package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"gitlab.com/danie1net0/cnaes-mei/internal/mei"
)

// page is a letter page with a single occupation, enough to exercise the
// command without reaching the government's site.
const page = `<table>
	<tr><th>OCUPAÇÃO</th><th>CNAE</th></tr>
	<tr><td>AÇOUGUEIRO(A) INDEPENDENTE</td><td>4722-9/01</td></tr>
</table>`

// listing serves that page for the letter A and 404 for every other letter.
func listing(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, page)
	}))
	t.Cleanup(server.Close)
	return server
}

// run executes the command and returns what it wrote to standard output.
func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr strings.Builder
	err := Run(t.Context(), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestRunWritesCSVToStandardOutput(t *testing.T) {
	server := listing(t)

	stdout, _, err := run(t, "-base-url", server.URL, "-min", "1")
	if err != nil {
		t.Fatalf("Run returned an error: %s", err)
	}

	want := "cnae,ocupacao,letra\n4722901,AÇOUGUEIRO(A) INDEPENDENTE,A\n"
	if stdout != want {
		t.Errorf("got:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestRunWritesJSON(t *testing.T) {
	server := listing(t)

	stdout, _, err := run(t, "-base-url", server.URL, "-min", "1", "-format", "json")
	if err != nil {
		t.Fatalf("Run returned an error: %s", err)
	}

	var activities []struct {
		CNAE       string `json:"cnae"`
		Occupation string `json:"ocupacao"`
		Letter     string `json:"letra"`
	}
	if err := json.Unmarshal([]byte(stdout), &activities); err != nil {
		t.Fatalf("the output is not JSON: %s\n%s", err, stdout)
	}
	if len(activities) != 1 || activities[0].CNAE != "4722901" {
		t.Errorf("got %+v, want one activity with the CNAE 4722901", activities)
	}
}

func TestRunWritesToAFile(t *testing.T) {
	server := listing(t)
	path := filepath.Join(t.TempDir(), "cnaes.tsv")

	stdout, _, err := run(t, "-base-url", server.URL, "-min", "1", "-format", "tsv", "-output", path)
	if err != nil {
		t.Fatalf("Run returned an error: %s", err)
	}
	if stdout != "" {
		t.Errorf("standard output got %q, want nothing when writing to a file", stdout)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read what was written: %s", err)
	}
	want := "cnae\tocupacao\tletra\n4722901\tAÇOUGUEIRO(A) INDEPENDENTE\tA\n"
	if string(written) != want {
		t.Errorf("the file has:\n%s\nwant:\n%s", written, want)
	}
}

func TestRunWhenTheFileCannotBeCreated(t *testing.T) {
	server := listing(t)
	path := filepath.Join(t.TempDir(), "nao", "existe", "cnaes.csv")

	_, _, err := run(t, "-base-url", server.URL, "-min", "1", "-output", path)

	if err == nil {
		t.Fatal("got no error, want the missing directory")
	}
	if !strings.Contains(err.Error(), "could not create") {
		t.Errorf("error is %q, want it to report the file", err)
	}
}

func TestRunReadsTheBaseURLFromTheEnvironment(t *testing.T) {
	server := listing(t)
	t.Setenv("MEI_BASE_URL", server.URL)

	stdout, _, err := run(t, "-min", "1")
	if err != nil {
		t.Fatalf("Run returned an error: %s", err)
	}
	if !strings.Contains(stdout, "4722901") {
		t.Errorf("got:\n%s\nwant the activity of the test server", stdout)
	}
}

func TestRunFailsWhenTheHarvestIsTooSmall(t *testing.T) {
	server := listing(t)

	_, _, err := run(t, "-base-url", server.URL, "-min", "10")

	if !errors.Is(err, mei.ErrTooFewActivities) {
		t.Fatalf("got error %v, want %v", err, mei.ErrTooFewActivities)
	}
}

func TestRunLogsEachLetterWhenVerbose(t *testing.T) {
	server := listing(t)

	_, stderr, err := run(t, "-base-url", server.URL, "-min", "1", "-verbose")
	if err != nil {
		t.Fatalf("Run returned an error: %s", err)
	}
	if !strings.Contains(stderr, "letter read") {
		t.Errorf("the log is %q, want it to mention each letter", stderr)
	}
}

func TestRunWithAnUnknownFormat(t *testing.T) {
	_, _, err := run(t, "-format", "xml")

	if err == nil {
		t.Fatal("got no error, want the unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error is %q, want it to report the format", err)
	}
}

func TestRunWithAnUnknownFlag(t *testing.T) {
	_, stderr, err := run(t, "-nao-existe")

	if err == nil {
		t.Fatal("got no error, want the unknown flag")
	}
	if !strings.Contains(stderr, "Usage") {
		t.Errorf("the output is %q, want the usage text", stderr)
	}
}

func TestRunWithAnUnexpectedArgument(t *testing.T) {
	_, _, err := run(t, "atividades")

	if err == nil {
		t.Fatal("got no error, want the unexpected argument")
	}
	if !strings.Contains(err.Error(), `unexpected argument "atividades"`) {
		t.Errorf("error is %q, want it to name the argument", err)
	}
}

func TestRunHelp(t *testing.T) {
	_, stderr, err := run(t, "-h")

	if !IsUsageError(err) {
		t.Fatalf("got error %v, want the help", err)
	}
	for _, want := range []string{"cnaes-mei collects", "-database-url", "-format"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the usage text does not mention %q:\n%s", want, stderr)
		}
	}
}

func TestRunVersion(t *testing.T) {
	stdout, _, err := run(t, "-version")

	if err != nil {
		t.Fatalf("Run returned an error: %s", err)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Error("got nothing, want a version")
	}
}

func TestRunWhenTheListingFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer server.Close()

	_, _, err := run(t, "-base-url", server.URL, "-parallel", "1")

	if err == nil {
		t.Fatal("got no error, want the rejection by the server")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error is %q, want it to report the status", err)
	}
}

func TestRunStoresInPostgres(t *testing.T) {
	uri := os.Getenv("TEST_POSTGRES_URL")
	if uri == "" {
		t.Skip("TEST_POSTGRES_URL is not set, skipping the test that needs PostgreSQL")
	}

	server := listing(t)
	table := fmt.Sprintf("cnae_mei_cli_%d", os.Getpid())
	t.Cleanup(func() { dropTable(t, uri, table) })

	stdout, stderr, err := run(t, "-base-url", server.URL, "-min", "1", "-database-url", uri, "-table", table)
	if err != nil {
		t.Fatalf("Run returned an error: %s", err)
	}
	if stdout != "" {
		t.Errorf("standard output got %q, want nothing when storing in the database", stdout)
	}
	if !strings.Contains(stderr, "activities stored") {
		t.Errorf("the log is %q, want it to report the storing", stderr)
	}
	if got := countRows(t, uri, table); got != 1 {
		t.Errorf("the table has %d rows, want 1", got)
	}
}

func TestRunStoresAndWrites(t *testing.T) {
	uri := os.Getenv("TEST_POSTGRES_URL")
	if uri == "" {
		t.Skip("TEST_POSTGRES_URL is not set, skipping the test that needs PostgreSQL")
	}

	server := listing(t)
	table := fmt.Sprintf("cnae_mei_cli_both_%d", os.Getpid())
	t.Cleanup(func() { dropTable(t, uri, table) })

	stdout, _, err := run(t, "-base-url", server.URL, "-min", "1", "-database-url", uri, "-table", table, "-output", "-")
	if err != nil {
		t.Fatalf("Run returned an error: %s", err)
	}
	if !strings.Contains(stdout, "4722901") {
		t.Errorf("standard output got %q, want the activities as well", stdout)
	}
	if got := countRows(t, uri, table); got != 1 {
		t.Errorf("the table has %d rows, want 1", got)
	}
}

func TestRunWithAnUnreachableDatabase(t *testing.T) {
	server := listing(t)

	_, _, err := run(t, "-base-url", server.URL, "-min", "1", "-database-url", "postgres://nobody@127.0.0.1:1/nothing?sslmode=disable&connect_timeout=1")

	if err == nil {
		t.Fatal("got no error, want the unreachable database")
	}
	if !strings.Contains(err.Error(), "could not connect") {
		t.Errorf("error is %q, want it to report the connection", err)
	}
}

func TestRunWithAnInvalidTableName(t *testing.T) {
	uri := os.Getenv("TEST_POSTGRES_URL")
	if uri == "" {
		t.Skip("TEST_POSTGRES_URL is not set, skipping the test that needs PostgreSQL")
	}

	server := listing(t)

	_, _, err := run(t, "-base-url", server.URL, "-min", "1", "-database-url", uri, "-table", "cnae mei")

	if err == nil {
		t.Fatal("got no error, want the invalid table name")
	}
	if !strings.Contains(err.Error(), "invalid table name") {
		t.Errorf("error is %q, want it to say the name is invalid", err)
	}
}

func TestIsUsageError(t *testing.T) {
	if !IsUsageError(fmt.Errorf("wrapped: %w", flag.ErrHelp)) {
		t.Error("a wrapped flag.ErrHelp is a usage error")
	}
	if IsUsageError(errors.New("anything else")) {
		t.Error("any other error is not a usage error")
	}
}

// dropTable removes a table left by a test.
func dropTable(t *testing.T, uri, table string) {
	t.Helper()
	exec(t, uri, "DROP TABLE IF EXISTS "+table)
}

// countRows returns how many rows a table has.
func countRows(t *testing.T, uri, table string) int {
	t.Helper()

	conn := connect(t, uri)
	defer conn.Close(context.Background()) //nolint:errcheck // nothing left to do with it

	var rows int
	if err := conn.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
		t.Fatalf("could not count the rows of %s: %s", table, err)
	}
	return rows
}

func exec(t *testing.T, uri, statement string) {
	t.Helper()

	conn := connect(t, uri)
	defer conn.Close(context.Background()) //nolint:errcheck // nothing left to do with it

	if _, err := conn.Exec(context.Background(), statement); err != nil {
		t.Errorf("could not run %q: %s", statement, err)
	}
}

// connect opens a connection for the test helpers.
func connect(t *testing.T, uri string) *pgx.Conn {
	t.Helper()

	conn, err := pgx.Connect(context.Background(), uri)
	if err != nil {
		t.Fatalf("could not connect: %s", err)
	}
	return conn
}

func TestMainSucceeds(t *testing.T) {
	server := listing(t)
	var stdout, stderr strings.Builder

	code := Main([]string{"-base-url", server.URL, "-min", "1"}, &stdout, &stderr)

	if code != 0 {
		t.Errorf("got exit code %d, want 0: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "4722901") {
		t.Errorf("got %q, want the activities", stdout.String())
	}
}

func TestMainReportsTheError(t *testing.T) {
	var stdout, stderr strings.Builder

	code := Main([]string{"-format", "xml"}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("got exit code %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "cnaes-mei: unknown format") {
		t.Errorf("got %q, want the error on standard error", stderr.String())
	}
}

func TestMainWithTheHelp(t *testing.T) {
	var stdout, stderr strings.Builder

	code := Main([]string{"-h"}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("got exit code %d, want 1", code)
	}
	if strings.Contains(stderr.String(), "cnaes-mei: flag") {
		t.Errorf("got %q, want the usage text without an error line", stderr.String())
	}
}
