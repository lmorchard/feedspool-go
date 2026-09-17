package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/embed"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	embedLast      string
	embedSince     string
	embedUntil     string
	embedModel     string
	embedForce     bool
	embedDryRun    bool
	embedBatchSize int
)

var embedCmd = &cobra.Command{
	Use:   "embed",
	Short: "Compute vector embeddings for items in a time window",
	Long: `Embed items in a time window so they can be compared by meaning.

Items are selected by effective date -- published date, falling back to when
feedspool first saw the item -- the same basis 'items --since/--until' uses.
The default window is the last 24 hours.

Embeddings are derived from the same stripped text the search index uses, so
run 'feedspool reindex' first if this reports items skipped for missing text.
An item is re-embedded when its text changes or when feedspool's own embedding
code changes; otherwise a second run has nothing to do.

Vectors are stored per model, so two models can be compared over the same
items without re-embedding between runs. Pass --model to embed with a model
other than the configured default.

This command makes network calls to the configured provider (a local Ollama by
default), so it is deliberately separate from fetch. An interrupted run
resumes where it stopped rather than starting over.`,
	Example: `  feedspool embed --last 2d
  feedspool embed --last 2d --dry-run
  feedspool embed --last 2d --model qwen3-embedding:0.6b
  feedspool embed --since 2026-09-14T00:00:00Z --until 2026-09-16T00:00:00Z
  feedspool embed --last 1w --force`,
	Args: cobra.NoArgs,
	RunE: runEmbed,
}

func init() {
	// --last rather than --max-age: cmd/build.go documents that --max-age
	// already means opposite things on fetch and render, and a third meaning
	// would make that worse.
	embedCmd.Flags().StringVar(&embedLast, "last", "",
		"Embed items from this far back (e.g. 24h, 2d, 1w); defaults to config or 1w")
	embedCmd.Flags().StringVar(&embedSince, "since", "",
		"Embed items with an effective date at or after this time (RFC3339)")
	embedCmd.Flags().StringVar(&embedUntil, "until", "",
		"Embed items with an effective date at or before this time (RFC3339)")
	embedCmd.Flags().StringVar(&embedModel, "model", "",
		"Embedding model to use; defaults to embed.model from config")
	embedCmd.Flags().BoolVar(&embedForce, "force", false,
		"Re-embed every item in the window, not just missing or stale ones")
	embedCmd.Flags().BoolVar(&embedDryRun, "dry-run", false,
		"Report how much work there is and exit without calling the provider")
	embedCmd.Flags().IntVar(&embedBatchSize, "batch-size", 0,
		"Items per provider request; defaults to the model's own batch size")

	// An explicit binding is required, not decoration: root.go calls
	// viper.AutomaticEnv() with no prefix or key replacer, so a dotted key
	// like embed.api_key has no usable environment spelling on its own. Same
	// arrangement serve.api.token has with FEEDSPOOL_API_TOKEN.
	//
	// There is deliberately no --api-key flag: a token passed on the command
	// line ends up in ps output.
	_ = viper.BindEnv("embed.api_key", "FEEDSPOOL_EMBED_API_KEY")

	rootCmd.AddCommand(embedCmd)
}

// resolveEmbedWindow parses the window flags, rejecting the combination that
// would otherwise silently ignore one of them.
//
// Mirrors how render rejects --max-age together with --start/--end.
func resolveEmbedWindow(cmd *cobra.Command, cfg *config.Config) (since, until time.Time, err error) {
	if cmd.Flags().Changed("last") && (cmd.Flags().Changed("since") || cmd.Flags().Changed("until")) {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"cannot specify both --last and an explicit range (--since/--until)",
		)
	}

	var last string
	if cmd.Flags().Changed("last") {
		last = embedLast
	} else if !cmd.Flags().Changed("since") && !cmd.Flags().Changed("until") {
		last = cfg.Embed.Last
		if last == "" {
			last = config.DefaultEmbedLast
		}
	}

	return database.ParseTimeWindow(last, embedSince, embedUntil)
}

func runEmbed(cmd *cobra.Command, _ []string) error {
	cfg := GetConfig()

	since, until, err := resolveEmbedWindow(cmd, cfg)
	if err != nil {
		return err
	}

	model := embedModel
	if model == "" {
		model = cfg.Embed.Model
	}

	db, err := openDatabase(cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()

	outstanding, err := db.CountItemsToEmbed(model, since, until, embedForce)
	if err != nil {
		return err
	}
	missingText, err := db.CountItemsMissingText(since, until)
	if err != nil {
		return err
	}

	logrus.Infof("Window %s to %s (effective date)",
		since.Format(time.RFC3339), until.Format(time.RFC3339))
	logrus.Infof("Model %s: %d items to embed", model, outstanding)
	if missingText > 0 {
		logrus.Warnf("Skipping %d items with no derived text; run `feedspool reindex` to derive it",
			missingText)
	}

	if embedDryRun {
		logrus.Info("Dry run: no embeddings computed")
		return nil
	}
	if outstanding == 0 {
		logrus.Info("Nothing to do")
		return nil
	}

	// --batch-size means items per provider request, so it goes to the
	// provider. Zero leaves the model's own measured default in place.
	providerBatch := embedBatchSize
	if providerBatch <= 0 {
		providerBatch = cfg.Embed.BatchSize
	}

	provider := embed.NewOllamaProvider(embed.Config{
		BaseURL:   cfg.Embed.BaseURL,
		Model:     model,
		APIKey:    cfg.Embed.APIKey,
		BatchSize: providerBatch,
		NumCtx:    cfg.Embed.NumCtx,
		Timeout:   cfg.Timeout,
	}, httpclient.NewClient(&httpclient.Config{Timeout: cfg.Timeout}))

	started := time.Now()
	var embedded int64
	if err := db.EmbedItems(
		context.Background(), provider, since, until,
		embedForce, driverBatchFor(providerBatch),
		func(done, total int64) {
			embedded = done
			logrus.Infof("Embedded %d of %d items", done, total)
		},
	); err != nil {
		return err
	}

	logrus.Infof("Embedded %d items with %s in %s",
		embedded, model, time.Since(started).Round(time.Millisecond))
	return nil
}

// driverBatchFor picks how many items the backfill reads and commits per
// batch, which is a different knob from how many go in one provider request.
//
// It only has to be at least the provider's batch size: the driver hands
// Compute one batch at a time, so a driver batch smaller than the provider's
// would cap the provider below what it was asked for. Zero means "use the
// driver's own default", which is what an unset --batch-size gets.
func driverBatchFor(providerBatch int) int {
	if providerBatch > defaultDriverBatch {
		return providerBatch
	}
	return 0
}

// defaultDriverBatch mirrors database.defaultStagedBatchSize. Kept here rather
// than exported from that package because it only matters for this comparison.
const defaultDriverBatch = 64
