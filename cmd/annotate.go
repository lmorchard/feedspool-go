package cmd

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var annotateCmd = &cobra.Command{
	Use:   "annotate <link> <kind> [value]",
	Short: "Add an annotation to an item",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(_ *cobra.Command, args []string) error {
		link := args[0]
		kind := args[1]
		var value string
		if len(args) > 2 {
			value = args[2]
		}
		return annotateItem(link, kind, value, true)
	},
}

var unannotateCmd = &cobra.Command{
	Use:   "unannotate <link> <kind> [value]",
	Short: "Remove an annotation from an item",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(_ *cobra.Command, args []string) error {
		link := args[0]
		kind := args[1]
		var value string
		if len(args) > 2 {
			value = args[2]
		}
		return annotateItem(link, kind, value, false)
	},
}

func init() {
	rootCmd.AddCommand(annotateCmd)
	rootCmd.AddCommand(unannotateCmd)
}

func annotateItem(link, kind, value string, add bool) error {
	cfg := GetConfig()
	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	if err := db.IsInitialized(); err != nil {
		return err
	}

	var feedURL, guid string
	err = db.GetConnection().QueryRow(queryFindItemByLink, link).Scan(&feedURL, &guid)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("item with link %q not found", link)
	} else if err != nil {
		return fmt.Errorf("failed to look up item: %w", err)
	}

	var nullVal sql.NullString
	if value != "" {
		nullVal = sql.NullString{String: value, Valid: true}
	}

	if add {
		if err := db.AddAnnotation(feedURL, guid, kind, nullVal, sql.NullString{}); err != nil {
			return err
		}
		//nolint:forbidigo
		fmt.Printf("Added annotation '%s' to item: %s\n", kind, link)
	} else {
		if err := db.RemoveAnnotation(feedURL, guid, kind, nullVal); err != nil {
			return err
		}
		//nolint:forbidigo
		fmt.Printf("Removed annotation '%s' from item: %s\n", kind, link)
	}

	return nil
}
