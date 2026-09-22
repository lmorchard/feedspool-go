package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/lmorchard/feedspool-go/internal/config"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/httpclient"
	"github.com/lmorchard/feedspool-go/internal/lineage"
	"github.com/lmorchard/feedspool-go/internal/topics"
	"github.com/lmorchard/feedspool-go/internal/trends"
)

var (
	topicsLast        string
	topicsSince       string
	topicsUntil       string
	topicsModel       string
	topicsLLM         string
	topicsThresh      float32
	topicsMin         int
	topicsMax         int
	topicsConcurrency int
	topicsNoInherit   bool
)

var topicsCmd = &cobra.Command{
	Use:   "topics",
	Short: "Cluster embedded items into trending topics",
	RunE:  runTopics,
}

func runTopics(cmd *cobra.Command, _ []string) error {
	cfg := GetConfig()

	params, err := resolveTopicsParams(cmd, cfg)
	if err != nil {
		return err
	}

	topicsCfg := cfg.Topics
	topicsCfg.Model = params.llmModel

	client := httpclient.NewClient(&httpclient.Config{
		UserAgent: "feedspool-topics",
		Timeout:   120 * time.Second, //nolint:mnd // Generation can be slow
	})

	var labeler topics.Labeler
	if topicsCfg.BaseURL != "" && (strings.Contains(topicsCfg.BaseURL, "/v1") ||
		strings.Contains(topicsCfg.BaseURL, "openai")) {
		// Heuristic to use OpenAI format if the URL looks like an OpenAI-compatible endpoint
		labeler = topics.NewOpenAILabeler(&topicsCfg, client)
	} else {
		labeler = topics.NewOllamaLabeler(&topicsCfg, client)
	}

	db, err := openDatabase(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	pipeline := topics.NewPipeline(db, labeler)
	pipeline.Lookback = cfg.Topics.LineageLookback
	pipeline.Lineage = lineage.Options{
		AttachThreshold:  cfg.Topics.LineageThreshold,
		InheritThreshold: cfg.Topics.InheritThreshold,
		Inherit:          !topicsNoInherit,
	}

	run, results, itemsMap, err := pipeline.Generate(
		context.Background(), params.embedModel, params.start, params.end,
		params.thresh, params.minItems, params.maxItems, params.concurrency,
		cfg.Topics.MaxFeedRatio, cfg.Topics.MinDiversityCount,
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
		printTopicsJSON(results, itemsMap, generatedTrends(db, run, results, itemsMap))
		return nil
	}

	printTopicsCLI(params.start, params.end, params.embedModel, labeler.ModelID(), results, itemsMap)
	return nil
}

type topicsParams struct {
	start       time.Time
	end         time.Time
	embedModel  string
	llmModel    string
	thresh      float32
	minItems    int
	maxItems    int
	concurrency int
}

func resolveOption[T comparable](cmd *cobra.Command, flagName string, flagVal, configVal, defaultVal T) T {
	var zero T
	if cmd.Flags().Changed(flagName) {
		return flagVal
	}
	if configVal != zero {
		return configVal
	}
	return defaultVal
}

func resolveTopicsParams(cmd *cobra.Command, cfg *config.Config) (*topicsParams, error) {
	if cmd.Flags().Changed("last") && (cmd.Flags().Changed("since") || cmd.Flags().Changed("until")) {
		return nil, fmt.Errorf("cannot specify both --last and an explicit range (--since/--until)")
	}

	var last string
	if cmd.Flags().Changed("last") {
		last = topicsLast
	} else if !cmd.Flags().Changed("since") && !cmd.Flags().Changed("until") {
		last = cfg.Topics.Last
		if last == "" {
			last = config.DefaultTopicsLast
		}
	}

	start, end, err := database.ParseTimeWindow(last, topicsSince, topicsUntil)
	if err != nil {
		return nil, fmt.Errorf("error parsing time window: %w", err)
	}

	model := topicsModel
	if !cmd.Flags().Changed("model") {
		if cfg.Topics.EmbedModel != "" {
			model = cfg.Topics.EmbedModel
		} else {
			model = cfg.Embed.Model
		}
	}
	if model == "" {
		return nil, fmt.Errorf("error: missing embed model config")
	}

	llmModel := resolveOption(cmd, "llm-model", topicsLLM, cfg.Topics.Model, "")
	if llmModel == "" {
		return nil, fmt.Errorf("error: missing topics model config")
	}

	return &topicsParams{
		start:      start,
		end:        end,
		embedModel: model,
		llmModel:   llmModel,
		thresh: resolveOption(cmd, "threshold", topicsThresh,
			cfg.Topics.Threshold, config.DefaultTopicsThreshold),
		minItems: resolveOption(cmd, "min-items", topicsMin,
			cfg.Topics.MinItems, config.DefaultTopicsMinItems),
		maxItems: resolveOption(cmd, "max-items", topicsMax,
			cfg.Topics.MaxItems, config.DefaultTopicsMaxItems),
		concurrency: resolveOption(cmd, "concurrency", topicsConcurrency,
			cfg.Topics.Concurrency, config.DefaultTopicsConcurrency),
	}, nil
}

// generatedTrends computes trends for a run that was just generated. A failure
// here is logged and the trend omitted: the run itself already succeeded.
func generatedTrends(
	db *database.DB, run *database.TopicRun, results []*database.Topic, itemsMap map[*database.Topic][]int64,
) map[int64]trends.Trend {
	byID := make(map[int64][]int64, len(itemsMap))
	for t, ids := range itemsMap {
		byID[t.ID] = ids
	}
	tr, err := topics.LoadTrends(context.Background(), db, run, results, byID)
	if err != nil {
		logrus.WithError(err).Warn("Could not compute topic trends; omitting them from output")
		return nil
	}
	return tr
}

func printTopicsJSON(
	results []*database.Topic, itemsMap map[*database.Topic][]int64, topicTrends map[int64]trends.Trend,
) {
	output := make([]map[string]any, len(results))
	for i, t := range results {
		transition := lineage.TransitionSurvived
		if t.ThreadIsNew {
			transition = lineage.TransitionNew
		}
		output[i] = map[string]any{
			"label":        t.Label,
			"score":        t.Score,
			"count":        len(itemsMap[t]),
			"thread_id":    t.ThreadID,
			"set_hash":     t.SetHash,
			"label_source": t.LabelSource,
			"transition":   transition,
		}
		if tr, ok := topicTrends[t.ID]; ok {
			output[i]["trend"] = tr
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

	topicsCmd.Flags().StringVar(&topicsLast, "last", "", "Window duration (e.g. 2d, 1w); defaults to config")
	topicsCmd.Flags().StringVar(&topicsSince, "since", "", "Start of window (RFC3339)")
	topicsCmd.Flags().StringVar(&topicsUntil, "until", "", "End of window (RFC3339)")
	topicsCmd.Flags().StringVar(&topicsModel, "model", "", "Embedding model (defaults to config)")
	topicsCmd.Flags().StringVar(&topicsLLM, "llm-model", "", "LLM model (defaults to config)")
	topicsCmd.Flags().Float32Var(&topicsThresh, "threshold", 0,
		"Cosine similarity threshold for clustering (0 = use config default)")
	topicsCmd.Flags().IntVar(&topicsMin, "min-items", 0,
		"Minimum items required in a cluster to keep it and label it (0 = use config default)")
	topicsCmd.Flags().IntVar(&topicsMax, "max-items", 0,
		"Maximum items allowed in a cluster before dropping it (0 = no maximum limit)")
	topicsCmd.Flags().IntVar(&topicsConcurrency, "concurrency", 0,
		"Concurrent LLM requests to make when generating labels (defaults to config)")
	topicsCmd.Flags().BoolVar(&topicsNoInherit, "no-inherit", false,
		"Label every cluster fresh instead of reusing the label of an unchanged topic")

	topicsCmd.MarkFlagsMutuallyExclusive("last", "since")
	topicsCmd.MarkFlagsMutuallyExclusive("last", "until")
}
