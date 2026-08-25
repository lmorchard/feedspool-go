package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/fetcher"
	"github.com/lmorchard/feedspool-go/internal/sitegroup"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	fetchTimeout       time.Duration
	fetchMaxItems      int
	fetchForce         bool
	fetchConcurrency   int
	fetchMaxAge        time.Duration
	fetchRemoveMissing bool
	fetchFormat        string
	fetchFilename      string
	fetchWithUnfurl    bool
	fetchFeedsDir      string
)

var fetchCmd = &cobra.Command{
	Use:   "fetch [URL]",
	Short: "Fetch feeds from URL, file, or database",
	Long: `Fetch command operates in multiple modes:

Single URL:
  feedspool fetch <url>         # Fetch one feed from URL

From file:  
  feedspool fetch --format opml --filename feeds.opml    # Fetch all feeds in OPML file
  feedspool fetch --format text --filename feeds.txt     # Fetch all feeds in text file

From database:
  feedspool fetch                                         # Fetch all feeds in database

The command supports all options from the former 'update' command including concurrency
control, age filtering, and database cleanup based on feed lists.

Parallel Unfurl:
  feedspool fetch --with-unfurl                          # Fetch feeds and unfurl metadata in parallel
  
The --with-unfurl flag enables parallel metadata extraction for new feed items. This runs
unfurl operations concurrently with feed fetching for improved performance. Only new items
without existing metadata will be processed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFetch,
}

func init() {
	fetchCmd.Flags().DurationVar(&fetchTimeout, "timeout", config.DefaultTimeout, "Feed fetch timeout")
	fetchCmd.Flags().IntVar(&fetchMaxItems, "max-items", config.DefaultMaxItems, "Maximum items to keep per feed")
	fetchCmd.Flags().BoolVar(&fetchForce, "force", false, "Ignore cache headers and fetch anyway")
	fetchCmd.Flags().IntVar(&fetchConcurrency, "concurrency", config.DefaultConcurrency, "Maximum concurrent fetches")
	fetchCmd.Flags().DurationVar(&fetchMaxAge, "max-age", 0, "Skip feeds fetched within this duration")
	fetchCmd.Flags().BoolVar(&fetchRemoveMissing, "remove-missing", false, "Delete feeds not in list (file mode only)")
	fetchCmd.Flags().StringVar(&fetchFormat, "format", "", "Feed list format (opml or text)")
	fetchCmd.Flags().StringVar(&fetchFilename, "filename", "", "Feed list filename")
	fetchCmd.Flags().BoolVar(&fetchWithUnfurl, "with-unfurl", false,
		"Run unfurl operations in parallel with feed fetching")
	fetchCmd.Flags().StringVar(&fetchFeedsDir, "feeds-dir", "",
		"Directory of OPML/text feed lists; fetches the deduped union of all of them")
	rootCmd.AddCommand(fetchCmd)
}

// setupGracefulShutdown sets up signal handling for graceful shutdown.
func setupGracefulShutdown() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		logrus.Infof("Received signal %v, shutting down gracefully...", sig)
		cancel()
	}()

	return ctx, cancel
}

func runFetch(_ *cobra.Command, args []string) error {
	cfg := GetConfig()

	if err := cfg.Validate(); err != nil {
		return err
	}

	// Check flag-level contradictions before anything that touches the
	// database, so a bad flag combination is reported instead of being
	// masked by an unrelated "database not initialized" error.
	if err := checkFetchFlagConflicts(); err != nil {
		return err
	}

	// Determine final withUnfurl value: CLI flag takes precedence over config
	withUnfurl := cfg.Fetch.WithUnfurl || fetchWithUnfurl

	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	// Create orchestrator
	orchestrator := fetcher.NewOrchestrator(db, cfg)

	// Set up graceful shutdown
	ctx, cancel := setupGracefulShutdown()
	defer cancel()

	// Use CLI flags if provided, otherwise fall back to config values
	concurrency := fetchConcurrency
	if fetchConcurrency == config.DefaultConcurrency {
		concurrency = cfg.Fetch.Concurrency
	}

	maxItems := fetchMaxItems
	if fetchMaxItems == config.DefaultMaxItems {
		maxItems = cfg.Fetch.MaxItems
	}

	// Create fetch options
	opts := fetcher.FetchOptions{
		Timeout:       fetchTimeout,
		MaxItems:      maxItems,
		MaxAge:        fetchMaxAge,
		Force:         fetchForce,
		Concurrency:   concurrency,
		WithUnfurl:    withUnfurl,
		RemoveMissing: fetchRemoveMissing,
	}

	// Determine fetch mode and execute.
	return dispatchFetch(ctx, orchestrator, args, opts, cfg)
}

// checkFetchFlagConflicts reports flag combinations that are a contradiction
// rather than a precedence question. It only inspects the flag globals, so it
// can run before any database or orchestrator setup.
func checkFetchFlagConflicts() error {
	if fetchFeedsDir != "" && (fetchFormat != "" || fetchFilename != "") {
		return errors.New("--feeds-dir cannot be combined with --format or --filename")
	}
	return nil
}

// dispatchFetch chooses among single-URL, directory, file, and database fetch
// modes and runs the chosen one.
func dispatchFetch(
	ctx context.Context, orchestrator *fetcher.Orchestrator, args []string,
	opts fetcher.FetchOptions, cfg *config.Config,
) error {
	if len(args) == 1 {
		return runSingleURLFetch(ctx, orchestrator, args[0], opts, cfg)
	}

	// An explicit --filename/--format overrides a configured feedlist.dir:
	// single-file mode wins when you ask for it directly. The contradiction
	// between --feeds-dir and those flags was already rejected in
	// checkFetchFlagConflicts, before the database was even opened.
	explicitFile := fetchFormat != "" || fetchFilename != ""

	feedsDir := fetchFeedsDir
	if feedsDir == "" && !explicitFile && cfg.HasFeedListDir() {
		feedsDir = cfg.FeedList.Dir
	}
	if feedsDir != "" {
		return runDirFetch(ctx, orchestrator, feedsDir, opts, cfg)
	}

	if explicitFile || cfg.HasDefaultFeedList() {
		return runFileFetch(ctx, orchestrator, opts, cfg)
	}

	// Database mode (no args, no file flags, no config defaults).
	return runDatabaseFetch(ctx, orchestrator, opts, cfg)
}

func runSingleURLFetch(
	ctx context.Context, orchestrator *fetcher.Orchestrator, feedURL string,
	opts fetcher.FetchOptions, cfg *config.Config,
) error {
	result, err := orchestrator.FetchSingle(ctx, feedURL, opts)
	if err != nil {
		return fmt.Errorf("failed to fetch feed: %w", err)
	}

	fetcher.PrintSingleResult(result, cfg)
	return nil
}

func runFileFetch(
	ctx context.Context, orchestrator *fetcher.Orchestrator,
	opts fetcher.FetchOptions, cfg *config.Config,
) error {
	validator := fetcher.FormatValidation{}

	format, filename, err := validator.DetermineFormatAndFilename(cfg, fetchFormat, fetchFilename)
	if err != nil {
		return err
	}

	feedFormat, err := validator.ValidateFormat(format)
	if err != nil {
		return err
	}

	results, err := orchestrator.FetchFromFile(ctx, feedFormat, filename, opts)
	if err != nil {
		return err
	}

	// Process and display results
	summary := fetcher.ProcessResults(results)
	summary.Mode = "file"
	summary.Print(cfg)

	return nil
}

func runDirFetch(
	ctx context.Context, orchestrator *fetcher.Orchestrator, dir string,
	opts fetcher.FetchOptions, cfg *config.Config,
) error {
	plan, err := sitegroup.PlanFetch(dir)
	if err != nil {
		return err
	}

	for _, s := range plan.Skipped {
		logrus.Warnf("Skipping feed list %s: %v", s.Path, s.Err)
	}

	if !cfg.JSON {
		fmt.Printf("Found %d %s, %d unique %s (%d %s)\n",
			len(plan.Sites), pluralize(len(plan.Sites), "feed list", "feed lists"),
			len(plan.URLs), pluralize(len(plan.URLs), "feed", "feeds"),
			plan.References, pluralize(plan.References, "reference", "references"))
	}
	if err := orchestrator.SynchronizeFeedConfigs(plan.FeedConfigs); err != nil {
		return err
	}
	if err := orchestrator.SynchronizeFeedParserConfigs(plan.ParserConfigs); err != nil {
		return err
	}

	results := orchestrator.FetchFromURLs(ctx, plan.URLs, opts)

	summary := fetcher.ProcessResults(results)
	summary.Mode = "dir"
	summary.Print(cfg)

	if len(plan.Skipped) > 0 {
		return sitegroup.ErrPartialFailure
	}
	return nil
}

func runDatabaseFetch(
	ctx context.Context, orchestrator *fetcher.Orchestrator,
	opts fetcher.FetchOptions, cfg *config.Config,
) error {
	results, err := orchestrator.FetchFromDatabase(ctx, opts)
	if err != nil {
		return err
	}

	// Process and display results
	summary := fetcher.ProcessResults(results)
	summary.Mode = "database"
	summary.Print(cfg)

	return nil
}
