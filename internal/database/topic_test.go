package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testEmbedModel = "nomic-test"
	testLLMModel   = "qwen-test"
	testTopic1     = "Topic 1"
	testTopic2     = "Topic 2"
)

func TestInsertTopicRun(t *testing.T) {
	db := setupTestDB(t)

	// Seed items
	id1 := seedItem(t, db, "item-1", time.Now())
	id2 := seedItem(t, db, "item-2", time.Now())

	now := time.Now().UTC()
	run := &TopicRun{
		CreatedAt:    now,
		WindowStart:  now.Add(-24 * time.Hour),
		WindowEnd:    now,
		EmbedModelID: testEmbedModel,
		LLMModelID:   testLLMModel,
	}

	topic1 := &Topic{Label: testTopic1, Score: 2.0}
	topic2 := &Topic{Label: testTopic2, Score: 1.0}

	topicItems := map[*Topic][]int64{
		topic1: {id1, id2},
		topic2: {id1},
	}

	err := db.InsertTopicRun(context.Background(), run, []*Topic{topic1, topic2}, topicItems)
	require.NoError(t, err)
	assert.NotZero(t, run.ID)
	assert.NotZero(t, topic1.ID)
	assert.NotZero(t, topic2.ID)

	// Verify insertion
	var count int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM topic_runs WHERE id = ?`, run.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	err = db.conn.QueryRow(`SELECT COUNT(*) FROM topics WHERE run_id = ?`, run.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	err = db.conn.QueryRow(`SELECT COUNT(*) FROM topic_items WHERE topic_id = ?`, topic1.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestInsertTopicRunRollback(t *testing.T) {
	db := setupTestDB(t)

	id1 := seedItem(t, db, "item-1", time.Now())

	run := &TopicRun{
		CreatedAt:    time.Now().UTC(),
		WindowStart:  time.Now().UTC(),
		WindowEnd:    time.Now().UTC(),
		EmbedModelID: testEmbedModel,
		LLMModelID:   testLLMModel,
	}

	topic1 := &Topic{Label: testTopic1, Score: 1.0}

	// id -9999 does not exist, triggering a foreign key constraint violation
	topicItems := map[*Topic][]int64{
		topic1: {id1, -9999},
	}

	err := db.InsertTopicRun(context.Background(), run, []*Topic{topic1}, topicItems)
	require.Error(t, err, "expected foreign key violation")

	// Verify rollback
	var count int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM topic_runs WHERE embed_model_id = 'nomic-test'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	err = db.conn.QueryRow(`SELECT COUNT(*) FROM topics WHERE label = 'Topic 1'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGetTopicRunAndItems(t *testing.T) {
	db := setupTestDB(t)

	// Ensure empty database returns nil for latest run
	latest, err := db.GetLatestTopicRun(context.Background())
	require.NoError(t, err)
	assert.Nil(t, latest)

	// Seed items
	id1 := seedItem(t, db, "item-1", time.Now())
	id2 := seedItem(t, db, "item-2", time.Now())

	now := time.Now().UTC()
	run := &TopicRun{
		CreatedAt:    now,
		WindowStart:  now.Add(-24 * time.Hour),
		WindowEnd:    now,
		EmbedModelID: testEmbedModel,
		LLMModelID:   testLLMModel,
	}

	topic1 := &Topic{Label: testTopic1, Score: 2.0}
	topic2 := &Topic{Label: testTopic2, Score: 1.0}

	topicItems := map[*Topic][]int64{
		topic1: {id1, id2},
		topic2: {id2},
	}

	err = db.InsertTopicRun(context.Background(), run, []*Topic{topic1, topic2}, topicItems)
	require.NoError(t, err)

	// Fetch latest run
	latest, err = db.GetLatestTopicRun(context.Background())
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, run.ID, latest.ID)
	// Database stores time with second precision, so we format and compare that
	assert.Equal(t, formatDatabaseTime(run.CreatedAt), formatDatabaseTime(latest.CreatedAt))
	assert.Equal(t, testEmbedModel, latest.EmbedModelID)

	// Fetch topics
	fetchedTopics, err := db.GetTopicsForRun(context.Background(), latest.ID)
	require.NoError(t, err)
	require.Len(t, fetchedTopics, 2)
	// Ordered by score DESC
	assert.Equal(t, testTopic1, fetchedTopics[0].Label)
	assert.Equal(t, 2.0, fetchedTopics[0].Score)
	assert.Equal(t, testTopic2, fetchedTopics[1].Label)

	// Fetch items map
	itemMap, err := db.GetTopicItems(context.Background(), latest.ID)
	require.NoError(t, err)
	require.Len(t, itemMap, 2)

	// topic1 has id1 and id2
	assert.ElementsMatch(t, []int64{id1, id2}, itemMap[fetchedTopics[0].ID])
	// topic2 has id2
	assert.ElementsMatch(t, []int64{id2}, itemMap[fetchedTopics[1].ID])
}

func TestDeleteTopicRuns(t *testing.T) {
	db := setupTestDB(t)

	id1 := seedItem(t, db, "item-1", time.Now())

	now := time.Now().UTC()
	oldRun := &TopicRun{
		CreatedAt:    now.Add(-60 * 24 * time.Hour),
		WindowStart:  now.Add(-61 * 24 * time.Hour),
		WindowEnd:    now.Add(-60 * 24 * time.Hour),
		EmbedModelID: testEmbedModel,
		LLMModelID:   testLLMModel,
	}
	topicOld := &Topic{Label: "Old Topic", Score: 1.0}
	err := db.InsertTopicRun(context.Background(), oldRun, []*Topic{topicOld}, map[*Topic][]int64{topicOld: {id1}})
	require.NoError(t, err)

	recentRun := &TopicRun{
		CreatedAt:    now,
		WindowStart:  now.Add(-24 * time.Hour),
		WindowEnd:    now,
		EmbedModelID: testEmbedModel,
		LLMModelID:   testLLMModel,
	}
	topicRecent := &Topic{Label: "Recent Topic", Score: 1.0}
	err = db.InsertTopicRun(context.Background(), recentRun, []*Topic{topicRecent}, map[*Topic][]int64{topicRecent: {id1}})
	require.NoError(t, err)

	// Purge topic runs older than 30 days, keeping latest
	cutoff := now.Add(-30 * 24 * time.Hour)
	deleted, err := db.DeleteTopicRuns(context.Background(), cutoff, true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	// Verify old run and its topics/items were deleted by CASCADE
	var count int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM topic_runs WHERE id = ?`, oldRun.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	err = db.conn.QueryRow(`SELECT COUNT(*) FROM topics WHERE id = ?`, topicOld.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify recent run remains
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM topic_runs WHERE id = ?`, recentRun.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
