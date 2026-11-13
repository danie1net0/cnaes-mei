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
type commandOptions struct {
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

	err := Run(ctx, args, stdout, stderr)
	if err == nil {
		return 0
	}

	// A usage error has already printed the usage text.
	if !IsUsageError(err) {
		fmt.Fprintln(stderr, "cnaes-mei:", err)
	}
	return 1
}

// Run executes the command and returns the error to report to the user.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	options, err := parse(args, stderr)
	if err != nil {
		return err
	}
	if options.version {
		fmt.Fprintln(stdout, Version)
		return nil
	}

	level := slog.LevelInfo
	if options.verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	scraper := mei.NewScraper(
		mei.WithBaseURL(options.baseURL),
		mei.WithHTTPClient(&http.Client{Timeout: options.timeout}),
		mei.WithLogger(logger),
		mei.WithParallel(options.parallel),
		mei.WithAttempts(options.attempts),
		mei.WithMinimum(options.minimum),
		mei.WithStrict(options.strict),
	)

	activities, err := scraper.Scrape(ctx)
	if err != nil {
		return err
	}

	if err := write(options, stdout, activities); err != nil {
		return err
	}
	return store(ctx, options, logger, activities)
}

// parse reads the flags, applying the environment variables as defaults.
func parse(args []string, stderr io.Writer) (commandOptions, error) {
	var options commandOptions
	var format string

	flags := flag.NewFlagSet("cnaes-mei", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, usage)
		flags.PrintDefaults()
	}

	flags.StringVar(&format, "format", string(output.CSV), "output format: "+output.Names())
	flags.StringVar(&options.outputPath, "output", stdoutPath, "file to write to, or - for standard output")
	flags.StringVar(&options.databaseURI, "database-url", os.Getenv("DATABASE_URL"), "PostgreSQL URI to store the activities in (default DATABASE_URL)")
	flags.StringVar(&options.table, "table", postgres.DefaultTable, "table to store the activities in")
	flags.StringVar(&options.baseURL, "base-url", baseURLDefault(), "address of the listing (default MEI_BASE_URL, else the gov.br listing)")
	flags.IntVar(&options.minimum, "min", defaultMinimum, "fail when fewer than this many activities are found; 0 disables the check")
	flags.IntVar(&options.parallel, "parallel", mei.DefaultParallel, "how many pages to fetch at the same time")
	flags.IntVar(&options.attempts, "attempts", mei.DefaultAttempts, "how many times to try a page that fails")
	flags.DurationVar(&options.timeout, "timeout", mei.DefaultTimeout, "timeout of each request")
	flags.BoolVar(&options.strict, "strict", false, "stop when a row cannot be read, instead of logging and skipping it")
	flags.BoolVar(&options.verbose, "verbose", false, "log each letter as it is read")
	flags.BoolVar(&options.version, "version", false, "print the version and exit")

	if err := flags.Parse(args); err != nil {
		return commandOptions{}, err
	}
	if flags.NArg() > 0 {
		return commandOptions{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}

	parsed, err := output.ParseFormat(format)
	if err != nil {
		return commandOptions{}, err
	}
	options.format = parsed

	// Without a database the activities have nowhere else to go, so they are
	// written out; with one, only an explicit -output asks for both.
	options.writeOutput = options.databaseURI == "" || isSet(flags, "output")
	return options, nil
}

// isSet reports whether the user passed a flag, as opposed to it keeping its
// default.
func isSet(flags *flag.FlagSet, name string) bool {
	var set bool
	flags.Visit(func(visited *flag.Flag) {
		if visited.Name == name {
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

// write sends the activities to standard output or to a file, and does nothing
// when the activities are only meant for the database. The error of closing the
// file joins the error of writing it, so that a failure to flush the last bytes
// is not lost.
func write(options commandOptions, stdout io.Writer, activities []mei.Activity) (err error) {
	if !options.writeOutput {
		return nil
	}
	if options.outputPath == stdoutPath {
		return output.Write(stdout, options.format, activities)
	}

	file, createError := os.Create(options.outputPath)
	if createError != nil {
		return fmt.Errorf("could not create %s: %w", options.outputPath, createError)
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	return output.Write(file, options.format, activities)
}

// store replaces the contents of the database table, and does nothing when no
// database was given.
func store(ctx context.Context, options commandOptions, logger *slog.Logger, activities []mei.Activity) error {
	if options.databaseURI == "" {
		return nil
	}

	database, err := postgres.Connect(ctx, options.databaseURI)
	if err != nil {
		return err
	}
	defer database.Close(ctx) //nolint:errcheck // closing a connection we are done with

	if err := database.Replace(ctx, options.table, activities); err != nil {
		return err
	}
	logger.Info("activities stored", "table", options.table, "total", len(activities))
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
