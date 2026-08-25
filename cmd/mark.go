package cmd

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var markSeenCmd = &cobra.Command{
	Use:   "mark-seen <link>",
	Short: "Mark an item as seen",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return markItemSeenStatus(args[0], true)
	},
}

var markUnseenCmd = &cobra.Command{
	Use:   "mark-unseen <link>",
	Short: "Mark an item as unseen",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return markItemSeenStatus(args[0], false)
	},
}

func init() {
	rootCmd.AddCommand(markSeenCmd)
	rootCmd.AddCommand(markUnseenCmd)
}

func markItemSeenStatus(link string, seen bool) error {
	cfg := GetConfig()
	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	// Find the item by link to get feed_url and guid
	var feedURL, guid string
	err = db.GetConnection().QueryRow(queryFindItemByLink, link).Scan(&feedURL, &guid)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("item with link %q not found", link)
	} else if err != nil {
		return fmt.Errorf("failed to look up item: %w", err)
	}

	if seen {
		if err := db.AddAnnotation(feedURL, guid, "seen", sql.NullString{}, sql.NullString{}); err != nil {
			return err
		}
		//nolint:forbidigo // User-facing output
		fmt.Printf("Marked item as seen: %s\n", link)
	} else {
		if err := db.RemoveAnnotation(feedURL, guid, "seen", sql.NullString{}); err != nil {
			return err
		}
		//nolint:forbidigo // User-facing output
		fmt.Printf("Marked item as unseen: %s\n", link)
	}

	return nil
}
