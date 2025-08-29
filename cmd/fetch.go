package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/fetcher"
	"github.com/spf13/cobra"
)

var (
	fetchTimeout  time.Duration
	fetchMaxItems int
	fetchForce    bool
)

var fetchCmd = &cobra.Command{
	Use:   "fetch [URL]",
	Short: "Fetch a single feed",
	Long:  `Fetches a single RSS/Atom feed and updates the database with its metadata and items.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runFetch,
}

func init() {
	fetchCmd.Flags().DurationVar(&fetchTimeout, "timeout", 30*time.Second, "Feed fetch timeout")
	fetchCmd.Flags().IntVar(&fetchMaxItems, "max-items", 100, "Maximum items to keep per feed")
	fetchCmd.Flags().BoolVar(&fetchForce, "force", false, "Ignore cache headers and fetch anyway")
	rootCmd.AddCommand(fetchCmd)
}

func runFetch(_ *cobra.Command, args []string) error {
	feedURL := args[0]
	cfg := GetConfig()

	if err := database.Connect(cfg.Database); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer database.Close()

	fetcher := fetcher.NewFetcher(fetchTimeout, fetchMaxItems, fetchForce)
	result := fetcher.FetchFeed(feedURL)

	if result.Error != nil {
		return fmt.Errorf("failed to fetch feed: %w", result.Error)
	}

	if cfg.JSON {
		output := map[string]interface{}{
			"url":    feedURL,
			"cached": result.Cached,
			"items":  result.ItemCount,
		}
		if result.Feed != nil {
			output["title"] = result.Feed.Title
			output["description"] = result.Feed.Description
		}
		jsonData, _ := json.Marshal(output)
		fmt.Println(string(jsonData))
	} else {
		if result.Cached {
			fmt.Printf("Feed not modified: %s\n", feedURL)
		} else if result.Feed != nil {
			fmt.Printf("Feed fetched successfully: %s\n", result.Feed.Title)
			fmt.Printf("  URL: %s\n", feedURL)
			fmt.Printf("  Items: %d\n", result.ItemCount)
		}
	}

	return nil
}
