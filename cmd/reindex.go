package cmd

import (
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/sirupsen/logrus"
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
		logrus.Info("Discarding every derived row before rebuilding")
	}
	if err := db.ReindexItemText(reindexForce, database.ItemTextProgressLogger()); err != nil {
		return err
	}

	logrus.Info("Search index is up to date")
	return nil
}
