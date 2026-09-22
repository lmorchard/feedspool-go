package topics

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/lineage"
)

const (
	testFeedURL    = "https://example.com/feed"
	testEmbedModel = "fake-embed"
	testDims       = 4
)

// fakeLabeler counts calls and returns a distinct label per call, so a test
// can tell an inherited label from a freshly generated one.
type fakeLabeler struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeLabeler) LabelCluster(_ context.Context, _ []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return fmt.Sprintf("label %d", f.calls), nil
}

func (f *fakeLabeler) ModelID() string { return "fake-llm" }

func (f *fakeLabeler) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// newTopicsTestDB seeds one feed and nine items, returning their IDs in GUID
// order. No embeddings yet: each test embeds the items it wants clustered.
func newTopicsTestDB(t *testing.T) (db *database.DB, ids []int64) {
	t.Helper()
	db, err := database.New(filepath.Join(t.TempDir(), "topics_test.db"))
	require.NoError(t, err)
	require.NoError(t, db.InitSchema())
	t.Cleanup(func() { db.Close() })

	require.NoError(t, db.UpsertFeed(&database.Feed{URL: testFeedURL, Title: "Feed", FeedJSON: database.JSON(`{}`)}))

	now := time.Now().UTC()
	ids = make([]int64, 0, 9)
	for i := 1; i <= 9; i++ {
		guid := fmt.Sprintf("g%d", i)
		require.NoError(t, db.UpsertItem(&database.Item{
			FeedURL:       testFeedURL,
			GUID:          guid,
			Title:         "Item " + guid,
			Link:          "https://example.com/" + guid,
			PublishedDate: now,
			FirstSeen:     sql.NullTime{Time: now, Valid: true},
			Content:       "content " + guid,
			ItemJSON:      database.JSON(`{}`),
		}))
		item, err := db.GetItem(testFeedURL, guid)
		require.NoError(t, err)
		ids = append(ids, item.ID)
	}
	return db, ids
}

// oneHot returns a unit vector with a 1 in position hot: identical items
// score 1.0 against each other and 0 against every other position.
func oneHot(hot int) []float32 {
	v := make([]float32, testDims)
	v[hot] = 1
	return v
}

func embed(t *testing.T, db *database.DB, ids []int64, hot int) {
	t.Helper()
	for _, id := range ids {
		_, err := db.GetConnection().Exec(`
			INSERT INTO item_embeddings (item_id, model_id, dims, vector, source_hash, generator_version, computed_at)
			VALUES (?, ?, ?, ?, 'h', 1, ?)
			ON CONFLICT(item_id, model_id) DO NOTHING`,
			id, testEmbedModel, testDims, database.EncodeVector(oneHot(hot)), time.Now().UTC().Format(time.RFC3339Nano))
		require.NoError(t, err)
	}
}

func generate(t *testing.T, p *Pipeline) []*database.Topic {
	t.Helper()
	now := time.Now().UTC()
	// maxFeedRatio 0 turns the diversity filter off: every fixture item is in one feed.
	run, topics, _, err := p.Generate(context.Background(), testEmbedModel,
		now.Add(-time.Hour), now.Add(time.Hour), 0.7, 2, 0, 2, 0, 2)
	require.NoError(t, err)
	require.NotNil(t, run)
	return topics
}

func byHash(topics []*database.Topic) map[string]*database.Topic {
	out := make(map[string]*database.Topic, len(topics))
	for _, t := range topics {
		out[t.SetHash] = t
	}
	return out
}

func countThreads(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.GetConnection().QueryRow(`SELECT COUNT(*) FROM topic_threads`).Scan(&n))
	return n
}

func TestGenerateInheritsLabelsForUnchangedClusters(t *testing.T) {
	db, ids := newTopicsTestDB(t)
	embed(t, db, ids[0:3], 0)
	embed(t, db, ids[3:6], 1)
	labeler := &fakeLabeler{}
	p := NewPipeline(db, labeler)

	first := generate(t, p)
	require.Len(t, first, 2)
	assert.Equal(t, 2, labeler.count())
	for _, topic := range first {
		assert.Equal(t, lineage.SourceGenerated, topic.LabelSource)
		assert.NotZero(t, topic.ThreadID)
		assert.True(t, topic.ThreadIsNew)
	}
	assert.NotEqual(t, first[0].ThreadID, first[1].ThreadID)

	second := generate(t, p)
	require.Len(t, second, 2)
	assert.Equal(t, 2, labeler.count(), "an unchanged cluster must not call the LLM again")

	prev := byHash(first)
	for _, topic := range second {
		before, ok := prev[topic.SetHash]
		require.True(t, ok, "second run produced a cluster the first run did not")
		assert.Equal(t, before.Label, topic.Label)
		assert.Equal(t, before.ThreadID, topic.ThreadID)
		assert.Equal(t, lineage.SourceInherited, topic.LabelSource)
		assert.False(t, topic.ThreadIsNew)
	}
	assert.Equal(t, 2, countThreads(t, db))
}

func TestGenerateOpensThreadForNewCluster(t *testing.T) {
	db, ids := newTopicsTestDB(t)
	embed(t, db, ids[0:3], 0)
	embed(t, db, ids[3:6], 1)
	labeler := &fakeLabeler{}
	p := NewPipeline(db, labeler)
	generate(t, p)

	embed(t, db, ids[6:9], 2)
	second := generate(t, p)
	require.Len(t, second, 3)
	assert.Equal(t, 3, labeler.count(), "only the new cluster is labeled")

	newThreads := 0
	for _, topic := range second {
		if topic.ThreadIsNew {
			newThreads++
			assert.Equal(t, lineage.SourceGenerated, topic.LabelSource)
		}
	}
	assert.Equal(t, 1, newThreads)
	assert.Equal(t, 3, countThreads(t, db))
}

func TestGenerateNoInheritRelabelsButKeepsThreads(t *testing.T) {
	db, ids := newTopicsTestDB(t)
	embed(t, db, ids[0:3], 0)
	embed(t, db, ids[3:6], 1)
	labeler := &fakeLabeler{}
	p := NewPipeline(db, labeler)
	first := generate(t, p)

	p.Lineage.Inherit = false
	second := generate(t, p)
	assert.Equal(t, 4, labeler.count())

	prev := byHash(first)
	for _, topic := range second {
		assert.Equal(t, lineage.SourceGenerated, topic.LabelSource)
		assert.Equal(t, prev[topic.SetHash].ThreadID, topic.ThreadID, "threads survive a --no-inherit run")
		assert.NotEqual(t, prev[topic.SetHash].Label, topic.Label)

		var threadLabel string
		require.NoError(t, db.GetConnection().QueryRow(
			`SELECT label FROM topic_threads WHERE id = ?`, topic.ThreadID).Scan(&threadLabel))
		assert.Equal(t, topic.Label, threadLabel, "a generated label replaces the thread's label")
	}
	assert.Equal(t, 2, countThreads(t, db))
}

func TestGenerateWithNoPriorRunsIsAllNew(t *testing.T) {
	db, ids := newTopicsTestDB(t)
	embed(t, db, ids[0:3], 0)
	p := NewPipeline(db, &fakeLabeler{})

	topics := generate(t, p)
	require.Len(t, topics, 1)
	assert.True(t, topics[0].ThreadIsNew)
	assert.Equal(t, lineage.TransitionNew, transitionOf(topics[0]))
}

// transitionOf mirrors what cmd/topics.go reports in --json.
func transitionOf(topic *database.Topic) string {
	if topic.ThreadIsNew {
		return lineage.TransitionNew
	}
	return lineage.TransitionSurvived
}
