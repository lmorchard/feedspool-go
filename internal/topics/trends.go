package topics

import (
	"context"
	"fmt"
	"time"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/trends"
)

// TrendOptions tunes LoadTrends. The zero value means every feed and the
// default growth margin.
type TrendOptions struct {
	// AllowedFeeds restricts both sides of the diff to items from these feeds:
	// a per-site page must not count another site's items as present now or
	// as dropped since the last run. nil means no restriction.
	AllowedFeeds map[string]bool
	// GrowthMargin overrides trends.DefaultGrowthMargin when positive.
	GrowthMargin int
}

// LoadTrends computes a trends.Trend for every topic in a run, keyed by topic
// ID. topicItems is keyed by topic ID, as database.GetTopicItems returns it.
//
// Topics with no thread (rows from before migration 14) get a trend with no
// previous topic and a zero first-seen time, which Compute reports as not new.
// Without a feed restriction, members with no item row (purged) still count
// for the diff, so a purge does not look like churn; with one they are
// ignored, since their feed cannot be known.
func LoadTrends(
	ctx context.Context, db *database.DB, run *database.TopicRun,
	topicList []*database.Topic, topicItems map[int64][]int64, opts TrendOptions,
) (map[int64]trends.Trend, error) {
	allowedFeeds := opts.AllowedFeeds
	prev, firstSeen, err := loadThreadHistory(ctx, db, run, topicList)
	if err != nil {
		return nil, err
	}

	var itemIDs []int64
	for _, t := range topicList {
		itemIDs = append(itemIDs, topicItems[t.ID]...)
	}
	for _, ids := range prev {
		itemIDs = append(itemIDs, ids...)
	}
	items, err := db.GetItemsByIDs(itemIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load topic items: %w", err)
	}

	keep := func(ids []int64) []int64 {
		if allowedFeeds == nil {
			return ids
		}
		var out []int64
		for _, id := range ids {
			if item, ok := items[id]; ok && allowedFeeds[item.FeedURL] {
				out = append(out, id)
			}
		}
		return out
	}

	out := make(map[int64]trends.Trend, len(topicList))
	for _, t := range topicList {
		ids := keep(topicItems[t.ID])
		in := &trends.Input{
			WindowStart: run.WindowStart, WindowEnd: run.WindowEnd, ItemIDs: ids,
			GrowthMargin: opts.GrowthMargin,
		}
		for _, id := range ids {
			if item, ok := items[id]; ok {
				in.Items = append(in.Items, trends.Item{FeedURL: item.FeedURL, At: item.EffectiveDate()})
			}
		}
		if t.ThreadID != 0 {
			var p []int64
			p, in.HasPrev = prev[t.ThreadID]
			in.PrevItemIDs = keep(p)
			in.ThreadFirstSeen = firstSeen[t.ThreadID]
		}
		out[t.ID] = trends.Compute(in)
	}
	return out, nil
}

// loadThreadHistory returns, for the run's threads, each thread's previous
// members and its first-seen time.
func loadThreadHistory(
	ctx context.Context, db *database.DB, run *database.TopicRun, topicList []*database.Topic,
) (prev map[int64][]int64, firstSeen map[int64]time.Time, err error) {
	var threadIDs []int64
	seen := make(map[int64]bool)
	for _, t := range topicList {
		if t.ThreadID != 0 && !seen[t.ThreadID] {
			seen[t.ThreadID] = true
			threadIDs = append(threadIDs, t.ThreadID)
		}
	}
	if prev, err = db.GetPreviousThreadItems(ctx, run.CreatedAt, threadIDs); err != nil {
		return nil, nil, fmt.Errorf("failed to load previous thread topics: %w", err)
	}
	if firstSeen, err = db.GetThreadFirstSeen(ctx, threadIDs); err != nil {
		return nil, nil, fmt.Errorf("failed to load thread first-seen times: %w", err)
	}
	return prev, firstSeen, nil
}
