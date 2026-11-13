package mei

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// pageWith builds a listing page with the given occupations.
func pageWith(rows ...string) string {
	var table strings.Builder
	table.WriteString("<table><tr><th>OCUPAÇÃO</th><th>CNAE</th></tr>")
	for _, row := range rows {
		table.WriteString(row)
	}
	table.WriteString("</table>")
	return table.String()
}

func row(occupation, cnae string) string {
	return fmt.Sprintf("<tr><td>%s</td><td>%s</td></tr>", occupation, cnae)
}

// listing serves pages by letter and answers 404 for every other letter, the
// way the government's site does.
func listing(test *testing.T, pages map[string]string) *httptest.Server {
	test.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, r *http.Request) {
		letter := strings.TrimPrefix(r.URL.Path, "/")
		page, ok := pages[letter]
		if !ok {
			http.NotFound(writer, r)
			return
		}
		fmt.Fprint(writer, page)
	}))
	test.Cleanup(server.Close)
	return server
}

func TestScrape(test *testing.T) {
	server := listing(test, map[string]string{
		"a": pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01")),
		"c": pageWith(
			row("CULTIVO DE ARROZ", "0111-3/01"),
			row("COSTUREIRO(A) INDEPENDENTE", "1412-6/01"),
		),
	})

	activities, err := NewScraper(WithBaseURL(server.URL)).Scrape(test.Context())
	if err != nil {
		test.Fatalf("Scrape returned an error: %s", err)
	}

	want := []Activity{
		{CNAE: 111301, Occupation: "CULTIVO DE ARROZ", Letter: "C"},
		{CNAE: 1412601, Occupation: "COSTUREIRO(A) INDEPENDENTE", Letter: "C"},
		{CNAE: 4722901, Occupation: "AÇOUGUEIRO(A) INDEPENDENTE", Letter: "A"},
	}
	if len(activities) != len(want) {
		test.Fatalf("got %d activities, want %d: %+v", len(activities), len(want), activities)
	}
	for index := range want {
		if activities[index] != want[index] {
			test.Errorf("activity %d is %+v, want %+v", index, activities[index], want[index])
		}
	}
}

func TestScrapeTrailingSlashInTheBaseURL(test *testing.T) {
	server := listing(test, map[string]string{"a": pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01"))})

	activities, err := NewScraper(WithBaseURL(server.URL + "/")).Scrape(test.Context())
	if err != nil {
		test.Fatalf("Scrape returned an error: %s", err)
	}
	if got, want := len(activities), 1; got != want {
		test.Fatalf("got %d activities, want %d", got, want)
	}
}

func TestScrapeRetriesWhatIsWorthRetrying(test *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a" {
			http.NotFound(writer, r)
			return
		}
		if attempts.Add(1) < 3 {
			http.Error(writer, "oops", http.StatusBadGateway)
			return
		}
		fmt.Fprint(writer, pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01")))
	}))
	defer server.Close()

	activities, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Millisecond),
	).Scrape(test.Context())
	if err != nil {
		test.Fatalf("Scrape returned an error: %s", err)
	}

	if got, want := len(activities), 1; got != want {
		test.Fatalf("got %d activities, want %d", got, want)
	}
	if got, want := attempts.Load(), int32(3); got != want {
		test.Errorf("the page was requested %d times, want %d", got, want)
	}
}

func TestScrapeGivesUpAfterTheLastAttempt(test *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(writer, "oops", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Millisecond),
		WithAttempts(2),
		WithParallel(1),
	).Scrape(test.Context())

	if err == nil {
		test.Fatal("got no error, want the server failure")
	}
	if !strings.Contains(err.Error(), "after 2 attempts") {
		test.Errorf("error is %q, want it to report the attempts", err)
	}
	if got := attempts.Load(); got < 2 {
		test.Errorf("the page was requested %d times, want at least 2", got)
	}
}

func TestScrapeDoesNotRetryWhatWillNotChange(test *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(writer, "no", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Millisecond),
		WithParallel(1),
	).Scrape(test.Context())

	if err == nil {
		test.Fatal("got no error, want the rejection")
	}
	if !strings.Contains(err.Error(), "403") {
		test.Errorf("error is %q, want it to report the status", err)
	}
	if got, want := attempts.Load(), int32(1); got != want {
		test.Errorf("the page was requested %d times, want %d", got, want)
	}
}

func TestScrapeSkipsRowsItCannotRead(test *testing.T) {
	server := listing(test, map[string]string{"b": pageWith(
		row("BARQUEIRO(A) INDEPENDENTE", "50-99"),
		row("BORDADEIRO(A) INDEPENDENTE", "1340-5/99"),
	)})
	var log strings.Builder

	activities, err := NewScraper(WithBaseURL(server.URL), WithLogger(debugLogger(&log))).Scrape(test.Context())
	if err != nil {
		test.Fatalf("Scrape returned an error: %s", err)
	}

	want := Activity{CNAE: 1340599, Occupation: "BORDADEIRO(A) INDEPENDENTE", Letter: "B"}
	if len(activities) != 1 || activities[0] != want {
		test.Fatalf("got %+v, want %+v", activities, []Activity{want})
	}
	if !strings.Contains(log.String(), "row skipped") || !strings.Contains(log.String(), "BARQUEIRO") {
		test.Errorf("the log does not report the skipped row:\n%s", log.String())
	}
}

func TestScrapeStrictStopsAtABrokenRow(test *testing.T) {
	server := listing(test, map[string]string{"b": pageWith(row("BARQUEIRO(A)", "50-99"))})

	_, err := NewScraper(WithBaseURL(server.URL), WithStrict(true)).Scrape(test.Context())

	if err == nil {
		test.Fatal("got no error, want the parsing failure")
	}
	if !strings.Contains(err.Error(), "letter B") {
		test.Errorf("error is %q, want it to name the letter", err)
	}
}

func TestScrapeStopsWhenThereIsNoTable(test *testing.T) {
	server := listing(test, map[string]string{"b": "<html><body><p>Em manutenção</p></body></html>"})

	_, err := NewScraper(WithBaseURL(server.URL)).Scrape(test.Context())

	if !errors.Is(err, ErrNoTable) {
		test.Fatalf("got error %v, want %v even outside strict mode", err, ErrNoTable)
	}
}

func TestUnwrapJoined(test *testing.T) {
	single := errors.New("one problem")
	if got := unwrapJoined(single); len(got) != 1 || got[0] != single {
		test.Errorf("got %v, want the error itself", got)
	}

	other := errors.New("another problem")
	if got := unwrapJoined(errors.Join(single, other)); len(got) != 2 {
		test.Errorf("got %v, want both errors", got)
	}
}

func TestScrapeTooFewActivities(test *testing.T) {
	server := listing(test, map[string]string{"a": pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01"))})

	_, err := NewScraper(WithBaseURL(server.URL), WithMinimum(10)).Scrape(test.Context())

	if !errors.Is(err, ErrTooFewActivities) {
		test.Fatalf("got error %v, want %v", err, ErrTooFewActivities)
	}
	if !strings.Contains(err.Error(), "got 1, expected at least 10") {
		test.Errorf("error is %q, want it to report both counts", err)
	}
}

func TestScrapeStopsWithTheContext(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "oops", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(test.Context())
	cancel()

	_, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Hour),
	).Scrape(ctx)

	if !errors.Is(err, context.Canceled) {
		test.Fatalf("got error %v, want %v", err, context.Canceled)
	}
}

func TestScrapeWithAnInvalidBaseURL(test *testing.T) {
	_, err := NewScraper(WithBaseURL("://not a url")).Scrape(test.Context())

	if err == nil {
		test.Fatal("got no error, want the invalid address")
	}
	if !strings.Contains(err.Error(), "could not create the request") {
		test.Errorf("error is %q, want it to report the request it could not build", err)
	}
}

func TestScrapeWithAnUnreachableServer(test *testing.T) {
	server := listing(test, nil)
	url := server.URL
	server.Close()

	_, err := NewScraper(
		WithBaseURL(url),
		WithBackoff(time.Millisecond),
		WithAttempts(1),
		WithParallel(1),
	).Scrape(test.Context())

	if err == nil {
		test.Fatal("got no error, want the connection failure")
	}
	if !strings.Contains(err.Error(), "could not fetch") {
		test.Errorf("error is %q, want it to report the fetch", err)
	}
}

func TestScraperDefaults(test *testing.T) {
	scraper := NewScraper()

	if scraper.baseURL != DefaultBaseURL {
		test.Errorf("base URL is %q, want %q", scraper.baseURL, DefaultBaseURL)
	}
	if scraper.parallel != DefaultParallel {
		test.Errorf("parallel is %d, want %d", scraper.parallel, DefaultParallel)
	}
	if scraper.attempts != DefaultAttempts {
		test.Errorf("attempts is %d, want %d", scraper.attempts, DefaultAttempts)
	}
	if scraper.backoff != DefaultBackoff {
		test.Errorf("backoff is %s, want %s", scraper.backoff, DefaultBackoff)
	}
	if scraper.client.Timeout != DefaultTimeout {
		test.Errorf("timeout is %s, want %s", scraper.client.Timeout, DefaultTimeout)
	}
	if scraper.minimum != 0 {
		test.Errorf("minimum is %d, want it off by default", scraper.minimum)
	}
}

func TestScraperOptions(test *testing.T) {
	client := &http.Client{Timeout: time.Minute}
	scraper := NewScraper(
		WithBaseURL("https://example.com/listing/"),
		WithHTTPClient(client),
		WithParallel(2),
		WithAttempts(5),
		WithBackoff(time.Second),
		WithMinimum(100),
	)

	if want := "https://example.com/listing"; scraper.baseURL != want {
		test.Errorf("base URL is %q, want %q", scraper.baseURL, want)
	}
	if scraper.client != client {
		test.Error("the HTTP client was not replaced")
	}
	if scraper.parallel != 2 || scraper.attempts != 5 || scraper.backoff != time.Second || scraper.minimum != 100 {
		test.Errorf("options not applied: %+v", scraper)
	}
}

func TestScrapeLogsEachLetter(test *testing.T) {
	server := listing(test, map[string]string{"a": pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01"))})
	var log strings.Builder

	_, err := NewScraper(WithBaseURL(server.URL), WithLogger(debugLogger(&log))).Scrape(test.Context())
	if err != nil {
		test.Fatalf("Scrape returned an error: %s", err)
	}

	for _, want := range []string{"letter read", "letter with no page", "activities collected"} {
		if !strings.Contains(log.String(), want) {
			test.Errorf("the log does not mention %q:\n%s", want, log.String())
		}
	}
}

// debugLogger writes every log line, down to debug level, to w.
func debugLogger(writer io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestScrapeStopsWhileWaitingToRetry(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "oops", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(test.Context(), 20*time.Millisecond)
	defer cancel()

	_, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Hour),
		WithParallel(1),
	).Scrape(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		test.Fatalf("got error %v, want %v", err, context.DeadlineExceeded)
	}
}
