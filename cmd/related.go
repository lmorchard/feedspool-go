package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/spf13/cobra"
)

var (
	relatedFormat string
	relatedFeed   string
	relatedGUID   string
	relatedModel  string
	relatedLimit  int
)

const defaultRelatedLimit = 10

var relatedCmd = &cobra.Command{
	Use:   "related [link]",
	Short: "Find items most similar to one item",
	Long: `Rank items by how close their embeddings are to one item's.

The subject is selected by link, the same way 'feedspool item' selects one --
use --feed and --guid together when a link matches more than one item.

Only items that have been embedded with the same model are candidates, so run
'feedspool embed' over the window you care about first. Similarity runs from
1.0 (identical direction) through 0 (unrelated) to -1.0 (opposed).`,
	Example: `  feedspool related https://example.com/posts/one
  feedspool related https://example.com/posts/one --limit 20
  feedspool related https://example.com/posts/one --model qwen3-embedding:0.6b
  feedspool --json related https://example.com/posts/one
  feedspool related --feed https://example.com/feed.xml --guid post-one`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return runRelated(args)
	},
}

func init() {
	relatedCmd.Flags().StringVar(&relatedFormat, "format", formatTable, "Output format (table|json)")
	relatedCmd.Flags().StringVar(&relatedFeed, "feed", "", "Select the subject by exact feed URL (requires --guid)")
	relatedCmd.Flags().StringVar(&relatedGUID, "guid", "", "Select the subject by exact item GUID (requires --feed)")
	relatedCmd.Flags().StringVar(&relatedModel, "model", "",
		"Embedding model to compare with; defaults to embed.model from config")
	relatedCmd.Flags().IntVar(&relatedLimit, "limit", defaultRelatedLimit, "Maximum neighbors to return")
	rootCmd.AddCommand(relatedCmd)
}

// RelatedOutput is the JSON shape of a related query.
type RelatedOutput struct {
	Subject   *database.Item      `json:"subject"`
	Model     string              `json:"model"`
	Neighbors []database.Neighbor `json:"neighbors"`
}

func runRelated(args []string) error {
	// Shares parseItemSelector and resolveItem with `item`, so the two cannot
	// drift on what an ambiguous link means or how it is reported.
	selector, err := parseItemSelector(args, relatedFeed, relatedGUID)
	if err != nil {
		return err
	}

	cfg := GetConfig()
	model := relatedModel
	if model == "" {
		model = cfg.Embed.Model
	}

	db, err := openDatabase(cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()

	subject, err := resolveItem(db, selector)
	if err != nil {
		return err
	}

	neighbors, err := db.NearestItems(model, subject.ID, relatedLimit)
	if err != nil {
		// "Not embedded yet" is the normal state of anything outside a window
		// the user has run embed over, so say what to do about it rather than
		// reporting a bare failure.
		if database.IsNoEmbedding(err) {
			return fmt.Errorf(
				"item %q has no %s embedding yet; run `feedspool embed` over a window containing it",
				subject.Link, model,
			)
		}
		return err
	}

	format := relatedFormat
	if format == formatTable && cfg.JSON {
		format = formatJSON
	}
	return outputRelated(format, &RelatedOutput{
		Subject:   subject,
		Model:     model,
		Neighbors: neighbors,
	})
}

func outputRelated(format string, output *RelatedOutput) error {
	switch format {
	case formatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	case formatTable:
		return outputRelatedTable(output)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func outputRelatedTable(output *RelatedOutput) error {
	fmt.Printf("Subject: %s\n", output.Subject.Title)
	fmt.Printf("Link:    %s\n", output.Subject.Link)
	fmt.Printf("Model:   %s\n\n", output.Model)

	if len(output.Neighbors) == 0 {
		fmt.Println("No other items are embedded with this model yet.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SIMILARITY\tDATE\tTITLE\tLINK")
	for _, neighbor := range output.Neighbors {
		fmt.Fprintf(w, "%.4f\t%s\t%s\t%s\n",
			neighbor.Similarity,
			neighbor.Item.EffectiveDate().Format(time.DateOnly),
			neighbor.Item.Title,
			neighbor.Item.Link)
	}
	return w.Flush()
}
