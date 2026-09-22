// Package trends turns a topic's items and its thread's history into
// explainable trend signals: a daily histogram, counts for the last and prior
// 24 hours, distinct feeds, what changed since the thread's previous topic,
// and a five-state status.
//
// It is pure -- no database, no clock. Everything is anchored on the run's
// window, so an old run always reports the same thing, and the renderer and
// the CLI can share it (issue #78, slices 2 and 3).
package trends

import (
	"strings"
	"time"
)

// Status values, in the precedence Compute applies them.
const (
	StatusNew     = "new"
	StatusGrowing = "growing"
	StatusFading  = "fading"
	StatusQuiet   = "quiet"
	StatusSteady  = "steady"
)

const day = 24 * time.Hour

// Item is the part of a feed item trends needs.
type Item struct {
	FeedURL string
	At      time.Time // the item's effective date
}

// Input is everything Compute needs about one topic.
type Input struct {
	WindowStart, WindowEnd time.Time
	Items                  []Item
	ItemIDs                []int64 // this topic's members
	PrevItemIDs            []int64 // the thread's previous topic's members
	HasPrev                bool    // false when the thread has no earlier topic
	ThreadFirstSeen        time.Time
}

// Trend is Compute's result, shaped for `topics --json`.
type Trend struct {
	Status          string    `json:"status"`
	DistinctFeeds   int       `json:"distinct_feeds"`
	Last24h         int       `json:"last_24h"`
	Prior24h        int       `json:"prior_24h"`
	NewItems        int       `json:"new_items"`
	DroppedItems    int       `json:"dropped_items"`
	Daily           []int     `json:"daily"`
	ThreadFirstSeen time.Time `json:"thread_first_seen"`
}

// Compute derives a topic's trend signals from its items and history.
func Compute(in *Input) Trend {
	t := Trend{Daily: dailyBuckets(in), ThreadFirstSeen: in.ThreadFirstSeen}
	n := len(t.Daily)
	t.Last24h = t.Daily[n-1]
	if n > 1 {
		t.Prior24h = t.Daily[n-2]
	}
	t.DistinctFeeds = distinctFeeds(in.Items)
	t.NewItems, t.DroppedItems = diff(in.ItemIDs, in.PrevItemIDs, in.HasPrev)
	t.Status = status(in, &t)
	return t
}

// dailyBuckets counts items into ceil(window/24h) buckets (at least one)
// ending at WindowEnd, oldest first. Bucket i covers
// (end-(n-i)*24h, end-(n-i-1)*24h]: closed on the right, so an item exactly
// k*24h old lands in the bucket that ends there. Items outside the window --
// possible through the first_seen fallback -- clamp into the first or last
// bucket, so the counts always sum to the number of items.
func dailyBuckets(in *Input) []int {
	n := max(1, int((in.WindowEnd.Sub(in.WindowStart)+day-1)/day))
	out := make([]int, n)
	for _, it := range in.Items {
		// Floor division; a negative age (after end) truncates toward zero.
		back := int(in.WindowEnd.Sub(it.At) / day)
		out[max(0, min(n-1, n-1-back))]++
	}
	return out
}

func distinctFeeds(items []Item) int {
	seen := make(map[string]struct{}, len(items))
	for _, it := range items {
		seen[it.FeedURL] = struct{}{}
	}
	return len(seen)
}

// diff counts members added and dropped relative to the previous topic.
// Without a previous topic everything is new and nothing was dropped.
func diff(cur, prev []int64, hasPrev bool) (added, dropped int) {
	if !hasPrev {
		return len(cur), 0
	}
	prevSet := make(map[int64]struct{}, len(prev))
	for _, id := range prev {
		prevSet[id] = struct{}{}
	}
	curSet := make(map[int64]struct{}, len(cur))
	for _, id := range cur {
		curSet[id] = struct{}{}
		if _, ok := prevSet[id]; !ok {
			added++
		}
	}
	for id := range prevSet {
		if _, ok := curSet[id]; !ok {
			dropped++
		}
	}
	return added, dropped
}

// status applies, first match wins: a thread first seen within the last 24h
// of the window is new; otherwise compare the last 24h with the 24h before.
// Nothing in either is quiet -- kept apart from steady because on real data
// it was 36 of 37 "steady" topics, dormant rather than consistently active.
func status(in *Input, t *Trend) string {
	switch {
	case !in.ThreadFirstSeen.Before(in.WindowEnd.Add(-day)):
		return StatusNew
	case t.Last24h > t.Prior24h:
		return StatusGrowing
	case t.Last24h < t.Prior24h:
		return StatusFading
	case t.Last24h == 0:
		return StatusQuiet
	default:
		return StatusSteady
	}
}

const sparkLevels = "▁▂▃▄▅▆▇█"

// Sparkline renders counts as one block character each, scaled to the
// slice's own maximum. An all-zero slice renders as the lowest block.
func Sparkline(daily []int) string {
	peak := 0
	for _, v := range daily {
		peak = max(peak, v)
	}
	levels := []rune(sparkLevels)
	var b strings.Builder
	top := len(levels) - 1
	for _, v := range daily {
		level := 0
		if peak > 0 {
			level = v * top / peak
		}
		b.WriteRune(levels[level])
	}
	return b.String()
}
