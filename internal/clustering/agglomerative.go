package clustering

import (
	"time"

	"github.com/sirupsen/logrus"

	"github.com/lmorchard/feedspool-go/internal/database"
)

// Cluster performs single-linkage clustering on the given embeddings.
// Returns a slice of clusters, where each cluster is a slice of ItemIDs.
// Two clusters are merged if the maximum similarity between any two members is >= threshold.
// Mathematically, for single-linkage at a fixed threshold, this is equivalent to
// finding the connected components of a graph where edges are pairs with similarity >= threshold.
func Cluster(
	embeddings []*database.ItemEmbedding, threshold float32,
) ([][]int64, error) {
	n := len(embeddings)
	if n == 0 {
		return nil, nil
	}

	logrus.Infof("Computing pairwise similarities and connected components for %d items...", n)
	start := time.Now()

	// Union-Find (Disjoint Set) to track connected components
	parent := make([]int, n)
	for i := 0; i < n; i++ {
		parent[i] = i
	}

	var find func(i int) int
	find = func(i int) int {
		if parent[i] == i {
			return i
		}
		parent[i] = find(parent[i])
		return parent[i]
	}

	union := func(i, j int) {
		rootI := find(i)
		rootJ := find(j)
		if rootI != rootJ {
			parent[rootI] = rootJ
		}
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			sim, err := database.DotProduct(embeddings[i].Vector, embeddings[j].Vector)
			if err != nil {
				return nil, err
			}
			if sim >= threshold {
				union(i, j)
			}
		}
	}

	// Group elements by root
	groups := make(map[int][]int64)
	for i := 0; i < n; i++ {
		root := find(i)
		groups[root] = append(groups[root], embeddings[i].ItemID)
	}

	var result [][]int64
	for _, cluster := range groups {
		result = append(result, cluster)
	}

	logrus.Infof("Finished clustering %d items into %d clusters in %v.", n, len(result), time.Since(start))
	return result, nil
}
