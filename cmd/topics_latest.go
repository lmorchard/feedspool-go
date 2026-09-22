package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/topics"
	"github.com/lmorchard/feedspool-go/internal/trends"
)

var topicsLatestCmd = &cobra.Command{
	Use:   "latest",
	Short: "Show the most recent topic run with trend signals (no generation)",
	Long: `Show the most recent topic run with trend signals for each topic.

Read-only: no clustering, no LLM calls, no new topic run. Trends are computed
from the stored run, its threads, and item dates -- see MANUAL.md for the
definitions of each column.`,
	Args: cobra.NoArgs,
	RunE: runTopicsLatest,
}

type latestRunJSON struct {
	ID          int64     `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	EmbedModel  string    `json:"embed_model"`
	LLMModel    string    `json:"llm_model"`
}

type latestTopicJSON struct {
	Label       string        `json:"label"`
	Score       float64       `json:"score"`
	Count       int           `json:"count"`
	ThreadID    int64         `json:"thread_id"`
	SetHash     string        `json:"set_hash"`
	LabelSource string        `json:"label_source"`
	Trend       *trends.Trend `json:"trend,omitempty"`
}

type latestJSON struct {
	Run    latestRunJSON     `json:"run"`
	Topics []latestTopicJSON `json:"topics"`
}

func runTopicsLatest(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()
	ctx := context.Background()

	db, err := openDatabase(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	run, err := db.GetLatestTopicRun(ctx)
	if err != nil {
		return fmt.Errorf("failed to load latest topic run: %w", err)
	}
	if run == nil {
		if cfg.JSON {
			fmt.Println("null")
		} else {
			fmt.Println("No topic runs yet. Run `feedspool topics` first.")
		}
		return nil
	}

	topicList, err := db.GetTopicsForRun(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("failed to load topics: %w", err)
	}
	items, err := db.GetTopicItems(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("failed to load topic items: %w", err)
	}
	topicTrends, err := topics.LoadTrends(ctx, db, run, topicList, items)
	if err != nil {
		return fmt.Errorf("failed to compute trends: %w", err)
	}

	if cfg.JSON {
		return printLatestJSON(run, topicList, items, topicTrends)
	}
	return printLatestTable(run, topicList, items, topicTrends)
}

func printLatestJSON(
	run *database.TopicRun, topicList []*database.Topic,
	items map[int64][]int64, topicTrends map[int64]trends.Trend,
) error {
	out := latestJSON{
		Run: latestRunJSON{
			ID: run.ID, CreatedAt: run.CreatedAt, WindowStart: run.WindowStart, WindowEnd: run.WindowEnd,
			EmbedModel: run.EmbedModelID, LLMModel: run.LLMModelID,
		},
		Topics: make([]latestTopicJSON, 0, len(topicList)),
	}
	for _, t := range topicList {
		entry := latestTopicJSON{
			Label: t.Label, Score: t.Score, Count: len(items[t.ID]),
			ThreadID: t.ThreadID, SetHash: t.SetHash, LabelSource: t.LabelSource,
		}
		if tr, ok := topicTrends[t.ID]; ok {
			entry.Trend = &tr
		}
		out.Topics = append(out.Topics, entry)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func printLatestTable(
	run *database.TopicRun, topicList []*database.Topic,
	items map[int64][]int64, topicTrends map[int64]trends.Trend,
) error {
	fmt.Printf("Topic run %d, computed %s, window %s to %s\n\n", run.ID,
		run.CreatedAt.Format("2006-01-02 15:04 UTC"),
		run.WindowStart.Format("2006-01-02 15:04"), run.WindowEnd.Format("2006-01-02 15:04"))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tITEMS\tFEEDS\t24H\tPREV24H\t+NEW\t-GONE\tDAILY\tLABEL")
	for _, t := range topicList {
		tr := topicTrends[t.ID]
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n",
			tr.Status, len(items[t.ID]), tr.DistinctFeeds, tr.Last24h, tr.Prior24h,
			tr.NewItems, tr.DroppedItems, trends.Sparkline(tr.Daily), t.Label)
	}
	return w.Flush()
}

func init() {
	topicsCmd.AddCommand(topicsLatestCmd)
}
