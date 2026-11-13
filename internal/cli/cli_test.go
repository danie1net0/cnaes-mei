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
func listing(test *testing.T) *httptest.Server {
	test.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, reader *http.Request) {
		if reader.URL.Path != "/a" {
			http.NotFound(writer, reader)
			return
		}
		fmt.Fprint(writer, page)
	}))
	test.Cleanup(server.Close)
	return server
}

// run executes the command and returns what it wrote to standard output.
func run(test *testing.T, args ...string) (string, string, error) {
	test.Helper()

	var stdout, stderr strings.Builder
	err := Run(test.Context(), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestRunWritesCSVToStandardOutput(test *testing.T) {
	server := listing(test)

	stdout, _, err := run(test, "-base-url", server.URL, "-min", "1")
	if err != nil {
		test.Fatalf("Run returned an error: %s", err)
	}

	want := "cnae,ocupacao,letra\n4722901,AÇOUGUEIRO(A) INDEPENDENTE,A\n"
	if stdout != want {
		test.Errorf("got:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestRunWritesJSON(test *testing.T) {
	server := listing(test)

	stdout, _, err := run(test, "-base-url", server.URL, "-min", "1", "-format", "json")
	if err != nil {
		test.Fatalf("Run returned an error: %s", err)
	}

	var activities []struct {
		CNAE       string `json:"cnae"`
		Occupation string `json:"ocupacao"`
		Letter     string `json:"letra"`
	}
	if err := json.Unmarshal([]byte(stdout), &activities); err != nil {
		test.Fatalf("the output is not JSON: %s\n%s", err, stdout)
	}
	if len(activities) != 1 || activities[0].CNAE != "4722901" {
		test.Errorf("got %+v, want one activity with the CNAE 4722901", activities)
	}
}

func TestRunWritesToAFile(test *testing.T) {
	server := listing(test)
	path := filepath.Join(test.TempDir(), "cnaes.tsv")

	stdout, _, err := run(test, "-base-url", server.URL, "-min", "1", "-format", "tsv", "-output", path)
	if err != nil {
		test.Fatalf("Run returned an error: %s", err)
	}
	if stdout != "" {
		test.Errorf("standard output got %q, want nothing when writing to a file", stdout)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		test.Fatalf("could not read what was written: %s", err)
	}
	want := "cnae\tocupacao\tletra\n4722901\tAÇOUGUEIRO(A) INDEPENDENTE\tA\n"
	if string(written) != want {
		test.Errorf("the file has:\n%s\nwant:\n%s", written, want)
	}
}

func TestRunWhenTheFileCannotBeCreated(test *testing.T) {
	server := listing(test)
	path := filepath.Join(test.TempDir(), "nao", "existe", "cnaes.csv")

	_, _, err := run(test, "-base-url", server.URL, "-min", "1", "-output", path)

	if err == nil {
		test.Fatal("got no error, want the missing directory")
	}
	if !strings.Contains(err.Error(), "could not create") {
		test.Errorf("error is %q, want it to report the file", err)
	}
}

func TestRunReadsTheBaseURLFromTheEnvironment(test *testing.T) {
	server := listing(test)
	test.Setenv("MEI_BASE_URL", server.URL)

	stdout, _, err := run(test, "-min", "1")
	if err != nil {
		test.Fatalf("Run returned an error: %s", err)
	}
	if !strings.Contains(stdout, "4722901") {
		test.Errorf("got:\n%s\nwant the activity of the test server", stdout)
	}
}

func TestRunFailsWhenTheHarvestIsTooSmall(test *testing.T) {
	server := listing(test)

	_, _, err := run(test, "-base-url", server.URL, "-min", "10")

	if !errors.Is(err, mei.ErrTooFewActivities) {
		test.Fatalf("got error %v, want %v", err, mei.ErrTooFewActivities)
	}
}

func TestRunLogsEachLetterWhenVerbose(test *testing.T) {
	server := listing(test)

	_, stderr, err := run(test, "-base-url", server.URL, "-min", "1", "-verbose")
	if err != nil {
		test.Fatalf("Run returned an error: %s", err)
	}
	if !strings.Contains(stderr, "letter read") {
		test.Errorf("the log is %q, want it to mention each letter", stderr)
	}
}

func TestRunWithAnUnknownFormat(test *testing.T) {
	_, _, err := run(test, "-format", "xml")

	if err == nil {
		test.Fatal("got no error, want the unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		test.Errorf("error is %q, want it to report the format", err)
	}
}

func TestRunWithAnUnknownFlag(test *testing.T) {
	_, stderr, err := run(test, "-nao-existe")

	if err == nil {
		test.Fatal("got no error, want the unknown flag")
	}
	if !strings.Contains(stderr, "Usage") {
		test.Errorf("the output is %q, want the usage text", stderr)
	}
}

func TestRunWithAnUnexpectedArgument(test *testing.T) {
	_, _, err := run(test, "atividades")

	if err == nil {
		test.Fatal("got no error, want the unexpected argument")
	}
	if !strings.Contains(err.Error(), `unexpected argument "atividades"`) {
		test.Errorf("error is %q, want it to name the argument", err)
	}
}

func TestRunHelp(test *testing.T) {
	_, stderr, err := run(test, "-h")

	if !IsUsageError(err) {
		test.Fatalf("got error %v, want the help", err)
	}
	for _, want := range []string{"cnaes-mei collects", "-database-url", "-format"} {
		if !strings.Contains(stderr, want) {
			test.Errorf("the usage text does not mention %q:\n%s", want, stderr)
		}
	}
}

func TestRunVersion(test *testing.T) {
	stdout, _, err := run(test, "-version")

	if err != nil {
		test.Fatalf("Run returned an error: %s", err)
	}
	if strings.TrimSpace(stdout) == "" {
		test.Error("got nothing, want a version")
	}
}

func TestRunWhenTheListingFails(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "no", http.StatusForbidden)
	}))
	defer server.Close()

	_, _, err := run(test, "-base-url", server.URL, "-parallel", "1")

	if err == nil {
		test.Fatal("got no error, want the rejection by the server")
	}
	if !strings.Contains(err.Error(), "403") {
		test.Errorf("error is %q, want it to report the status", err)
	}
}

func TestRunStoresInPostgres(test *testing.T) {
	uri := os.Getenv("TEST_POSTGRES_URL")
	if uri == "" {
		test.Skip("TEST_POSTGRES_URL is not set, skipping the test that needs PostgreSQL")
	}

	server := listing(test)
	table := fmt.Sprintf("cnae_mei_cli_%d", os.Getpid())
	test.Cleanup(func() { dropTable(test, uri, table) })

	stdout, stderr, err := run(test, "-base-url", server.URL, "-min", "1", "-database-url", uri, "-table", table)
	if err != nil {
		test.Fatalf("Run returned an error: %s", err)
	}
	if stdout != "" {
		test.Errorf("standard output got %q, want nothing when storing in the database", stdout)
	}
	if !strings.Contains(stderr, "activities stored") {
		test.Errorf("the log is %q, want it to report the storing", stderr)
	}
	if got := countRows(test, uri, table); got != 1 {
		test.Errorf("the table has %d rows, want 1", got)
	}
}

func TestRunStoresAndWrites(test *testing.T) {
	uri := os.Getenv("TEST_POSTGRES_URL")
	if uri == "" {
		test.Skip("TEST_POSTGRES_URL is not set, skipping the test that needs PostgreSQL")
	}

	server := listing(test)
	table := fmt.Sprintf("cnae_mei_cli_both_%d", os.Getpid())
	test.Cleanup(func() { dropTable(test, uri, table) })

	stdout, _, err := run(test, "-base-url", server.URL, "-min", "1", "-database-url", uri, "-table", table, "-output", "-")
	if err != nil {
		test.Fatalf("Run returned an error: %s", err)
	}
	if !strings.Contains(stdout, "4722901") {
		test.Errorf("standard output got %q, want the activities as well", stdout)
	}
	if got := countRows(test, uri, table); got != 1 {
		test.Errorf("the table has %d rows, want 1", got)
	}
}

func TestRunWithAnUnreachableDatabase(test *testing.T) {
	server := listing(test)

	_, _, err := run(test, "-base-url", server.URL, "-min", "1", "-database-url", "postgres://nobody@127.0.0.1:1/nothing?sslmode=disable&connect_timeout=1")

	if err == nil {
		test.Fatal("got no error, want the unreachable database")
	}
	if !strings.Contains(err.Error(), "could not connect") {
		test.Errorf("error is %q, want it to report the connection", err)
	}
}

func TestRunWithAnInvalidTableName(test *testing.T) {
	uri := os.Getenv("TEST_POSTGRES_URL")
	if uri == "" {
		test.Skip("TEST_POSTGRES_URL is not set, skipping the test that needs PostgreSQL")
	}

	server := listing(test)

	_, _, err := run(test, "-base-url", server.URL, "-min", "1", "-database-url", uri, "-table", "cnae mei")

	if err == nil {
		test.Fatal("got no error, want the invalid table name")
	}
	if !strings.Contains(err.Error(), "invalid table name") {
		test.Errorf("error is %q, want it to say the name is invalid", err)
	}
}

func TestIsUsageError(test *testing.T) {
	if !IsUsageError(fmt.Errorf("wrapped: %w", flag.ErrHelp)) {
		test.Error("a wrapped flag.ErrHelp is a usage error")
	}
	if IsUsageError(errors.New("anything else")) {
		test.Error("any other error is not a usage error")
	}
}

// dropTable removes a table left by a test.
func dropTable(test *testing.T, uri, table string) {
	test.Helper()
	exec(test, uri, "DROP TABLE IF EXISTS "+table)
}

// countRows returns how many rows a table has.
func countRows(test *testing.T, uri, table string) int {
	test.Helper()

	conn := connect(test, uri)
	defer conn.Close(context.Background()) //nolint:errcheck // nothing left to do with it

	var rows int
	if err := conn.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
		test.Fatalf("could not count the rows of %s: %s", table, err)
	}
	return rows
}

func exec(test *testing.T, uri, statement string) {
	test.Helper()

	conn := connect(test, uri)
	defer conn.Close(context.Background()) //nolint:errcheck // nothing left to do with it

	if _, err := conn.Exec(context.Background(), statement); err != nil {
		test.Errorf("could not run %q: %s", statement, err)
	}
}

// connect opens a connection for the test helpers.
func connect(test *testing.T, uri string) *pgx.Conn {
	test.Helper()

	conn, err := pgx.Connect(context.Background(), uri)
	if err != nil {
		test.Fatalf("could not connect: %s", err)
	}
	return conn
}

func TestMainSucceeds(test *testing.T) {
	server := listing(test)
	var stdout, stderr strings.Builder

	code := Main([]string{"-base-url", server.URL, "-min", "1"}, &stdout, &stderr)

	if code != 0 {
		test.Errorf("got exit code %d, want 0: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "4722901") {
		test.Errorf("got %q, want the activities", stdout.String())
	}
}

func TestMainReportsTheError(test *testing.T) {
	var stdout, stderr strings.Builder

	code := Main([]string{"-format", "xml"}, &stdout, &stderr)

	if code != 1 {
		test.Errorf("got exit code %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "cnaes-mei: unknown format") {
		test.Errorf("got %q, want the error on standard error", stderr.String())
	}
}

func TestMainWithTheHelp(test *testing.T) {
	var stdout, stderr strings.Builder

	code := Main([]string{"-h"}, &stdout, &stderr)

	if code != 1 {
		test.Errorf("got exit code %d, want 1", code)
	}
	if strings.Contains(stderr.String(), "cnaes-mei: flag") {
		test.Errorf("got %q, want the usage text without an error line", stderr.String())
	}
}
