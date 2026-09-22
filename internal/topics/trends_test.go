package topics

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/lineage"
	"github.com/lmorchard/feedspool-go/internal/trends"
)

// latestTrends loads the latest run and its trends the way `topics latest` will.
func latestTrends(t *testing.T, db *database.DB) (*database.TopicRun, []*database.Topic, map[int64]trends.Trend) {
	t.Helper()
	ctx := context.Background()
	run, err := db.GetLatestTopicRun(ctx)
	require.NoError(t, err)
	require.NotNil(t, run)
	topicList, err := db.GetTopicsForRun(ctx, run.ID)
	require.NoError(t, err)
	items, err := db.GetTopicItems(ctx, run.ID)
	require.NoError(t, err)
	got, err := LoadTrends(ctx, db, run, topicList, items)
	require.NoError(t, err)
	return run, topicList, got
}

func sumInts(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

func TestLoadTrendsAfterTwoRuns(t *testing.T) {
	db, ids := newTopicsTestDB(t)
	embed(t, db, ids[0:3], 0)
	p := NewPipeline(db, &fakeLabeler{})
	first := generate(t, p)
	require.Len(t, first, 1)
	firstRun, err := db.GetLatestTopicRun(context.Background())
	require.NoError(t, err)

	embed(t, db, ids[3:6], 1)
	generate(t, p)

	_, topicList, got := latestTrends(t, db)
	require.Len(t, topicList, 2)
	for _, topic := range topicList {
		tr, ok := got[topic.ID]
		require.True(t, ok, "every topic gets a trend")
		assert.Equal(t, trends.StatusNew, tr.Status, "both threads opened within 24h")
		assert.Equal(t, 1, tr.DistinctFeeds)
		assert.Equal(t, 3, sumInts(tr.Daily))

		if topic.ThreadID == first[0].ThreadID {
			assert.Equal(t, 0, tr.NewItems, "survivor: nothing new")
			assert.Equal(t, 0, tr.DroppedItems)
			assert.Equal(t, formatTime(firstRun.CreatedAt), formatTime(tr.ThreadFirstSeen))
		} else {
			assert.Equal(t, 3, tr.NewItems, "new thread: everything new")
			assert.Equal(t, 0, tr.DroppedItems)
		}
	}
}

func TestLoadTrendsDiffAgainstPreviousTopic(t *testing.T) {
	db, ids := newTopicsTestDB(t)
	embed(t, db, ids[0:3], 0)
	first := generate(t, NewPipeline(db, &fakeLabeler{}))
	require.Len(t, first, 1)

	// A later run whose topic keeps the thread but swaps item 1 for item 7.
	now := time.Now().UTC().Add(time.Hour)
	topic := &database.Topic{
		Label: first[0].Label, Score: 3, ThreadID: first[0].ThreadID,
		LabelSource: lineage.SourceInherited,
	}
	run := &database.TopicRun{
		CreatedAt: now, WindowStart: now.Add(-7 * 24 * time.Hour), WindowEnd: now,
		EmbedModelID: testEmbedModel, LLMModelID: (&fakeLabeler{}).ModelID(),
	}
	require.NoError(t, db.InsertTopicRun(context.Background(), run, []*database.Topic{topic},
		map[*database.Topic][]int64{topic: {ids[1], ids[2], ids[6]}}))

	_, _, got := latestTrends(t, db)
	tr := got[topic.ID]
	assert.Equal(t, 1, tr.NewItems)
	assert.Equal(t, 1, tr.DroppedItems)
}

func TestLoadTrendsToleratesPurgedItem(t *testing.T) {
	db, ids := newTopicsTestDB(t)
	embed(t, db, ids[0:3], 0)
	generate(t, NewPipeline(db, &fakeLabeler{}))

	ctx := context.Background()
	run, err := db.GetLatestTopicRun(ctx)
	require.NoError(t, err)
	topicList, err := db.GetTopicsForRun(ctx, run.ID)
	require.NoError(t, err)
	items, err := db.GetTopicItems(ctx, run.ID)
	require.NoError(t, err)

	// A real purge would cascade the topic_items row away; an ID with no item
	// row is the only way to reach the missing-item path.
	id := topicList[0].ID
	items[id] = append(items[id], 999999)

	got, err := LoadTrends(ctx, db, run, topicList, items)
	require.NoError(t, err)
	tr := got[id]
	assert.Equal(t, 3, sumInts(tr.Daily), "the missing item has no date to bucket")
	assert.Equal(t, 4, tr.NewItems, "but it still counts as a member for the diff")
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
