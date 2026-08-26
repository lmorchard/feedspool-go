package cmd

import (
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var reindexForce bool

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild the full-text search index",
	Long: `Derive search text for any items that lack it and bring the full-text
search index up to date. Ordinary use needs no flags: fetching maintains the
index, and this command only fills in what is missing.

Use --force after changing how text is derived or tokenized. It discards every
derived row and rebuilds from the items themselves, which takes as long as the
original migration did.`,
	Example: `  feedspool reindex
  feedspool reindex --force`,
	Args: cobra.NoArgs,
	RunE: runReindex,
}

func init() {
	reindexCmd.Flags().BoolVar(&reindexForce, "force", false,
		"Discard and rebuild every derived row, e.g. after a tokenizer change")
	rootCmd.AddCommand(reindexCmd)
}

func runReindex(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()
	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()
	if err := db.IsInitialized(); err != nil {
		return err
	}

	if reindexForce {
		fmt.Println("Discarding all derived text before rebuilding...")
	}
	fmt.Println("Updating full-text search index...")

	// Progress goes to stdout rather than logrus: a rebuild of a large spool
	// runs for tens of seconds, and the default log level is Warn, so a command
	// reporting at info level would be indistinguishable from one doing nothing.
	var indexed int64
	if err := db.ReindexItemText(reindexForce, func(done, total int64) {
		indexed = done
		fmt.Printf("Indexed %d of %d outstanding items\n", done, total)
	}); err != nil {
		return err
	}

	fmt.Printf("Search index is up to date (%d items indexed)\n", indexed)
	return nil
}
