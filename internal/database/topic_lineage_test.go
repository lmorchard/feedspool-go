package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lmorchard/feedspool-go/internal/lineage"
)

func newTopicRun(at time.Time) *TopicRun {
	return &TopicRun{
		CreatedAt:    at,
		WindowStart:  at.Add(-24 * time.Hour),
		WindowEnd:    at,
		EmbedModelID: testEmbedModel,
		LLMModelID:   testLLMModel,
	}
}

type threadRow struct {
	firstSeen, lastSeen, label, labeledAt string
}

func readThread(t *testing.T, db *DB, id int64) threadRow {
	t.Helper()
	var row threadRow
	err := db.conn.QueryRow(
		`SELECT first_seen_at, last_seen_at, label, labeled_at FROM topic_threads WHERE id = ?`, id,
	).Scan(&row.firstSeen, &row.lastSeen, &row.label, &row.labeledAt)
	require.NoError(t, err)
	return row
}

func countRows(t *testing.T, db *DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.conn.QueryRow(query, args...).Scan(&n))
	return n
}

func TestInsertTopicRunOpensThreads(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	id1 := seedItem(t, db, "item-1", time.Now())
	id2 := seedItem(t, db, "item-2", time.Now())
	id3 := seedItem(t, db, "item-3", time.Now())

	now := time.Now().UTC()
	run := newTopicRun(now)
	topic1 := &Topic{Label: testTopic1, Score: 2.0}
	topic2 := &Topic{Label: testTopic2, Score: 1.0}
	require.NoError(t, db.InsertTopicRun(ctx, run, []*Topic{topic1, topic2},
		map[*Topic][]int64{topic1: {id2, id1}, topic2: {id3}}))

	assert.NotZero(t, topic1.ThreadID)
	assert.NotZero(t, topic2.ThreadID)
	assert.NotEqual(t, topic1.ThreadID, topic2.ThreadID)
	assert.True(t, topic1.ThreadIsNew)
	assert.Equal(t, lineage.SetHash([]int64{id1, id2}), topic1.SetHash)
	assert.Len(t, topic1.SetHash, 32)
	assert.Equal(t, lineage.SourceGenerated, topic1.LabelSource)

	row := readThread(t, db, topic1.ThreadID)
	assert.Equal(t, testTopic1, row.label)
	assert.Equal(t, formatDatabaseTime(now), row.firstSeen)
	assert.Equal(t, row.firstSeen, row.lastSeen)
	assert.Equal(t, row.firstSeen, row.labeledAt)

	assert.Equal(t, 2, countRows(t, db, `SELECT COUNT(*) FROM topic_lineage`))
	var source string
	require.NoError(t, db.conn.QueryRow(
		`SELECT label_source FROM topic_lineage WHERE topic_id = ?`, topic1.ID).Scan(&source))
	assert.Equal(t, lineage.SourceGenerated, source)
}

func TestInsertTopicRunAttachesInheritedTopic(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	id1 := seedItem(t, db, "item-1", time.Now())

	t0 := time.Now().UTC().Add(-time.Hour)
	first := &Topic{Label: testTopic1, Score: 1.0}
	require.NoError(t, db.InsertTopicRun(ctx, newTopicRun(t0), []*Topic{first}, map[*Topic][]int64{first: {id1}}))

	t1 := t0.Add(time.Hour)
	second := &Topic{Label: testTopic1, Score: 1.0, ThreadID: first.ThreadID, LabelSource: lineage.SourceInherited}
	require.NoError(t, db.InsertTopicRun(ctx, newTopicRun(t1), []*Topic{second}, map[*Topic][]int64{second: {id1}}))

	assert.False(t, second.ThreadIsNew)
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM topic_threads`))
	row := readThread(t, db, first.ThreadID)
	assert.Equal(t, formatDatabaseTime(t1), row.lastSeen)
	assert.Equal(t, formatDatabaseTime(t0), row.firstSeen)
	assert.Equal(t, formatDatabaseTime(t0), row.labeledAt, "an inherited label must not move labeled_at")
	assert.Equal(t, testTopic1, row.label)
}

func TestInsertTopicRunRelabelsThreadOnGeneratedSurvivor(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	id1 := seedItem(t, db, "item-1", time.Now())

	t0 := time.Now().UTC().Add(-time.Hour)
	first := &Topic{Label: testTopic1, Score: 1.0}
	require.NoError(t, db.InsertTopicRun(ctx, newTopicRun(t0), []*Topic{first}, map[*Topic][]int64{first: {id1}}))

	t1 := t0.Add(time.Hour)
	second := &Topic{Label: "Fresh Label", Score: 1.0, ThreadID: first.ThreadID, LabelSource: lineage.SourceGenerated}
	require.NoError(t, db.InsertTopicRun(ctx, newTopicRun(t1), []*Topic{second}, map[*Topic][]int64{second: {id1}}))

	row := readThread(t, db, first.ThreadID)
	assert.Equal(t, "Fresh Label", row.label)
	assert.Equal(t, formatDatabaseTime(t1), row.labeledAt)
	assert.Equal(t, formatDatabaseTime(t1), row.lastSeen)
}

func TestInsertTopicRunRejectsUnknownThread(t *testing.T) {
	db := setupTestDB(t)
	id1 := seedItem(t, db, "item-1", time.Now())

	topic := &Topic{Label: testTopic1, Score: 1.0, ThreadID: 9999, LabelSource: lineage.SourceInherited}
	err := db.InsertTopicRun(context.Background(), newTopicRun(time.Now().UTC()),
		[]*Topic{topic}, map[*Topic][]int64{topic: {id1}})
	require.Error(t, err)
	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM topic_runs`))
	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM topic_threads`))
}

func TestGetTopicsForRunIncludesLineage(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	id1 := seedItem(t, db, "item-1", time.Now())

	run := newTopicRun(time.Now().UTC())
	topic := &Topic{Label: testTopic1, Score: 2.0}
	require.NoError(t, db.InsertTopicRun(ctx, run, []*Topic{topic}, map[*Topic][]int64{topic: {id1}}))

	// A row written before migration 14 has no lineage; it must still load.
	_, err := db.conn.Exec(`INSERT INTO topics (run_id, label, score) VALUES (?, ?, ?)`, run.ID, "Legacy", 1.0)
	require.NoError(t, err)

	got, err := db.GetTopicsForRun(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, topic.ThreadID, got[0].ThreadID)
	assert.Equal(t, topic.SetHash, got[0].SetHash)
	assert.Equal(t, lineage.SourceGenerated, got[0].LabelSource)

	assert.Equal(t, "Legacy", got[1].Label)
	assert.Zero(t, got[1].ThreadID)
	assert.Empty(t, got[1].SetHash)
	assert.Empty(t, got[1].LabelSource)
}

func TestGetLineageCandidates(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	id1 := seedItem(t, db, "item-1", time.Now())
	id2 := seedItem(t, db, "item-2", time.Now())
	id3 := seedItem(t, db, "item-3", time.Now())

	base := time.Now().UTC().Add(-3 * time.Hour)
	runs := make([]*TopicRun, 0, 3)
	topics := make([]*Topic, 0, 3)
	for i, label := range []string{"Run1", "Run2", "Run3"} {
		run := newTopicRun(base.Add(time.Duration(i) * time.Hour))
		topic := &Topic{Label: label, Score: 1.0}
		require.NoError(t, db.InsertTopicRun(ctx, run, []*Topic{topic}, map[*Topic][]int64{topic: {id3, id1, id2}}))
		runs = append(runs, run)
		topics = append(topics, topic)
	}

	got, err := db.GetLineageCandidates(ctx, base.Add(3*time.Hour), 2)
	require.NoError(t, err)
	require.Len(t, got, 2, "lookback 2 must return the two most recent runs only")
	byTopic := map[int64]lineage.Candidate{}
	for _, c := range got {
		byTopic[c.TopicID] = c
	}
	c3, ok := byTopic[topics[2].ID]
	require.True(t, ok)
	assert.Equal(t, topics[2].ThreadID, c3.ThreadID)
	assert.Equal(t, "Run3", c3.ThreadLabel)
	assert.Equal(t, topics[2].SetHash, c3.Hash)
	assert.Equal(t, []int64{id1, id2, id3}, c3.Items, "items must come back ascending")
	_, ok = byTopic[topics[0].ID]
	assert.False(t, ok, "run 1 is outside the lookback")

	got, err = db.GetLineageCandidates(ctx, runs[1].CreatedAt, 6)
	require.NoError(t, err)
	require.Len(t, got, 1, "before = run 2's time must see only run 1")
	assert.Equal(t, topics[0].ID, got[0].TopicID)

	got, err = db.GetLineageCandidates(ctx, base, 6)
	require.NoError(t, err)
	assert.Empty(t, got)
}
