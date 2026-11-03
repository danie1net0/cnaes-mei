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
func listing(t *testing.T, pages map[string]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		letter := strings.TrimPrefix(r.URL.Path, "/")
		page, ok := pages[letter]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, page)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestScrape(t *testing.T) {
	server := listing(t, map[string]string{
		"a": pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01")),
		"c": pageWith(
			row("CULTIVO DE ARROZ", "0111-3/01"),
			row("COSTUREIRO(A) INDEPENDENTE", "1412-6/01"),
		),
	})

	activities, err := NewScraper(WithBaseURL(server.URL)).Scrape(t.Context())
	if err != nil {
		t.Fatalf("Scrape returned an error: %s", err)
	}

	want := []Activity{
		{CNAE: 111301, Occupation: "CULTIVO DE ARROZ", Letter: "C"},
		{CNAE: 1412601, Occupation: "COSTUREIRO(A) INDEPENDENTE", Letter: "C"},
		{CNAE: 4722901, Occupation: "AÇOUGUEIRO(A) INDEPENDENTE", Letter: "A"},
	}
	if len(activities) != len(want) {
		t.Fatalf("got %d activities, want %d: %+v", len(activities), len(want), activities)
	}
	for i := range want {
		if activities[i] != want[i] {
			t.Errorf("activity %d is %+v, want %+v", i, activities[i], want[i])
		}
	}
}

func TestScrapeTrailingSlashInTheBaseURL(t *testing.T) {
	server := listing(t, map[string]string{"a": pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01"))})

	activities, err := NewScraper(WithBaseURL(server.URL + "/")).Scrape(t.Context())
	if err != nil {
		t.Fatalf("Scrape returned an error: %s", err)
	}
	if got, want := len(activities), 1; got != want {
		t.Fatalf("got %d activities, want %d", got, want)
	}
}

func TestScrapeRetriesWhatIsWorthRetrying(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a" {
			http.NotFound(w, r)
			return
		}
		if attempts.Add(1) < 3 {
			http.Error(w, "oops", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01")))
	}))
	defer server.Close()

	activities, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Millisecond),
	).Scrape(t.Context())
	if err != nil {
		t.Fatalf("Scrape returned an error: %s", err)
	}

	if got, want := len(activities), 1; got != want {
		t.Fatalf("got %d activities, want %d", got, want)
	}
	if got, want := attempts.Load(), int32(3); got != want {
		t.Errorf("the page was requested %d times, want %d", got, want)
	}
}

func TestScrapeGivesUpAfterTheLastAttempt(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "oops", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Millisecond),
		WithAttempts(2),
		WithParallel(1),
	).Scrape(t.Context())

	if err == nil {
		t.Fatal("got no error, want the server failure")
	}
	if !strings.Contains(err.Error(), "after 2 attempts") {
		t.Errorf("error is %q, want it to report the attempts", err)
	}
	if got := attempts.Load(); got < 2 {
		t.Errorf("the page was requested %d times, want at least 2", got)
	}
}

func TestScrapeDoesNotRetryWhatWillNotChange(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Millisecond),
		WithParallel(1),
	).Scrape(t.Context())

	if err == nil {
		t.Fatal("got no error, want the rejection")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error is %q, want it to report the status", err)
	}
	if got, want := attempts.Load(), int32(1); got != want {
		t.Errorf("the page was requested %d times, want %d", got, want)
	}
}

func TestScrapeSkipsRowsItCannotRead(t *testing.T) {
	server := listing(t, map[string]string{"b": pageWith(
		row("BARQUEIRO(A) INDEPENDENTE", "50-99"),
		row("BORDADEIRO(A) INDEPENDENTE", "1340-5/99"),
	)})
	var log strings.Builder

	activities, err := NewScraper(WithBaseURL(server.URL), WithLogger(debugLogger(&log))).Scrape(t.Context())
	if err != nil {
		t.Fatalf("Scrape returned an error: %s", err)
	}

	want := Activity{CNAE: 1340599, Occupation: "BORDADEIRO(A) INDEPENDENTE", Letter: "B"}
	if len(activities) != 1 || activities[0] != want {
		t.Fatalf("got %+v, want %+v", activities, []Activity{want})
	}
	if !strings.Contains(log.String(), "row skipped") || !strings.Contains(log.String(), "BARQUEIRO") {
		t.Errorf("the log does not report the skipped row:\n%s", log.String())
	}
}

func TestScrapeStrictStopsAtABrokenRow(t *testing.T) {
	server := listing(t, map[string]string{"b": pageWith(row("BARQUEIRO(A)", "50-99"))})

	_, err := NewScraper(WithBaseURL(server.URL), WithStrict(true)).Scrape(t.Context())

	if err == nil {
		t.Fatal("got no error, want the parsing failure")
	}
	if !strings.Contains(err.Error(), "letter B") {
		t.Errorf("error is %q, want it to name the letter", err)
	}
}

func TestScrapeStopsWhenThereIsNoTable(t *testing.T) {
	server := listing(t, map[string]string{"b": "<html><body><p>Em manutenção</p></body></html>"})

	_, err := NewScraper(WithBaseURL(server.URL)).Scrape(t.Context())

	if !errors.Is(err, ErrNoTable) {
		t.Fatalf("got error %v, want %v even outside strict mode", err, ErrNoTable)
	}
}

func TestUnwrapJoined(t *testing.T) {
	single := errors.New("one problem")
	if got := unwrapJoined(single); len(got) != 1 || got[0] != single {
		t.Errorf("got %v, want the error itself", got)
	}

	other := errors.New("another problem")
	if got := unwrapJoined(errors.Join(single, other)); len(got) != 2 {
		t.Errorf("got %v, want both errors", got)
	}
}

func TestScrapeTooFewActivities(t *testing.T) {
	server := listing(t, map[string]string{"a": pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01"))})

	_, err := NewScraper(WithBaseURL(server.URL), WithMinimum(10)).Scrape(t.Context())

	if !errors.Is(err, ErrTooFewActivities) {
		t.Fatalf("got error %v, want %v", err, ErrTooFewActivities)
	}
	if !strings.Contains(err.Error(), "got 1, expected at least 10") {
		t.Errorf("error is %q, want it to report both counts", err)
	}
}

func TestScrapeStopsWithTheContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "oops", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Hour),
	).Scrape(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got error %v, want %v", err, context.Canceled)
	}
}

func TestScrapeWithAnInvalidBaseURL(t *testing.T) {
	_, err := NewScraper(WithBaseURL("://not a url")).Scrape(t.Context())

	if err == nil {
		t.Fatal("got no error, want the invalid address")
	}
	if !strings.Contains(err.Error(), "could not create the request") {
		t.Errorf("error is %q, want it to report the request it could not build", err)
	}
}

func TestScrapeWithAnUnreachableServer(t *testing.T) {
	server := listing(t, nil)
	url := server.URL
	server.Close()

	_, err := NewScraper(
		WithBaseURL(url),
		WithBackoff(time.Millisecond),
		WithAttempts(1),
		WithParallel(1),
	).Scrape(t.Context())

	if err == nil {
		t.Fatal("got no error, want the connection failure")
	}
	if !strings.Contains(err.Error(), "could not fetch") {
		t.Errorf("error is %q, want it to report the fetch", err)
	}
}

func TestScraperDefaults(t *testing.T) {
	s := NewScraper()

	if s.baseURL != DefaultBaseURL {
		t.Errorf("base URL is %q, want %q", s.baseURL, DefaultBaseURL)
	}
	if s.parallel != DefaultParallel {
		t.Errorf("parallel is %d, want %d", s.parallel, DefaultParallel)
	}
	if s.attempts != DefaultAttempts {
		t.Errorf("attempts is %d, want %d", s.attempts, DefaultAttempts)
	}
	if s.backoff != DefaultBackoff {
		t.Errorf("backoff is %s, want %s", s.backoff, DefaultBackoff)
	}
	if s.client.Timeout != DefaultTimeout {
		t.Errorf("timeout is %s, want %s", s.client.Timeout, DefaultTimeout)
	}
	if s.minimum != 0 {
		t.Errorf("minimum is %d, want it off by default", s.minimum)
	}
}

func TestScraperOptions(t *testing.T) {
	client := &http.Client{Timeout: time.Minute}
	s := NewScraper(
		WithBaseURL("https://example.com/listing/"),
		WithHTTPClient(client),
		WithParallel(2),
		WithAttempts(5),
		WithBackoff(time.Second),
		WithMinimum(100),
	)

	if want := "https://example.com/listing"; s.baseURL != want {
		t.Errorf("base URL is %q, want %q", s.baseURL, want)
	}
	if s.client != client {
		t.Error("the HTTP client was not replaced")
	}
	if s.parallel != 2 || s.attempts != 5 || s.backoff != time.Second || s.minimum != 100 {
		t.Errorf("options not applied: %+v", s)
	}
}

func TestScrapeLogsEachLetter(t *testing.T) {
	server := listing(t, map[string]string{"a": pageWith(row("AÇOUGUEIRO(A) INDEPENDENTE", "4722-9/01"))})
	var log strings.Builder

	_, err := NewScraper(WithBaseURL(server.URL), WithLogger(debugLogger(&log))).Scrape(t.Context())
	if err != nil {
		t.Fatalf("Scrape returned an error: %s", err)
	}

	for _, want := range []string{"letter read", "letter with no page", "activities collected"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("the log does not mention %q:\n%s", want, log.String())
		}
	}
}

// debugLogger writes every log line, down to debug level, to w.
func debugLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestScrapeStopsWhileWaitingToRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "oops", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	_, err := NewScraper(
		WithBaseURL(server.URL),
		WithBackoff(time.Hour),
		WithParallel(1),
	).Scrape(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got error %v, want %v", err, context.DeadlineExceeded)
	}
}
