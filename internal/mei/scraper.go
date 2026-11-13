package mei

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// DefaultBaseURL is where the government publishes the occupations, one page
// per initial letter.
const DefaultBaseURL = "https://www.gov.br/empresas-e-negocios/pt-br/empreendedor/quero-ser-mei/atividades-permitidas"

// DefaultUserAgent identifies this scraper to the server. A blank user agent is
// rejected by the government's CDN.
const DefaultUserAgent = "cnaes-mei (+https://gitlab.com/danie1net0/cnaes-mei)"

// Defaults for a Scraper left zeroed.
const (
	DefaultParallel = 4
	DefaultAttempts = 3
	DefaultBackoff  = time.Second
	DefaultTimeout  = 30 * time.Second
)

// ErrTooFewActivities reports a harvest far smaller than expected, which is how
// a silent layout change shows up: the pages still answer 200 and the tables
// still parse, but almost nothing comes out of them.
var ErrTooFewActivities = errors.New("too few activities")

// letters are the initials that have a page. The listing has no index to crawl,
// so every letter of the alphabet is tried and the ones that answer 404 are
// skipped — that way a letter gained or lost by the source needs no code change.
var letters = []string{
	"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
	"n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
}

// Scraper reads the published listing. The zero value is not usable; call
// NewScraper.
type Scraper struct {
	baseURL   string
	client    *http.Client
	logger    *slog.Logger
	userAgent string
	parallel  int
	attempts  int
	backoff   time.Duration
	minimum   int
	strict    bool
}

// Option customizes a Scraper.
type Option func(*Scraper)

// WithBaseURL points the scraper at another address, which is what the tests
// and a mirror of the listing need.
func WithBaseURL(url string) Option {
	return func(scraper *Scraper) { scraper.baseURL = strings.TrimSuffix(url, "/") }
}

// WithHTTPClient replaces the HTTP client, including its timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(scraper *Scraper) { scraper.client = client }
}

// WithLogger sets where progress is reported.
func WithLogger(logger *slog.Logger) Option {
	return func(scraper *Scraper) { scraper.logger = logger }
}

// WithParallel sets how many letter pages are fetched at the same time.
func WithParallel(count int) Option {
	return func(scraper *Scraper) { scraper.parallel = count }
}

// WithAttempts sets how many times a failing page is tried before giving up.
func WithAttempts(count int) Option {
	return func(scraper *Scraper) { scraper.attempts = count }
}

// WithBackoff sets the pause before the first retry; it doubles at each attempt.
func WithBackoff(duration time.Duration) Option {
	return func(scraper *Scraper) { scraper.backoff = duration }
}

// WithMinimum sets how many activities a harvest must yield to be accepted.
// Zero turns the check off.
func WithMinimum(count int) Option {
	return func(scraper *Scraper) { scraper.minimum = count }
}

// WithStrict makes a row that cannot be read stop the whole run. By default
// such a row is logged and skipped, because the listing does publish the odd
// malformed CNAE and one typo should not cost the other five hundred rows.
func WithStrict(strict bool) Option {
	return func(scraper *Scraper) { scraper.strict = strict }
}

// NewScraper builds a scraper with the defaults applied, then the options.
func NewScraper(options ...Option) *Scraper {
	scraper := &Scraper{
		baseURL:   DefaultBaseURL,
		client:    &http.Client{Timeout: DefaultTimeout},
		logger:    slog.New(slog.DiscardHandler),
		userAgent: DefaultUserAgent,
		parallel:  DefaultParallel,
		attempts:  DefaultAttempts,
		backoff:   DefaultBackoff,
	}
	for _, option := range options {
		option(scraper)
	}
	return scraper
}

// Scrape fetches every letter page and returns the activities, sorted by CNAE
// and occupation so that two runs over unchanged pages produce the same bytes.
func (scraper *Scraper) Scrape(ctx context.Context) ([]Activity, error) {
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(scraper.parallel)

	found := make([][]Activity, len(letters))
	for i, letter := range letters {
		group.Go(func() error {
			activities, err := scraper.scrapeLetter(ctx, letter)
			if err != nil {
				return err
			}
			found[i] = activities
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	var activities []Activity
	for _, fromLetter := range found {
		activities = append(activities, fromLetter...)
	}
	slices.SortFunc(activities, func(first, second Activity) int {
		return cmp.Or(cmp.Compare(first.CNAE, second.CNAE), cmp.Compare(first.Occupation, second.Occupation))
	})

	if scraper.minimum > 0 && len(activities) < scraper.minimum {
		return nil, fmt.Errorf("%w: got %d, expected at least %d — the listing may have changed", ErrTooFewActivities, len(activities), scraper.minimum)
	}
	scraper.logger.Info("activities collected", "total", len(activities))
	return activities, nil
}

// scrapeLetter fetches and parses one letter page. A letter with no page is not
// an error: not every initial has occupations.
func (scraper *Scraper) scrapeLetter(ctx context.Context, letter string) ([]Activity, error) {
	body, found, err := scraper.get(ctx, scraper.baseURL+"/"+letter)
	if err != nil {
		return nil, err
	}
	if !found {
		scraper.logger.Debug("letter with no page", "letter", letter)
		return nil, nil
	}
	defer body.Close()

	upper := strings.ToUpper(letter)
	activities, err := ParsePage(body, upper)
	if err != nil && (scraper.strict || errors.Is(err, ErrNoTable)) {
		return nil, fmt.Errorf("letter %s: %w", upper, err)
	}

	for _, problem := range unwrapJoined(err) {
		scraper.logger.Warn("row skipped", "letter", upper, "reason", problem)
	}

	scraper.logger.Debug("letter read", "letter", upper, "activities", len(activities))
	return activities, nil
}

// get fetches a URL, retrying what is worth retrying. The second return value
// is false when the page does not exist, which the caller treats as empty
// rather than as a failure. The body is the caller's to close.
func (scraper *Scraper) get(ctx context.Context, url string) (io.ReadCloser, bool, error) {
	var last error
	for attempt := 1; attempt <= scraper.attempts; attempt++ {
		if err := scraper.pauseBeforeRetry(ctx, url, attempt, last); err != nil {
			return nil, false, err
		}

		body, found, err := scraper.try(ctx, url)
		if err == nil {
			return body, found, nil
		}
		last = err
		if ctx.Err() != nil || !retryable(err) {
			return nil, false, err
		}
	}
	return nil, false, fmt.Errorf("gave up on %s after %d attempts: %w", url, scraper.attempts, last)
}

// pauseBeforeRetry waits before every attempt after the first, doubling the
// wait as the attempts go by. The first attempt waits for nothing.
func (scraper *Scraper) pauseBeforeRetry(ctx context.Context, url string, attempt int, last error) error {
	if attempt == 1 {
		return nil
	}

	if err := sleep(ctx, scraper.backoff<<(attempt-2)); err != nil {
		return err
	}

	scraper.logger.Debug("retrying", "url", url, "attempt", attempt, "previous_error", last)
	return nil
}

// try performs a single request.
func (scraper *Scraper) try(ctx context.Context, url string) (io.ReadCloser, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("could not create the request for %s: %w", url, err)
	}
	request.Header.Set("User-Agent", scraper.userAgent)

	response, err := scraper.client.Do(request)
	if err != nil {
		return nil, false, retry(fmt.Errorf("could not fetch %s: %w", url, err))
	}

	switch {
	case response.StatusCode == http.StatusNotFound:
		response.Body.Close()
		return nil, false, nil
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError:
		response.Body.Close()
		return nil, false, retry(fmt.Errorf("%s answered %s", url, response.Status))
	case response.StatusCode != http.StatusOK:
		response.Body.Close()
		return nil, false, fmt.Errorf("%s answered %s", url, response.Status)
	}
	return response.Body, true, nil
}

// retryableError marks a failure worth another attempt: a network hiccup, a
// rate limit or a server-side error.
type retryableError struct{ error }

// Unwrap keeps errors.Is working through the marker, so that a caller can still
// recognize a context cancellation or any other wrapped cause.
func (wrapped retryableError) Unwrap() error { return wrapped.error }

func retry(err error) error { return retryableError{err} }

func retryable(err error) bool {
	var r retryableError
	return errors.As(err, &r)
}

// unwrapJoined splits the error of errors.Join back into its parts, so that
// each unreadable row becomes one log line. No error is no parts, which lets
// the caller loop over the result without asking first.
func unwrapJoined(err error) []error {
	if err == nil {
		return nil
	}

	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []error{err}
	}
	return joined.Unwrap()
}

// sleep waits for the given duration, or stops early when the context is done.
func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
