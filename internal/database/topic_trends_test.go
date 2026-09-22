package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lmorchard/feedspool-go/internal/lineage"
)

// insertThreadedRun writes one run; topics maps a label to (thread ID, items).
// A zero thread ID opens a new thread. Returns the topics by label.
func insertThreadedRun(
	t *testing.T, db *DB, at time.Time, topics map[string]threadedTopic,
) map[string]*Topic {
	t.Helper()
	list := make([]*Topic, 0, len(topics))
	items := make(map[*Topic][]int64, len(topics))
	byLabel := make(map[string]*Topic, len(topics))
	for label, tt := range topics {
		topic := &Topic{Label: label, Score: float64(len(tt.items)), ThreadID: tt.thread}
		if tt.thread != 0 {
			topic.LabelSource = lineage.SourceInherited
		}
		list = append(list, topic)
		items[topic] = tt.items
		byLabel[label] = topic
	}
	require.NoError(t, db.InsertTopicRun(context.Background(), newTopicRun(at), list, items))
	return byLabel
}

type threadedTopic struct {
	thread int64
	items  []int64
}

func TestGetPreviousThreadItems(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	ids := make([]int64, 5)
	for i := range ids {
		ids[i] = seedItem(t, db, "item-"+string(rune('a'+i)), time.Now())
	}

	t0 := time.Now().UTC().Add(-3 * time.Hour)
	r0 := insertThreadedRun(t, db, t0, map[string]threadedTopic{
		"A": {items: []int64{ids[0], ids[1]}},
		"B": {items: []int64{ids[3]}},
	})
	threadA, threadB := r0["A"].ThreadID, r0["B"].ThreadID
	insertThreadedRun(t, db, t0.Add(time.Hour), map[string]threadedTopic{
		"A": {thread: threadA, items: []int64{ids[1], ids[0]}},
	})
	t2 := t0.Add(2 * time.Hour)
	insertThreadedRun(t, db, t2, map[string]threadedTopic{
		"A": {thread: threadA, items: []int64{ids[0], ids[1], ids[2]}},
		"B": {thread: threadB, items: []int64{ids[3], ids[4]}},
	})

	got, err := db.GetPreviousThreadItems(ctx, t2, []int64{threadA, threadB})
	require.NoError(t, err)
	assert.Equal(t, []int64{ids[0], ids[1]}, got[threadA], "A diffs against run 1, ascending")
	assert.Equal(t, []int64{ids[3]}, got[threadB], "B skipped run 1, so it diffs against run 0")

	got, err = db.GetPreviousThreadItems(ctx, t0, []int64{threadA, threadB})
	require.NoError(t, err)
	assert.Empty(t, got, "nothing precedes the first run")
}

func TestGetPreviousThreadItemsEmptyInput(t *testing.T) {
	db := setupTestDB(t)
	got, err := db.GetPreviousThreadItems(context.Background(), time.Now(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGetThreadFirstSeen(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	id := seedItem(t, db, "item-1", time.Now())

	t0 := time.Now().UTC().Add(-2 * time.Hour)
	r0 := insertThreadedRun(t, db, t0, map[string]threadedTopic{"A": {items: []int64{id}}})
	insertThreadedRun(t, db, t0.Add(time.Hour), map[string]threadedTopic{
		"A": {thread: r0["A"].ThreadID, items: []int64{id}},
	})

	got, err := db.GetThreadFirstSeen(ctx, []int64{r0["A"].ThreadID, 99999})
	require.NoError(t, err)
	require.Contains(t, got, r0["A"].ThreadID)
	assert.Equal(t, formatDatabaseTime(t0), formatDatabaseTime(got[r0["A"].ThreadID]))
	assert.NotContains(t, got, int64(99999))

	got, err = db.GetThreadFirstSeen(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}
