package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/lmorchard/feedspool-go/internal/topics"
)

var (
	topicsLast        string
	topicsSince       string
	topicsUntil       string
	topicsModel       string
	topicsLLM         string
	topicsThresh      float32
	topicsMin         int
	topicsConcurrency int
)

var topicsCmd = &cobra.Command{
	Use:   "topics",
	Short: "Cluster embedded items into trending topics",
	RunE:  runTopics,
}

//nolint:cyclop
func runTopics(_ *cobra.Command, _ []string) error {
	cfg := GetConfig()

	if topicsLast != "" && (topicsSince != "" || topicsUntil != "") {
		return fmt.Errorf("cannot specify both --last and an explicit range (--since/--until)")
	}
	start, end, err := database.ParseTimeWindow(topicsLast, topicsSince, topicsUntil)
	if err != nil {
		return fmt.Errorf("error parsing time window: %w", err)
	}

	model := topicsModel
	if model == "" {
		model = cfg.Embed.Model
	}
	if model == "" {
		return fmt.Errorf("error: missing embed model config")
	}

	topicsCfg := cfg.Topics
	if topicsLLM != "" {
		topicsCfg.Model = topicsLLM
	} else if topicsCfg.Model == "" {
		return fmt.Errorf("error: missing topics model config")
	}

	client := httpclient.NewClient(&httpclient.Config{
		UserAgent: "feedspool-topics",
		Timeout:   120 * time.Second, //nolint:mnd // Generation can be slow
	})

	var labeler topics.Labeler
	if topicsCfg.BaseURL != "" && (strings.Contains(topicsCfg.BaseURL, "/v1") ||
		strings.Contains(topicsCfg.BaseURL, "openai")) {
		// Heuristic to use OpenAI format if the URL looks like an OpenAI-compatible endpoint
		labeler = topics.NewOpenAILabeler(topicsCfg, client)
	} else {
		labeler = topics.NewOllamaLabeler(topicsCfg, client)
	}

	db, err := openDatabase(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	pipeline := topics.NewPipeline(db, labeler)

	concurrency := topicsConcurrency
	if concurrency <= 0 {
		concurrency = cfg.Topics.Concurrency
	}

	run, results, itemsMap, err := pipeline.Generate(
		context.Background(), model, start, end, topicsThresh, topicsMin, concurrency,
	)
	if err != nil {
		return fmt.Errorf("error generating topics: %w", err)
	}

	if run == nil {
		if cfg.JSON {
			fmt.Println("[]")
		} else {
			fmt.Println("No items embedded in this time window.")
		}
		return nil
	}

	if cfg.JSON {
		printTopicsJSON(results, itemsMap)
		return nil
	}

	printTopicsCLI(start, end, model, labeler.ModelID(), results, itemsMap)
	return nil
}

func printTopicsJSON(results []*database.Topic, itemsMap map[*database.Topic][]int64) {
	output := make([]map[string]any, len(results))
	for i, t := range results {
		output[i] = map[string]any{
			"label": t.Label,
			"score": t.Score,
			"count": len(itemsMap[t]),
		}
	}
	jsonOut, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(jsonOut))
}

func printTopicsCLI(
	start, end time.Time, model, llmModel string, results []*database.Topic, itemsMap map[*database.Topic][]int64,
) {
	fmt.Printf("Generated topics for window %s to %s using %s and %s\n\n",
		start.Format("2006-01-02"), end.Format("2006-01-02"), model, llmModel)
	for _, t := range results {
		if t.Score < 2.0 { //nolint:mnd // Skip small topics
			continue
		}
		fmt.Printf("■ %s (score: %.1f, items: %d)\n", t.Label, t.Score, len(itemsMap[t]))
	}
}

func init() {
	rootCmd.AddCommand(topicsCmd)

	topicsCmd.Flags().StringVar(&topicsLast, "last", "1d", "Window duration (e.g. 2d, 1w)")
	topicsCmd.Flags().StringVar(&topicsSince, "since", "", "Start of window (RFC3339)")
	topicsCmd.Flags().StringVar(&topicsUntil, "until", "", "End of window (RFC3339)")
	topicsCmd.Flags().StringVar(&topicsModel, "model", "", "Embedding model (defaults to config)")
	topicsCmd.Flags().StringVar(&topicsLLM, "llm-model", "", "LLM model (defaults to config)")
	topicsCmd.Flags().Float32Var(&topicsThresh, "threshold", 0.70, //nolint:mnd
		"Cosine similarity threshold for clustering")
	topicsCmd.Flags().IntVar(&topicsMin, "min-items", 5, //nolint:mnd
		"Minimum items required in a cluster to keep it and label it")
	topicsCmd.Flags().IntVar(&topicsConcurrency, "concurrency", 0,
		"Concurrent LLM requests to make when generating labels (defaults to config)")

	topicsCmd.MarkFlagsMutuallyExclusive("last", "since")
	topicsCmd.MarkFlagsMutuallyExclusive("last", "until")
}
