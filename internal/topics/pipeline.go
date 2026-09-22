package topics

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/lmorchard/feedspool-go/internal/clustering"
	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/lineage"
)

// Pipeline orchestrates the topic generation process.
type Pipeline struct {
	db      *database.DB
	labeler Labeler

	// Lineage controls how clusters are matched to threads from earlier runs
	// and when a matched cluster keeps the thread's label instead of asking
	// the LLM again. Lookback is how many previous runs are candidates.
	// NewPipeline sets the measured defaults; cmd/topics overrides from config.
	Lineage  lineage.Options
	Lookback int
}

func NewPipeline(db *database.DB, labeler Labeler) *Pipeline {
	return &Pipeline{
		db:      db,
		labeler: labeler,
		Lineage: lineage.Options{
			AttachThreshold:  lineage.DefaultAttachThreshold,
			InheritThreshold: lineage.DefaultInheritThreshold,
			Inherit:          true,
		},
		Lookback: lineage.DefaultLookback,
	}
}

// Generate finds clusters of embedded items and labels them.
// Output topics are sorted by descending score (size).
func (p *Pipeline) Generate(
	ctx context.Context, embedModel string, since, until time.Time,
	threshold float32, minItems, maxItems, concurrency int,
	maxFeedRatio float32, minDiversityCount int,
) (*database.TopicRun, []*database.Topic, map[*database.Topic][]int64, error) {
	logrus.Infof("Fetching embeddings for model %q...", embedModel)
	embeddings, err := p.db.GetEmbeddingsForWindow(ctx, embedModel, since, until)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to fetch embeddings: %w", err)
	}
	if len(embeddings) == 0 {
		logrus.Info("No embeddings found in the specified window.")
		return nil, nil, nil, nil
	}

	logrus.Infof("Clustering %d items with threshold %.2f (this may take a moment)...", len(embeddings), threshold)
	clusters, err := clustering.Cluster(embeddings, threshold)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("clustering failed: %w", err)
	}

	validClusters := filterClusters(clusters, minItems, maxItems)

	if maxFeedRatio > 0 {
		validClusters, err = p.filterByDiversity(validClusters, maxFeedRatio, minDiversityCount)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// Fetch item details for labeling
	var allItemIDs []int64
	for _, cluster := range validClusters {
		allItemIDs = append(allItemIDs, cluster...)
	}

	itemsMap, err := p.db.GetItemTextsByIDs(allItemIDs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to fetch item texts: %w", err)
	}

	run := &database.TopicRun{
		CreatedAt:    time.Now().UTC(),
		WindowStart:  since,
		WindowEnd:    until,
		EmbedModelID: embedModel,
		LLMModelID:   p.labeler.ModelID(),
	}

	// Match clusters to threads from earlier runs before labeling, so a
	// cluster whose membership has not changed keeps its label and skips
	// the LLM. The run is not in the database yet, so "runs created before
	// run.CreatedAt" is exactly the previous runs.
	candidates, err := p.db.GetLineageCandidates(ctx, run.CreatedAt, p.Lookback)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load lineage candidates: %w", err)
	}
	assignments := lineage.Assign(validClusters, candidates, p.Lineage)

	topics, topicItemsMap, err := p.labelClusters(ctx, validClusters, assignments, itemsMap, concurrency)
	if err != nil {
		return nil, nil, nil, err
	}

	logrus.Infof("Saving %d topics to database...", len(topics))

	// Sort topics by score descending
	sort.Slice(topics, func(i, j int) bool {
		return topics[i].Score > topics[j].Score
	})

	if err := p.db.InsertTopicRun(ctx, run, topics, topicItemsMap); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to persist topic run: %w", err)
	}

	logrus.Info("Done generating topics.")
	return run, topics, topicItemsMap, nil
}

// labelClusters turns clusters into Topics, concurrently. A cluster whose
// assignment carries an inherited label takes it without an LLM call; the
// rest are labeled fresh. The first labeling error aborts the whole run.
func (p *Pipeline) labelClusters(
	ctx context.Context, clusters [][]int64, assignments []lineage.Assignment,
	itemsMap map[int64]*database.ItemText, concurrency int,
) ([]*database.Topic, map[*database.Topic][]int64, error) {
	type clusterResult struct {
		topic   *database.Topic
		cluster []int64
		err     error
	}

	if concurrency <= 0 {
		concurrency = 5
	}
	resultsCh := make(chan clusterResult, len(clusters))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	logrus.Infof("Labeling %d clusters (concurrency: %d)...", len(clusters), concurrency)

	for i, cluster := range clusters {
		wg.Add(1)
		go func(c []int64, a lineage.Assignment) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			topic := &database.Topic{
				Score:       float64(len(c)),
				ThreadID:    a.ThreadID,
				SetHash:     a.Hash,
				ThreadIsNew: a.ThreadID == 0,
			}
			var err error
			if a.Label != "" {
				topic.Label, topic.LabelSource = a.Label, lineage.SourceInherited
			} else {
				topic.Label, err = p.getLabelForCluster(ctx, c, itemsMap)
				topic.LabelSource = lineage.SourceGenerated
			}
			resultsCh <- clusterResult{topic: topic, cluster: c, err: err}
		}(cluster, assignments[i])
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var topics []*database.Topic
	topicItemsMap := make(map[*database.Topic][]int64)
	var inherited, generated, newThreads int
	for res := range resultsCh {
		if res.err != nil {
			return nil, nil, res.err
		}
		topics = append(topics, res.topic)
		topicItemsMap[res.topic] = res.cluster

		switch {
		case res.topic.ThreadIsNew:
			newThreads++
			generated++
		case res.topic.LabelSource == lineage.SourceInherited:
			inherited++
		default:
			generated++
		}
		if done := len(topics); done%10 == 0 || done == len(clusters) {
			logrus.Infof("Labeled %d of %d clusters", done, len(clusters))
		}
	}

	logrus.Infof("Labeled %d topics: %d inherited, %d generated, %d new threads",
		len(topics), inherited, generated, newThreads)
	return topics, topicItemsMap, nil
}

func (p *Pipeline) getLabelForCluster(
	ctx context.Context, cluster []int64, itemsMap map[int64]*database.ItemText,
) (string, error) {
	var titles []string
	var rawTitles []string
	for i, itemID := range cluster {
		if i >= 5 { //nolint:mnd // only use top 5 titles for LLM labeling to save context
			break
		}
		item, ok := itemsMap[itemID]
		if ok && item.Title != "" {
			rawTitles = append(rawTitles, item.Title)
			// truncate body to save context window
			body := item.Body
			if len(body) > 500 {
				body = body[:500] + "..."
			}
			snippet := fmt.Sprintf("Title: %s\nSnippet: %s", item.Title, body)
			titles = append(titles, snippet)
		}
	}

	if len(cluster) == 1 {
		if len(rawTitles) > 0 {
			return rawTitles[0], nil // Use the item's own title for singletons
		}
		return "Untitled Item", nil
	}

	label, err := p.labeler.LabelCluster(ctx, titles)
	if err != nil {
		return "", fmt.Errorf("failed to label cluster: %w", err)
	}
	return label, nil
}

func filterClusters(clusters [][]int64, minItems, maxItems int) [][]int64 {
	var validClusters [][]int64
	for _, c := range clusters {
		if len(c) >= minItems && (maxItems <= 0 || len(c) <= maxItems) {
			validClusters = append(validClusters, c)
		}
	}

	if maxItems > 0 {
		logrus.Infof("Formed %d clusters with %d-%d items. Fetching item details...", len(validClusters), minItems, maxItems)
	} else {
		logrus.Infof("Formed %d clusters with %d+ items. Fetching item details...", len(validClusters), minItems)
	}
	return validClusters
}

func (p *Pipeline) filterByDiversity(
	clusters [][]int64, maxFeedRatio float32, minDiversityCount int,
) ([][]int64, error) {
	var allIDs []int64
	for _, c := range clusters {
		allIDs = append(allIDs, c...)
	}
	if len(allIDs) == 0 {
		return clusters, nil
	}

	itemsMap, err := p.db.GetItemsByIDs(allIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch items for diversity filtering: %w", err)
	}

	var filtered [][]int64
	rejected := 0
	for _, c := range clusters {
		if len(c) < minDiversityCount {
			filtered = append(filtered, c)
			continue
		}
		feedCounts := make(map[string]int)
		for _, id := range c {
			if item, ok := itemsMap[id]; ok {
				feedCounts[item.FeedURL]++
			}
		}
		isSpam := false
		for _, count := range feedCounts {
			if float32(count)/float32(len(c)) >= maxFeedRatio {
				isSpam = true
				break
			}
		}
		if isSpam {
			rejected++
		} else {
			filtered = append(filtered, c)
		}
	}

	if rejected > 0 {
		logrus.Infof("Filtered out %d cluster(s) where a single feed controlled >=%.0f%% of items",
			rejected, maxFeedRatio*100)
	}
	return filtered, nil
}
