package renderer

import (
	"fmt"
	"html/template"
	"sort"
	"strconv"
	"strings"

	"github.com/lmorchard/feedspool-go/internal/database"
	"github.com/lmorchard/feedspool-go/internal/trends"
)

// Sparkline geometry, ported from the slice 3 mockup.
const (
	sparkBarWidth = 6
	sparkBarGap   = 2
	sparkHeight   = 18
)

// TopicStatusGroup is one status heading on the topics page and its topics.
type TopicStatusGroup struct {
	Status string // "" only when trends could not be loaded
	Topics []*database.Topic
}

// groupTopicsByStatus buckets topics by trend status in trends.StatusOrder,
// omitting empty groups. Within a group: last 24h descending, then item count
// (Score) descending, then label -- the order chosen in the mockup.
func groupTopicsByStatus(topicList []*database.Topic, tr map[int64]trends.Trend) []TopicStatusGroup {
	byStatus := make(map[string][]*database.Topic)
	for _, t := range topicList {
		s := tr[t.ID].Status
		byStatus[s] = append(byStatus[s], t)
	}

	var groups []TopicStatusGroup
	for _, status := range trends.StatusOrder() {
		list := byStatus[status]
		if len(list) == 0 {
			continue
		}
		sort.SliceStable(list, func(i, j int) bool {
			a, b := tr[list[i].ID], tr[list[j].ID]
			if a.Last24h != b.Last24h {
				return a.Last24h > b.Last24h
			}
			if list[i].Score != list[j].Score {
				return list[i].Score > list[j].Score
			}
			return list[i].Label < list[j].Label
		})
		groups = append(groups, TopicStatusGroup{Status: status, Topics: list})
	}
	return groups
}

// statusTally summarizes groups for the page's meta line: "2 new · 3 growing".
func statusTally(groups []TopicStatusGroup) string {
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		if g.Status != "" {
			parts = append(parts, fmt.Sprintf("%d %s", len(g.Topics), g.Status))
		}
	}
	return strings.Join(parts, " · ")
}

// sparklineSVG draws one bar per day, scaled to the topic's own peak with a
// 1px minimum; the last (most recent) bar carries class "last". Every value
// interpolated is an integer, so the markup is safe to return as HTML.
func sparklineSVG(daily []int) template.HTML {
	if len(daily) == 0 {
		return ""
	}
	peak := 1
	counts := make([]string, len(daily))
	for i, v := range daily {
		peak = max(peak, v)
		counts[i] = strconv.Itoa(v)
	}
	list := strings.Join(counts, ", ")
	width := len(daily)*(sparkBarWidth+sparkBarGap) - sparkBarGap

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="spark-svg" width="%d" height="%d" role="img" aria-label="items per day: %s">`,
		width, sparkHeight, list)
	fmt.Fprintf(&b, `<title>%s per day, oldest first</title>`, list)
	for i, v := range daily {
		h := max(1, (v*sparkHeight*2+peak)/(2*peak)) // rounded v/peak*height
		class := ""
		if i == len(daily)-1 {
			class = `class="last" `
		}
		fmt.Fprintf(&b, `<rect %sx="%d" y="%d" width="%d" height="%d" rx="1"/>`,
			class, i*(sparkBarWidth+sparkBarGap), sparkHeight-h, sparkBarWidth, h)
	}
	b.WriteString(`</svg>`)
	// #nosec G203 -- built only from integers above; no caller-supplied text.
	return template.HTML(b.String())
}

// trendDelta shows the last 24h against the 24h before (counts, not a Trend,
// because a template cannot take the address of a map value): "▲3", "▼3" or "·".
func trendDelta(last, prior int) string {
	switch d := last - prior; {
	case d > 0:
		return "▲" + strconv.Itoa(d)
	case d < 0:
		return "▼" + strconv.Itoa(-d)
	default:
		return "·"
	}
}

// trendDeltaClass is the CSS class that colors trendDelta, taken from the
// topic's status so the arrow always agrees with the badge beside it: a
// "steady" topic's ▲1 is shown but left uncolored, whatever margin is set.
func trendDeltaClass(status string) string {
	switch status {
	case trends.StatusGrowing:
		return "delta-up"
	case trends.StatusFading:
		return "delta-down"
	default:
		return ""
	}
}
