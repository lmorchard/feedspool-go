// Package lineage matches topic clusters across runs by item-set overlap, so a
// topic keeps one thread identity -- and one label -- from hour to hour.
//
// It is a leaf package: internal/database (the migration 14 backfill) and
// internal/topics (live runs) both import it, so it imports neither. That is
// what lets the backfill and a live run apply exactly the same rule.
//
// The rule, measured against 167 hourly production runs (issue #78): an exact
// set-hash match covers 96% of topics; any Jaccard threshold from 0.4 to 0.6
// classifies the rest identically; contested threads are rare (2 splits, 0
// merges in 166 run pairs), so "largest cluster wins" is all the arbitration
// needed.
package lineage

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	// DefaultLookback is how many previous runs are searched for a
	// predecessor. Six hourly runs still match 92% of topics, enough to bridge
	// a failed hour or two, and the count is cadence-independent.
	DefaultLookback = 6
	// DefaultAttachThreshold is the Jaccard at or above which a cluster joins
	// an existing thread. Anything between 0.4 and 0.6 gives the same answer
	// on real data; 0.5 is the middle of that plateau.
	DefaultAttachThreshold = 0.5
	// DefaultInheritThreshold is the Jaccard at or above which a cluster also
	// keeps the thread's label instead of asking the LLM again. 0.9 separates
	// "the same item set, give or take one" from a real change.
	DefaultInheritThreshold = 0.9
	// hashLength mirrors itemtext.SourceHash: 32 hex characters of sha256.
	hashLength = 32
)

// Label provenance and transition names, persisted in topic_lineage and
// reported in `topics --json`.
const (
	SourceGenerated    = "generated"
	SourceInherited    = "inherited"
	TransitionNew      = "new"
	TransitionSurvived = "survived"
)

// Candidate is a topic from a previous run that a new cluster may continue.
type Candidate struct {
	TopicID     int64
	ThreadID    int64
	ThreadLabel string  // the thread's current label, what a survivor inherits
	Hash        string  // SetHash of Items
	Items       []int64 // ascending
}

// Options tunes Assign. Inherit false assigns threads but never reuses a label.
type Options struct {
	AttachThreshold  float64
	InheritThreshold float64
	Inherit          bool
}

// Assignment is Assign's verdict for one cluster.
type Assignment struct {
	Hash       string
	ThreadID   int64   // 0 means open a new thread
	Jaccard    float64 // 1 for an exact hash match, 0 for a new thread
	Label      string  // inherited label; "" means label this cluster fresh
	Transition string  // TransitionNew or TransitionSurvived
}

// SetHash fingerprints an item set independent of order: a sorted copy of the
// IDs, joined by newlines, hashed, and truncated like itemtext.SourceHash.
func SetHash(items []int64) string {
	sorted := slices.Sorted(slices.Values(items))
	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = strconv.FormatInt(id, 10)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])[:hashLength]
}

// Jaccard is |a ∩ b| / |a ∪ b| over two ascending slices, by merge walk.
// Two empty sets have no overlap to speak of and score 0.
func Jaccard(a, b []int64) float64 {
	shared := 0
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			shared++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// Assign resolves every cluster to a thread: an exact hash match first, then
// the best Jaccard at or above AttachThreshold, else a new thread.
//
// Clusters are processed largest first (ties by smallest first item ID), and a
// thread once claimed is unavailable to later clusters, so a contested thread
// goes to its largest claimant and the result does not depend on the order
// clusters were supplied. Output is indexed like clusters.
func Assign(clusters [][]int64, candidates []Candidate, opts Options) []Assignment {
	sorted := make([][]int64, len(clusters))
	for i, c := range clusters {
		sorted[i] = slices.Sorted(slices.Values(c))
	}
	order := processingOrder(sorted)

	byHash := make(map[string]*Candidate, len(candidates))
	for i := range candidates {
		c := &candidates[i]
		if prev, ok := byHash[c.Hash]; !ok || c.TopicID > prev.TopicID {
			byHash[c.Hash] = c // the most recent topic speaks for a hash
		}
	}

	claimed := make(map[int64]bool)
	out := make([]Assignment, len(clusters))
	for _, idx := range order {
		items := sorted[idx]
		a := Assignment{Hash: SetHash(items), Transition: TransitionNew}
		if c, ok := byHash[a.Hash]; ok && !claimed[c.ThreadID] {
			attach(&a, c, 1, opts, claimed)
		} else if best, j := bestCandidate(items, candidates, claimed); best != nil && j >= opts.AttachThreshold {
			attach(&a, best, j, opts, claimed)
		}
		out[idx] = a
	}
	return out
}

// processingOrder sorts cluster indices largest first, ties by first item ID.
// Clusters are disjoint, so first item IDs differ and the order is total.
func processingOrder(sorted [][]int64) []int {
	order := make([]int, len(sorted))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool {
		a, b := sorted[order[x]], sorted[order[y]]
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		return len(a) > 0 && a[0] < b[0]
	})
	return order
}

func attach(a *Assignment, c *Candidate, jaccard float64, opts Options, claimed map[int64]bool) {
	a.ThreadID, a.Jaccard, a.Transition = c.ThreadID, jaccard, TransitionSurvived
	claimed[c.ThreadID] = true
	if opts.Inherit && jaccard >= opts.InheritThreshold {
		a.Label = c.ThreadLabel
	}
}

// bestCandidate is the unclaimed candidate with the highest Jaccard; ties go
// to the most recent TopicID so the answer is stable.
func bestCandidate(items []int64, candidates []Candidate, claimed map[int64]bool) (best *Candidate, bestJ float64) {
	for i := range candidates {
		c := &candidates[i]
		if claimed[c.ThreadID] {
			continue
		}
		j := Jaccard(items, c.Items)
		if j > bestJ || (j == bestJ && j > 0 && c.TopicID > best.TopicID) {
			best, bestJ = c, j
		}
	}
	return best, bestJ
}
