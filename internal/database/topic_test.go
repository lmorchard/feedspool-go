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
	topic2 := &Topic{Label: "Topic 2", Score: 1.0}

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
