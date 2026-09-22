// Command topic-lineage is the measurement spike for issue #78.
//
// It answers, against real hourly `feedspool topics` runs, the questions the
// issue leaves open before anyone picks a number:
//
//  1. How well do topics in adjacent runs match by item-set overlap (Jaccard)?
//     Is the distribution of best-match scores bimodal, so a threshold exists?
//  2. How fast does overlap decay when runs are skipped (lag 2, 3, 6, ...)?
//     That decides how many previous runs lineage should match against.
//  3. How often do splits and merges happen?
//  4. Among topics that clearly survived, how often is the LLM label identical?
//     That quantifies the flicker label inheritance would remove.
//  5. Following mutual-best matches into threads, how long do threads live and
//     how far do their labels drift?
//
// Read-only: it opens the file with mode=ro and reads only the migration-13
// tables (topic_runs, topics, topic_items), so it works on a spool that has not
// been migrated to 14 and cannot alter one -- no journal-mode switch, no WAL
// sidecars, no migrations. Usage:
//
//	go run ./scripts/topic-lineage --database data/feeds.db --last 24h
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // the same pure-Go driver feedspool itself uses
)

const (
	histogramBuckets   = 10
	majorityShare      = 0.5
	defaultThreshold   = 0.5
	defaultTopThreads  = 15
	defaultLast        = 24 * time.Hour
	maxLabelsPerThread = 8
	percent            = 100
	p50, p25, p10      = 50, 25, 10
	strongJaccard      = 0.9
)

type topic struct {
	id    int64
	label string
	items map[int64]struct{}
}

type topicRun struct {
	id     int64
	at     time.Time
	topics []*topic
}

// match is one overlapping (predecessor, successor) pair.
type match struct {
	prev, cur *topic
	shared    int
	jaccard   float64
}

type options struct {
	database  string
	last      time.Duration
	threshold float64
	top       int
	verbose   bool
}

func main() {
	if err := runSpike(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func parseOptions() options {
	var opts options
	flag.StringVar(&opts.database, "database", "data/feeds.db",
		"path to the feedspool database (only SELECTs are issued)")
	flag.DurationVar(&opts.last, "last", defaultLast, "only consider runs created within this duration; 0 means all")
	flag.Float64Var(&opts.threshold, "threshold", defaultThreshold,
		"Jaccard at or above which two topics count as the same thread")
	flag.IntVar(&opts.top, "top", defaultTopThreads, "how many of the longest threads to print")
	flag.BoolVar(&opts.verbose, "verbose", false, "print every matched pair for each adjacent run")
	flag.Parse()
	return opts
}

func runSpike() error {
	opts := parseOptions()

	if _, err := os.Stat(opts.database); err != nil {
		return fmt.Errorf("database %q: %w", opts.database, err)
	}
	// Not database.New: that would switch a non-WAL file to WAL and create
	// sidecars, and this is a measurement, not a command.
	db, err := sql.Open("sqlite", "file:"+opts.database+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()
	var since time.Time
	if opts.last > 0 {
		since = time.Now().Add(-opts.last)
	}
	runs, err := loadRuns(ctx, db, since)
	if err != nil {
		return err
	}
	if len(runs) < 2 {
		return errors.New("need at least two topic runs in the window; widen --last")
	}

	printTimeline(runs)
	printBestJaccardHistogram(runs)
	printLagDecay(runs, opts.threshold)
	printThresholdSweep(runs)
	printSplitsMerges(runs)
	printLabelAgreement(runs, opts.threshold)
	printThreads(runs, opts.threshold, opts.top)
	if opts.verbose {
		printPairs(runs, opts.threshold)
	}
	return nil
}

// loadRuns reads every run created at or after since, oldest first, with its
// topics and their item sets.
func loadRuns(ctx context.Context, db *sql.DB, since time.Time) ([]*topicRun, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, created_at FROM topic_runs
		WHERE created_at >= ?
		ORDER BY created_at ASC`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("query topic_runs: %w", err)
	}
	defer rows.Close()

	var runs []*topicRun
	for rows.Next() {
		var r topicRun
		var createdAt string
		if err := rows.Scan(&r.id, &createdAt); err != nil {
			return nil, fmt.Errorf("scan topic_runs: %w", err)
		}
		if r.at, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, fmt.Errorf("parse created_at %q: %w", createdAt, err)
		}
		runs = append(runs, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, r := range runs {
		if err := loadTopics(ctx, db, r); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

// loadTopics reads one run's topics and item sets from the migration-13 tables
// only, deliberately not through database.GetTopicsForRun, which joins
// topic_lineage and so needs migration 14.
func loadTopics(ctx context.Context, db *sql.DB, r *topicRun) error {
	byID, err := loadTopicRows(ctx, db, r)
	if err != nil {
		return err
	}
	return loadTopicItems(ctx, db, r.id, byID)
}

func loadTopicRows(ctx context.Context, db *sql.DB, r *topicRun) (map[int64]*topic, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, label FROM topics WHERE run_id = ? ORDER BY score DESC, id`, r.id)
	if err != nil {
		return nil, fmt.Errorf("query topics for run %d: %w", r.id, err)
	}
	defer rows.Close()

	byID := make(map[int64]*topic)
	for rows.Next() {
		t := &topic{items: make(map[int64]struct{})}
		if err := rows.Scan(&t.id, &t.label); err != nil {
			return nil, fmt.Errorf("scan topic: %w", err)
		}
		r.topics = append(r.topics, t)
		byID[t.id] = t
	}
	return byID, rows.Err()
}

func loadTopicItems(ctx context.Context, db *sql.DB, runID int64, byID map[int64]*topic) error {
	items, err := db.QueryContext(ctx, `
		SELECT ti.topic_id, ti.item_id FROM topic_items ti
		JOIN topics t ON t.id = ti.topic_id
		WHERE t.run_id = ?`, runID)
	if err != nil {
		return fmt.Errorf("query topic items for run %d: %w", runID, err)
	}
	defer items.Close()
	for items.Next() {
		var topicID, itemID int64
		if err := items.Scan(&topicID, &itemID); err != nil {
			return fmt.Errorf("scan topic item: %w", err)
		}
		if t, ok := byID[topicID]; ok {
			t.items[itemID] = struct{}{}
		}
	}
	return items.Err()
}

// matches returns every overlapping pair between the topics of prev and cur.
func matches(prev, cur *topicRun) []match {
	var out []match
	for _, a := range prev.topics {
		for _, b := range cur.topics {
			shared := overlap(a.items, b.items)
			if shared == 0 {
				continue
			}
			union := len(a.items) + len(b.items) - shared
			out = append(out, match{prev: a, cur: b, shared: shared, jaccard: float64(shared) / float64(union)})
		}
	}
	return out
}

func overlap(a, b map[int64]struct{}) int {
	if len(a) > len(b) {
		a, b = b, a
	}
	n := 0
	for id := range a {
		if _, ok := b[id]; ok {
			n++
		}
	}
	return n
}

// bestByCur picks, for each successor topic, its highest-Jaccard predecessor.
func bestByCur(ms []match) map[*topic]match {
	best := make(map[*topic]match)
	for _, m := range ms {
		if cur, ok := best[m.cur]; !ok || m.jaccard > cur.jaccard {
			best[m.cur] = m
		}
	}
	return best
}

// bestByPrev picks, for each predecessor topic, its highest-Jaccard successor.
func bestByPrev(ms []match) map[*topic]match {
	best := make(map[*topic]match)
	for _, m := range ms {
		if prev, ok := best[m.prev]; !ok || m.jaccard > prev.jaccard {
			best[m.prev] = m
		}
	}
	return best
}

// mutualBest returns the successor→predecessor links where each is the other's
// best match. This is the conservative "same thread" relation.
func mutualBest(ms []match) map[*topic]match {
	byCur, byPrev := bestByCur(ms), bestByPrev(ms)
	out := make(map[*topic]match)
	for cur, m := range byCur {
		if back, ok := byPrev[m.prev]; ok && back.cur == cur {
			out[cur] = m
		}
	}
	return out
}

func printTimeline(runs []*topicRun) {
	fmt.Printf("== %d runs, %s to %s\n\n", len(runs),
		runs[0].at.Format(time.RFC3339), runs[len(runs)-1].at.Format(time.RFC3339))
	fmt.Printf("%6s  %-20s  %6s  %6s  %8s\n", "run", "created_at", "topics", "items", "largest")
	for _, r := range runs {
		items, largest := 0, 0
		for _, t := range r.topics {
			items += len(t.items)
			if len(t.items) > largest {
				largest = len(t.items)
			}
		}
		fmt.Printf("%6d  %-20s  %6d  %6d  %8d\n", r.id, r.at.Format("2006-01-02 15:04"), len(r.topics), items, largest)
	}
	fmt.Println()
}

// printBestJaccardHistogram is the headline result: for every topic in every
// run after the first, the Jaccard of its best predecessor. A usable threshold
// exists if this is bimodal -- a pile near 1.0 and a pile near 0.
func printBestJaccardHistogram(runs []*topicRun) {
	var buckets [histogramBuckets + 1]int // last bucket is exactly 1.0
	none, total := 0, 0
	for i := 1; i < len(runs); i++ {
		best := bestByCur(matches(runs[i-1], runs[i]))
		for _, t := range runs[i].topics {
			total++
			m, ok := best[t]
			if !ok {
				none++
				continue
			}
			buckets[bucketOf(m.jaccard)]++
		}
	}

	fmt.Println("== Best-predecessor Jaccard, all topics in all adjacent run pairs (lag 1)")
	fmt.Printf("%-12s %6s  %s\n", "jaccard", "count", "")
	fmt.Printf("%-12s %6d  %s\n", "no overlap", none, bar(none, total))
	for i := range histogramBuckets {
		lo, hi := float64(i)/histogramBuckets, float64(i+1)/histogramBuckets
		fmt.Printf("[%.1f, %.1f)   %6d  %s\n", lo, hi, buckets[i], bar(buckets[i], total))
	}
	fmt.Printf("%-12s %6d  %s\n", "exactly 1.0", buckets[histogramBuckets], bar(buckets[histogramBuckets], total))
	fmt.Printf("%-12s %6d\n\n", "total", total)
}

func bucketOf(j float64) int {
	if j >= 1 {
		return histogramBuckets
	}
	return int(j * histogramBuckets)
}

func bar(n, total int) string {
	if total == 0 {
		return ""
	}
	const width = 50
	return strings.Repeat("#", n*width/total)
}

// printLagDecay compares each run to the run lag steps earlier. How quickly
// the median best Jaccard falls tells us how many previous runs lineage must
// look back through to bridge a failed hour or two.
func printLagDecay(runs []*topicRun, threshold float64) {
	lags := []int{1, 2, 3, 6, 12, 24}
	fmt.Printf("== Best-predecessor Jaccard by lag (threshold %.2f)\n", threshold)
	fmt.Printf("%4s  %6s  %7s  %7s  %7s  %9s\n", "lag", "pairs", "median", "p25", "p10", ">=thresh")
	for _, lag := range lags {
		if lag >= len(runs) {
			continue
		}
		var scores []float64
		pairs := 0
		for i := lag; i < len(runs); i++ {
			pairs++
			best := bestByCur(matches(runs[i-lag], runs[i]))
			for _, t := range runs[i].topics {
				scores = append(scores, best[t].jaccard) // zero value when unmatched
			}
		}
		sort.Float64s(scores)
		above := 0
		for _, s := range scores {
			if s >= threshold {
				above++
			}
		}
		fmt.Printf("%4d  %6d  %7.3f  %7.3f  %7.3f  %8.1f%%\n", lag, pairs,
			percentile(scores, p50), percentile(scores, p25), percentile(scores, p10),
			float64(above)*percent/float64(len(scores)))
	}
	fmt.Println()
}

func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := len(sorted) * p / percent
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// printThresholdSweep classifies every topic at each candidate threshold:
// survived (mutual best at or above t), attached (best at or above t but not
// mutual -- a split or merge participant), emerged (nothing at or above t),
// and on the predecessor side, disappeared.
func printThresholdSweep(runs []*topicRun) {
	thresholds := []float64{0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9}
	fmt.Println("== Transition counts by threshold, summed over adjacent run pairs")
	fmt.Printf("%6s  %9s  %9s  %9s  %12s\n", "t", "survived", "attached", "emerged", "disappeared")
	for _, t := range thresholds {
		var survived, attached, emerged, disappeared int
		for i := 1; i < len(runs); i++ {
			ms := matches(runs[i-1], runs[i])
			byCur, byPrev, mutual := bestByCur(ms), bestByPrev(ms), mutualBest(ms)
			for _, cur := range runs[i].topics {
				switch m, ok := byCur[cur]; {
				case !ok || m.jaccard < t:
					emerged++
				case mutual[cur].jaccard >= t:
					survived++
				default:
					attached++
				}
			}
			for _, prev := range runs[i-1].topics {
				if m, ok := byPrev[prev]; !ok || m.jaccard < t {
					disappeared++
				}
			}
		}
		fmt.Printf("%6.2f  %9d  %9d  %9d  %12d\n", t, survived, attached, emerged, disappeared)
	}
	fmt.Println()
}

// printSplitsMerges counts structural events independent of any Jaccard
// threshold: a split is a predecessor that supplies the majority of two or
// more successors; a merge is a successor that absorbed the majority of two
// or more predecessors.
func printSplitsMerges(runs []*topicRun) {
	splits, merges := 0, 0
	var examples []string
	for i := 1; i < len(runs); i++ {
		ms := matches(runs[i-1], runs[i])
		successors := make(map[*topic][]match) // prev -> successors it dominates
		predecessors := make(map[*topic][]match)
		for _, m := range ms {
			if float64(m.shared)/float64(len(m.cur.items)) >= majorityShare {
				successors[m.prev] = append(successors[m.prev], m)
			}
			if float64(m.shared)/float64(len(m.prev.items)) >= majorityShare {
				predecessors[m.cur] = append(predecessors[m.cur], m)
			}
		}
		for prev, ss := range successors {
			if len(ss) >= 2 {
				splits++
				examples = append(examples, describeEvent("split", runs[i].id, prev, ss, true))
			}
		}
		for cur, ps := range predecessors {
			if len(ps) >= 2 {
				merges++
				examples = append(examples, describeEvent("merge", runs[i].id, cur, ps, false))
			}
		}
	}
	fmt.Printf("== Splits and merges (majority share >= %.0f%%): %d splits, %d merges over %d pairs\n",
		majorityShare*percent, splits, merges, len(runs)-1)
	sort.Strings(examples)
	for _, e := range examples {
		fmt.Println("  " + e)
	}
	fmt.Println()
}

func describeEvent(kind string, runID int64, hub *topic, ms []match, hubIsPrev bool) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		other := m.cur
		if !hubIsPrev {
			other = m.prev
		}
		parts = append(parts, fmt.Sprintf("%q(%d)", other.label, len(other.items)))
	}
	return fmt.Sprintf("run %d %s: %q(%d) <-> %s", runID, kind, hub.label, len(hub.items), strings.Join(parts, ", "))
}

// printLabelAgreement asks: among topics that clearly survived, how often did
// the LLM produce the identical label again? The gap is the flicker that label
// inheritance would remove, and the LLM calls it would save.
func printLabelAgreement(runs []*topicRun, threshold float64) {
	var survived, same, surviving09, same09 int
	for i := 1; i < len(runs); i++ {
		for cur, m := range mutualBest(matches(runs[i-1], runs[i])) {
			if m.jaccard < threshold {
				continue
			}
			survived++
			equal := strings.EqualFold(strings.TrimSpace(cur.label), strings.TrimSpace(m.prev.label))
			if equal {
				same++
			}
			if m.jaccard >= strongJaccard {
				surviving09++
				if equal {
					same09++
				}
			}
		}
	}
	fmt.Printf("== Label agreement among survivors\n")
	fmt.Printf("  Jaccard >= %.2f: %d survivors, %d identical labels (%.1f%%)\n",
		threshold, survived, same, ratio(same, survived))
	fmt.Printf("  Jaccard >= 0.90: %d survivors, %d identical labels (%.1f%%)\n\n",
		surviving09, same09, ratio(same09, surviving09))
}

func ratio(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) * percent / float64(total)
}

type thread struct {
	topics []*topic
	first  int // index into runs
}

// printThreads follows mutual-best links at the threshold across the whole run
// sequence, then reports how long threads live and how their labels drift.
func printThreads(runs []*topicRun, threshold float64, top int) {
	threadOf := make(map[*topic]*thread)
	var threads []*thread
	for _, t := range runs[0].topics {
		th := &thread{topics: []*topic{t}}
		threadOf[t], threads = th, append(threads, th)
	}
	for i := 1; i < len(runs); i++ {
		mutual := mutualBest(matches(runs[i-1], runs[i]))
		for _, cur := range runs[i].topics {
			if m, ok := mutual[cur]; ok && m.jaccard >= threshold {
				th := threadOf[m.prev]
				th.topics = append(th.topics, cur)
				threadOf[cur] = th
				continue
			}
			th := &thread{topics: []*topic{cur}, first: i}
			threadOf[cur], threads = th, append(threads, th)
		}
	}

	lengths := make(map[int]int)
	fullSpan := 0
	for _, th := range threads {
		lengths[len(th.topics)]++
		if len(th.topics) == len(runs) {
			fullSpan++
		}
	}
	fmt.Printf("== Threads at Jaccard >= %.2f: %d threads over %d runs, %d span every run\n",
		threshold, len(threads), len(runs), fullSpan)
	fmt.Printf("  thread length (runs) -> count: ")
	keys := make([]int, 0, len(lengths))
	for k := range lengths {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		fmt.Printf("%d:%d ", k, lengths[k])
	}
	fmt.Println()

	sort.Slice(threads, func(a, b int) bool { return len(threads[a].topics) > len(threads[b].topics) })
	fmt.Printf("\n  longest %d threads (distinct consecutive labels, sizes first->last):\n", top)
	for i, th := range threads {
		if i >= top {
			break
		}
		fmt.Printf("  %3d runs  %s..%s  size %d->%d  %d labels: %s\n",
			len(th.topics), runs[th.first].at.Format("01-02 15:04"),
			runs[th.first+len(th.topics)-1].at.Format("01-02 15:04"),
			len(th.topics[0].items), len(th.topics[len(th.topics)-1].items),
			len(distinctLabels(th)), joinLabels(distinctLabels(th)))
	}
	fmt.Println()
}

func distinctLabels(th *thread) []string {
	var out []string
	for _, t := range th.topics {
		if len(out) == 0 || !strings.EqualFold(out[len(out)-1], t.label) {
			out = append(out, t.label)
		}
	}
	return out
}

func joinLabels(labels []string) string {
	if len(labels) > maxLabelsPerThread {
		labels = append(labels[:maxLabelsPerThread:maxLabelsPerThread], "...")
	}
	return strings.Join(labels, " -> ")
}

// printPairs is the verbose dump: every successor's best predecessor for every
// adjacent run pair, so individual decisions can be checked by eye.
func printPairs(runs []*topicRun, threshold float64) {
	fmt.Println("== Per-pair best matches (verbose)")
	for i := 1; i < len(runs); i++ {
		ms := matches(runs[i-1], runs[i])
		byCur, mutual := bestByCur(ms), mutualBest(ms)
		fmt.Printf("-- run %d -> %d\n", runs[i-1].id, runs[i].id)
		for _, cur := range runs[i].topics {
			m, ok := byCur[cur]
			if !ok {
				fmt.Printf("   EMERGED   %q(%d)\n", cur.label, len(cur.items))
				continue
			}
			state := "attached"
			if _, isMutual := mutual[cur]; isMutual {
				state = "mutual  "
			}
			if m.jaccard < threshold {
				state = "below   "
			}
			fmt.Printf("   %s  J=%.3f shared=%-3d %q(%d) <- %q(%d)\n", state, m.jaccard, m.shared,
				cur.label, len(cur.items), m.prev.label, len(m.prev.items))
		}
	}
}
