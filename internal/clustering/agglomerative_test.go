package clustering

import (
	"testing"

	"github.com/lmorchard/feedspool-go/internal/database"
)

// unitVectorAt returns a one-hot unit vector.
func unitVectorAt(dims, hot int) []float32 {
	v := make([]float32, dims)
	v[hot] = 1.0
	return v
}

// mixedVector returns a vector with two hot indices.
func mixedVector(dims, hot1, hot2 int) []float32 {
	v := make([]float32, dims)
	v[hot1] = 0.7071 // approx 1/sqrt(2)
	v[hot2] = 0.7071
	return v
}

func TestClusterMismatch(t *testing.T) {
	embeddings := []*database.ItemEmbedding{
		{ItemID: 1, Vector: unitVectorAt(10, 0)},
		{ItemID: 2, Vector: unitVectorAt(20, 0)},
	}
	_, err := Cluster(embeddings, 0.7)
	if err == nil {
		t.Fatal("expected error for mismatched vector widths, got nil")
	}
}

func TestClusterEmpty(t *testing.T) {
	var embeddings []*database.ItemEmbedding
	clusters, err := Cluster(embeddings, 0.7)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(clusters))
	}
}

func TestClusterSingle(t *testing.T) {
	embeddings := []*database.ItemEmbedding{
		{ItemID: 1, Vector: unitVectorAt(10, 0)},
	}
	clusters, err := Cluster(embeddings, 0.7)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	if clusters[0][0] != 1 {
		t.Errorf("expected ItemID 1 in cluster, got %d", clusters[0][0])
	}
}

func TestClusterDuplicates(t *testing.T) {
	// Duplicates have similarity 1.0, should always cluster together if threshold < 1.0
	embeddings := []*database.ItemEmbedding{
		{ItemID: 1, Vector: unitVectorAt(10, 0)},
		{ItemID: 2, Vector: unitVectorAt(10, 0)},
	}
	clusters, err := Cluster(embeddings, 0.7)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	if len(clusters[0]) != 2 {
		t.Errorf("expected 2 items in cluster, got %d", len(clusters[0]))
	}
}

func TestClusterSeparation(t *testing.T) {
	embeddings := []*database.ItemEmbedding{
		{ItemID: 1, Vector: unitVectorAt(10, 0)},
		{ItemID: 2, Vector: unitVectorAt(10, 0)},
		{ItemID: 3, Vector: unitVectorAt(10, 1)},
		{ItemID: 4, Vector: unitVectorAt(10, 1)},
	}
	clusters, err := Cluster(embeddings, 0.7)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}
}

func TestClusterChaining(t *testing.T) {
	// A and B have 0.707 similarity. B and C have 0.707. A and C have 0.
	embeddings := []*database.ItemEmbedding{
		{ItemID: 1, Vector: unitVectorAt(10, 0)},   // A
		{ItemID: 2, Vector: mixedVector(10, 0, 1)}, // B
		{ItemID: 3, Vector: unitVectorAt(10, 1)},   // C
	}

	// With threshold 0.70, A and B merge. Then C merges with AB because sim(C, B) = 0.707 > 0.70
	clusters, err := Cluster(embeddings, 0.70)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 chained cluster, got %d", len(clusters))
	}

	// With threshold 0.80, nothing merges
	clusters, err = Cluster(embeddings, 0.80)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 3 {
		t.Fatalf("expected 3 separate clusters, got %d", len(clusters))
	}
}
