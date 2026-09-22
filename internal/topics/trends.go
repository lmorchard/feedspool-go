package topics

import (
	"context"
	"fmt"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/trends"
)

// LoadTrends computes a trends.Trend for every topic in a run, keyed by topic
// ID. topicItems is keyed by topic ID, as database.GetTopicItems returns it.
//
// Topics with no thread (rows from before migration 14) get a trend with no
// previous topic and a zero first-seen time, which Compute reports as not new.
// Items with no row (purged) contribute no date or feed, but still count as
// members for the diff, so a purge does not look like churn.
func LoadTrends(
	ctx context.Context, db *database.DB, run *database.TopicRun,
	topicList []*database.Topic, topicItems map[int64][]int64,
) (map[int64]trends.Trend, error) {
	var threadIDs, itemIDs []int64
	seenThread := make(map[int64]bool)
	for _, t := range topicList {
		if t.ThreadID != 0 && !seenThread[t.ThreadID] {
			seenThread[t.ThreadID] = true
			threadIDs = append(threadIDs, t.ThreadID)
		}
		itemIDs = append(itemIDs, topicItems[t.ID]...)
	}

	prev, err := db.GetPreviousThreadItems(ctx, run.CreatedAt, threadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load previous thread topics: %w", err)
	}
	firstSeen, err := db.GetThreadFirstSeen(ctx, threadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load thread first-seen times: %w", err)
	}
	items, err := db.GetItemsByIDs(itemIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load topic items: %w", err)
	}

	out := make(map[int64]trends.Trend, len(topicList))
	for _, t := range topicList {
		ids := topicItems[t.ID]
		in := &trends.Input{
			WindowStart: run.WindowStart,
			WindowEnd:   run.WindowEnd,
			ItemIDs:     ids,
		}
		for _, id := range ids {
			if item, ok := items[id]; ok {
				in.Items = append(in.Items, trends.Item{FeedURL: item.FeedURL, At: item.EffectiveDate()})
			}
		}
		if t.ThreadID != 0 {
			in.PrevItemIDs, in.HasPrev = prev[t.ThreadID]
			in.ThreadFirstSeen = firstSeen[t.ThreadID]
		}
		out[t.ID] = trends.Compute(in)
	}
	return out, nil
}
