package trends

import (
	"reflect"
	"testing"
	"time"
	"unicode/utf8"
)

// windowEnd is the fixed anchor every test measures from.
func windowEnd() time.Time { return time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC) }

func at(d time.Duration) Item { return Item{FeedURL: "f", At: windowEnd().Add(-d)} }

func window(d time.Duration) *Input {
	return &Input{WindowStart: windowEnd().Add(-d), WindowEnd: windowEnd()}
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

func TestDailyBucketsSevenDayWindow(t *testing.T) {
	in := window(7 * day)
	in.Items = []Item{at(time.Hour), at(25 * time.Hour), at(25 * time.Hour), at(6*day + 23*time.Hour)}
	got := Compute(in).Daily
	if want := []int{1, 0, 0, 0, 0, 2, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Daily = %v, want %v", got, want)
	}
}

func TestDailyBucketsEdgeBelongsToEarlierBucket(t *testing.T) {
	in := window(7 * day)
	in.Items = []Item{at(day)}
	got := Compute(in).Daily
	n := len(got)
	if got[n-2] != 1 || got[n-1] != 0 {
		t.Fatalf("item exactly 24h old: Daily = %v, want it at index n-2", got)
	}

	in.Items = []Item{at(0)}
	if got := Compute(in).Daily; got[len(got)-1] != 1 {
		t.Fatalf("item at window end: Daily = %v, want it in the last bucket", got)
	}
}

func TestDailyBucketsClampsOutOfWindow(t *testing.T) {
	in := window(7 * day)
	in.Items = []Item{at(10 * day), at(-2 * time.Hour), at(3 * time.Hour)}
	got := Compute(in).Daily
	if got[0] != 1 {
		t.Errorf("item before the window should clamp to index 0: %v", got)
	}
	if got[len(got)-1] != 2 {
		t.Errorf("item after end should clamp into the last bucket: %v", got)
	}
	if sum(got) != len(in.Items) {
		t.Errorf("sum(Daily) = %d, want %d", sum(got), len(in.Items))
	}
}

func TestDailyBucketsPartialDay(t *testing.T) {
	if n := len(Compute(window(36 * time.Hour)).Daily); n != 2 {
		t.Errorf("36h window: %d buckets, want 2", n)
	}
	if n := len(Compute(window(time.Hour)).Daily); n != 1 {
		t.Errorf("1h window: %d buckets, want 1", n)
	}
	if n := len(Compute(window(0)).Daily); n != 1 {
		t.Errorf("empty window: %d buckets, want 1", n)
	}
}

func TestComputeLastAndPrior(t *testing.T) {
	in := window(7 * day)
	in.Items = []Item{at(time.Hour), at(2 * time.Hour), at(30 * time.Hour)}
	got := Compute(in)
	if got.Last24h != 2 || got.Prior24h != 1 {
		t.Fatalf("Last24h/Prior24h = %d/%d, want 2/1", got.Last24h, got.Prior24h)
	}

	in = window(12 * time.Hour)
	in.Items = []Item{at(time.Hour)}
	if got := Compute(in); got.Last24h != 1 || got.Prior24h != 0 {
		t.Fatalf("one-bucket window: Last24h/Prior24h = %d/%d, want 1/0", got.Last24h, got.Prior24h)
	}
}

func TestComputeDistinctFeeds(t *testing.T) {
	in := window(day)
	for _, f := range []string{"a", "b", "a", "c", "b"} {
		in.Items = append(in.Items, Item{FeedURL: f, At: windowEnd()})
	}
	if got := Compute(in).DistinctFeeds; got != 3 {
		t.Fatalf("DistinctFeeds = %d, want 3", got)
	}
}

func TestComputeDiff(t *testing.T) {
	in := window(day)
	in.ItemIDs = []int64{1, 2, 3, 4}
	in.PrevItemIDs = []int64{2, 3, 5}
	in.HasPrev = true
	if got := Compute(in); got.NewItems != 2 || got.DroppedItems != 1 {
		t.Fatalf("with previous: new/dropped = %d/%d, want 2/1", got.NewItems, got.DroppedItems)
	}

	in.HasPrev = false
	in.PrevItemIDs = nil
	if got := Compute(in); got.NewItems != 4 || got.DroppedItems != 0 {
		t.Fatalf("without previous: new/dropped = %d/%d, want 4/0", got.NewItems, got.DroppedItems)
	}
}

func TestComputeStatusPrecedence(t *testing.T) {
	cases := []struct {
		name         string
		firstSeenAgo time.Duration
		last, prior  int
		want         string
	}{
		{"new beats fading", 2 * time.Hour, 1, 5, StatusNew},
		{"growing", 3 * day, 6, 2, StatusGrowing},
		{"fading", 3 * day, 2, 6, StatusFading},
		{"steady", 3 * day, 3, 3, StatusSteady},
		{"one more is within the margin", 3 * day, 3, 2, StatusSteady},
		{"one fewer is within the margin", 3 * day, 2, 3, StatusSteady},
		{"growing at exactly the margin", 3 * day, 4, 2, StatusGrowing},
		{"fading at exactly the margin", 3 * day, 2, 4, StatusFading},
		{"nothing today, one yesterday is steady, not quiet", 3 * day, 0, 1, StatusSteady},
		{"quiet: nothing in either window", 3 * day, 0, 0, StatusQuiet},
		{"new beats quiet", 2 * time.Hour, 0, 0, StatusNew},
		{"new boundary is inclusive", day, 0, 0, StatusNew},
		{"just past the new boundary", day + time.Second, 0, 0, StatusQuiet},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := window(7 * day)
			in.ThreadFirstSeen = windowEnd().Add(-c.firstSeenAgo)
			for range c.last {
				in.Items = append(in.Items, at(time.Hour))
			}
			for range c.prior {
				in.Items = append(in.Items, at(30*time.Hour))
			}
			if got := Compute(in).Status; got != c.want {
				t.Fatalf("Status = %q, want %q", got, c.want)
			}
		})
	}
}

func TestComputeCarriesFirstSeen(t *testing.T) {
	in := window(day)
	in.ThreadFirstSeen = windowEnd().Add(-5 * day)
	if got := Compute(in).ThreadFirstSeen; !got.Equal(in.ThreadFirstSeen) {
		t.Fatalf("ThreadFirstSeen = %v, want %v", got, in.ThreadFirstSeen)
	}
}

func TestSparkline(t *testing.T) {
	cases := []struct {
		in   []int
		want string
	}{
		{[]int{0, 1, 2, 4, 8}, "▁▁▂▄█"},
		{[]int{0, 0, 0}, "▁▁▁"},
		{[]int{5}, "█"},
		{nil, ""},
	}
	for _, c := range cases {
		got := Sparkline(c.in)
		if got != c.want {
			t.Errorf("Sparkline(%v) = %q, want %q", c.in, got, c.want)
		}
		if utf8.RuneCountInString(got) != len(c.in) {
			t.Errorf("Sparkline(%v) has %d runes, want %d", c.in, utf8.RuneCountInString(got), len(c.in))
		}
	}
}

func TestStatusOrder(t *testing.T) {
	want := []string{StatusNew, StatusGrowing, StatusFading, StatusSteady, StatusQuiet}
	got := StatusOrder()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StatusOrder() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if StatusOrder()[0] != StatusNew {
		t.Fatal("StatusOrder returned a shared slice")
	}
}

func TestComputeHonorsGrowthMargin(t *testing.T) {
	mk := func(margin, last, prior int) string {
		in := window(7 * day)
		in.ThreadFirstSeen = windowEnd().Add(-3 * day)
		in.GrowthMargin = margin
		for range last {
			in.Items = append(in.Items, at(time.Hour))
		}
		for range prior {
			in.Items = append(in.Items, at(30*time.Hour))
		}
		return Compute(in).Status
	}
	if got := mk(1, 3, 2); got != StatusGrowing {
		t.Errorf("margin 1, 3 vs 2: %q, want growing", got)
	}
	if got := mk(3, 4, 2); got != StatusSteady {
		t.Errorf("margin 3, 4 vs 2: %q, want steady", got)
	}
	if got := mk(3, 5, 2); got != StatusGrowing {
		t.Errorf("margin 3, 5 vs 2: %q, want growing", got)
	}
	if got := mk(0, 3, 2); got != StatusSteady {
		t.Errorf("margin 0 means the default (%d), 3 vs 2: %q, want steady", DefaultGrowthMargin, got)
	}
}
