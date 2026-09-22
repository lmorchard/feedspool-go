package renderer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/trends"
)

func TestGroupTopicsByStatus(t *testing.T) {
	mk := func(id int64, label string, score float64) *database.Topic {
		return &database.Topic{ID: id, Label: label, Score: score}
	}
	topicList := []*database.Topic{
		mk(1, "Quiet one", 9), mk(2, "New B", 3), mk(3, "Grow low", 8),
		mk(4, "Grow high", 2), mk(5, "Fade", 4), mk(6, "New A", 3), mk(7, "Grow tie", 8),
	}
	tr := map[int64]trends.Trend{
		1: {Status: trends.StatusQuiet},
		2: {Status: trends.StatusNew, Last24h: 1},
		3: {Status: trends.StatusGrowing, Last24h: 2},
		4: {Status: trends.StatusGrowing, Last24h: 5},
		5: {Status: trends.StatusFading, Last24h: 1},
		6: {Status: trends.StatusNew, Last24h: 1},
		7: {Status: trends.StatusGrowing, Last24h: 2},
	}

	got := groupTopicsByStatus(topicList, tr)

	statuses := make([]string, 0, len(got))
	labels := map[string][]string{}
	for _, g := range got {
		statuses = append(statuses, g.Status)
		for _, topic := range g.Topics {
			labels[g.Status] = append(labels[g.Status], topic.Label)
		}
	}
	if want := []string{trends.StatusNew, trends.StatusGrowing, trends.StatusFading, trends.StatusQuiet}; !reflect.DeepEqual(statuses, want) {
		t.Fatalf("group order = %v, want %v (empty steady omitted)", statuses, want)
	}
	// last 24h desc, then score desc, then label.
	if want := []string{"Grow high", "Grow low", "Grow tie"}; !reflect.DeepEqual(labels[trends.StatusGrowing], want) {
		t.Errorf("growing order = %v, want %v", labels[trends.StatusGrowing], want)
	}
	if want := []string{"New A", "New B"}; !reflect.DeepEqual(labels[trends.StatusNew], want) {
		t.Errorf("new order = %v, want %v (ties broken by label)", labels[trends.StatusNew], want)
	}
}

func TestStatusTally(t *testing.T) {
	groups := []TopicStatusGroup{
		{Status: "new", Topics: make([]*database.Topic, 2)},
		{Status: "growing", Topics: make([]*database.Topic, 3)},
		{Status: "quiet", Topics: make([]*database.Topic, 1)},
	}
	if got, want := statusTally(groups), "2 new · 3 growing · 1 quiet"; got != want {
		t.Fatalf("statusTally = %q, want %q", got, want)
	}
	if got := statusTally(nil); got != "" {
		t.Fatalf("statusTally(nil) = %q, want empty", got)
	}
}

func TestSparklineSVG(t *testing.T) {
	got := string(sparklineSVG([]int{0, 2, 4}))
	for _, want := range []string{
		`<svg class="spark-svg" width="22" height="18"`,
		`aria-label="items per day: 0, 2, 4"`,
		`<title>0, 2, 4 per day, oldest first</title>`,
		`<rect x="0" y="17" width="6" height="1" rx="1"/>`,
		`<rect x="8" y="9" width="6" height="9" rx="1"/>`,
		`<rect class="last" x="16" y="0" width="6" height="18" rx="1"/>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("sparkline missing %q\n got: %s", want, got)
		}
	}
	if n := strings.Count(got, "<rect"); n != 3 {
		t.Errorf("%d bars, want 3", n)
	}
	if got := sparklineSVG(nil); got != "" {
		t.Errorf("sparklineSVG(nil) = %q, want empty", got)
	}
	if got := string(sparklineSVG([]int{0, 0})); strings.Count(got, `height="1"`) != 2 {
		t.Errorf("all-zero bars should be 1px: %s", got)
	}
}

func TestTrendDelta(t *testing.T) {
	cases := []struct {
		last, prior int
		text        string
	}{
		{5, 2, "▲3"},
		{1, 4, "▼3"},
		{2, 2, "·"},
	}
	for _, c := range cases {
		if got := trendDelta(c.last, c.prior); got != c.text {
			t.Errorf("trendDelta(%d/%d) = %q, want %q", c.last, c.prior, got, c.text)
		}
	}
}

// The arrow is colored by the status badge beside it, so the two cannot
// disagree whatever growth margin is configured.
func TestTrendDeltaClass(t *testing.T) {
	cases := map[string]string{
		trends.StatusGrowing: "delta-up",
		trends.StatusFading:  "delta-down",
		trends.StatusSteady:  "",
		trends.StatusNew:     "",
		trends.StatusQuiet:   "",
	}
	for status, want := range cases {
		if got := trendDeltaClass(status); got != want {
			t.Errorf("trendDeltaClass(%q) = %q, want %q", status, got, want)
		}
	}
}
