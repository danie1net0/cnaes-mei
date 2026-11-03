// Package cli wires the command line to the scraper, the writers and the
// database.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"gitlab.com/danie1net0/cnaes-mei/internal/mei"
	"gitlab.com/danie1net0/cnaes-mei/internal/output"
	"gitlab.com/danie1net0/cnaes-mei/internal/postgres"
)

// defaultMinimum is the smallest harvest accepted by default. The listing had
// 500-odd occupations when this was written, so a run that brings back fewer
// than 400 means the source changed, not that the MEI lost a third of its
// occupations overnight.
const defaultMinimum = 400

// stdoutPath is the output file name that means standard output.
const stdoutPath = "-"

const usage = `cnaes-mei collects the occupations allowed for an MEI, and the CNAE of each
one, from the listing published at gov.br.

Usage:
  cnaes-mei [flags]

By default the activities are written to standard output. With -database-url
they are written to a PostgreSQL table instead, replacing its contents in a
single transaction; passing -output as well does both.

Flags:
`

// options are the command-line options, after parsing.
type options struct {
	format      output.Format
	outputPath  string
	databaseURI string
	table       string
	baseURL     string
	minimum     int
	parallel    int
	attempts    int
	timeout     time.Duration
	strict      bool
	verbose     bool
	version     bool
	writeOutput bool
}

// Main runs the command, reports what went wrong and returns the exit code.
// It is everything main() does, kept here so that it can be tested.
func Main(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := Run(ctx, args, stdout, stderr); err != nil {
		// A usage error has already printed the usage text.
		if !IsUsageError(err) {
			fmt.Fprintln(stderr, "cnaes-mei:", err)
		}
		return 1
	}
	return 0
}

// Run executes the command and returns the error to report to the user.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opts, err := parse(args, stderr)
	if err != nil {
		return err
	}
	if opts.version {
		fmt.Fprintln(stdout, Version)
		return nil
	}

	level := slog.LevelInfo
	if opts.verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	scraper := mei.NewScraper(
		mei.WithBaseURL(opts.baseURL),
		mei.WithHTTPClient(&http.Client{Timeout: opts.timeout}),
		mei.WithLogger(logger),
		mei.WithParallel(opts.parallel),
		mei.WithAttempts(opts.attempts),
		mei.WithMinimum(opts.minimum),
		mei.WithStrict(opts.strict),
	)

	activities, err := scraper.Scrape(ctx)
	if err != nil {
		return err
	}

	if opts.writeOutput {
		if err := write(opts, stdout, activities); err != nil {
			return err
		}
	}
	if opts.databaseURI != "" {
		if err := store(ctx, opts, logger, activities); err != nil {
			return err
		}
	}
	return nil
}

// parse reads the flags, applying the environment variables as defaults.
func parse(args []string, stderr io.Writer) (options, error) {
	var opts options
	var format string

	flags := flag.NewFlagSet("cnaes-mei", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, usage)
		flags.PrintDefaults()
	}

	flags.StringVar(&format, "format", string(output.CSV), "output format: "+output.Names())
	flags.StringVar(&opts.outputPath, "output", stdoutPath, "file to write to, or - for standard output")
	flags.StringVar(&opts.databaseURI, "database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URI to store the activities in (default DATABASE_URL)")
	flags.StringVar(&opts.table, "table", postgres.DefaultTable, "table to store the activities in")
	flags.StringVar(&opts.baseURL, "base-url", baseURLDefault(), "address of the listing (default MEI_BASE_URL, else the gov.br listing)")
	flags.IntVar(&opts.minimum, "min", defaultMinimum, "fail when fewer than this many activities are found; 0 disables the check")
	flags.IntVar(&opts.parallel, "parallel", mei.DefaultParallel, "how many pages to fetch at the same time")
	flags.IntVar(&opts.attempts, "attempts", mei.DefaultAttempts, "how many times to try a page that fails")
	flags.DurationVar(&opts.timeout, "timeout", mei.DefaultTimeout, "timeout of each request")
	flags.BoolVar(&opts.strict, "strict", false, "stop when a row cannot be read, instead of logging and skipping it")
	flags.BoolVar(&opts.verbose, "verbose", false, "log each letter as it is read")
	flags.BoolVar(&opts.version, "version", false, "print the version and exit")

	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}

	parsed, err := output.ParseFormat(format)
	if err != nil {
		return options{}, err
	}
	opts.format = parsed

	// Without a database the activities have nowhere else to go, so they are
	// written out; with one, only an explicit -output asks for both.
	opts.writeOutput = opts.databaseURI == "" || isSet(flags, "output")
	return opts, nil
}

// isSet reports whether the user passed a flag, as opposed to it keeping its
// default.
func isSet(flags *flag.FlagSet, name string) bool {
	var set bool
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// baseURLDefault lets the listing be pointed elsewhere without a flag, the way
// the tests and a mirror need.
func baseURLDefault() string {
	if url := os.Getenv("MEI_BASE_URL"); url != "" {
		return url
	}
	return mei.DefaultBaseURL
}

// write sends the activities to standard output or to a file. The error of
// closing the file joins the error of writing it, so that a failure to flush
// the last bytes is not lost.
func write(opts options, stdout io.Writer, activities []mei.Activity) (err error) {
	out := stdout
	if opts.outputPath != stdoutPath {
		file, ferr := os.Create(opts.outputPath)
		if ferr != nil {
			return fmt.Errorf("could not create %s: %w", opts.outputPath, ferr)
		}
		defer func() { err = errors.Join(err, file.Close()) }()
		out = file
	}
	return output.Write(out, opts.format, activities)
}

// store replaces the contents of the database table.
func store(ctx context.Context, opts options, logger *slog.Logger, activities []mei.Activity) error {
	db, err := postgres.Connect(ctx, opts.databaseURI)
	if err != nil {
		return err
	}
	defer db.Close(ctx) //nolint:errcheck // closing a connection we are done with

	if err := db.Replace(ctx, opts.table, activities); err != nil {
		return err
	}
	logger.Info("activities stored", "table", opts.table, "total", len(activities))
	return nil
}

// Version is what the -version flag prints. A release build sets it with
//
//	go build -ldflags "-X gitlab.com/danie1net0/cnaes-mei/internal/cli.Version=$(git describe --tags --always)"
var Version = "devel"

// IsUsageError reports whether the error came from the flags themselves, in
// which case the usage text has already been printed.
func IsUsageError(err error) bool {
	return errors.Is(err, flag.ErrHelp)
}
