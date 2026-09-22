package lineage

import (
	"reflect"
	"testing"
)

func seq(from, to int64) []int64 {
	out := make([]int64, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

func candidate(topicID, threadID int64, label string, items []int64) Candidate {
	return Candidate{TopicID: topicID, ThreadID: threadID, ThreadLabel: label, Hash: SetHash(items), Items: items}
}

func defaults() Options {
	return Options{AttachThreshold: DefaultAttachThreshold, InheritThreshold: DefaultInheritThreshold, Inherit: true}
}

func TestSetHashIsOrderIndependent(t *testing.T) {
	a := SetHash([]int64{3, 1, 2})
	b := SetHash([]int64{1, 2, 3})
	if a != b {
		t.Fatalf("hash depends on order: %q vs %q", a, b)
	}
	if len(a) != hashLength {
		t.Fatalf("hash length %d, want %d", len(a), hashLength)
	}
	if c := SetHash([]int64{1, 2, 4}); c == a {
		t.Fatal("hash did not change when one item changed")
	}
}

func TestJaccard(t *testing.T) {
	cases := []struct {
		name string
		a, b []int64
		want float64
	}{
		{"identical", seq(1, 4), seq(1, 4), 1},
		{"disjoint", seq(1, 4), seq(5, 8), 0},
		{"three of five", []int64{1, 2, 3, 4}, []int64{1, 2, 3, 5}, 0.6},
		{"both empty", nil, nil, 0},
		{"one empty", seq(1, 3), nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Jaccard(c.a, c.b); got != c.want {
				t.Fatalf("Jaccard = %v, want %v", got, c.want)
			}
		})
	}
}

func TestAssignExactMatchInheritsLabel(t *testing.T) {
	items := seq(1, 5)
	cands := []Candidate{candidate(10, 7, "Old Label", items)}

	got := Assign([][]int64{{5, 4, 3, 2, 1}}, cands, defaults())

	want := Assignment{Hash: SetHash(items), ThreadID: 7, Jaccard: 1, Label: "Old Label", Transition: TransitionSurvived}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAssignNearMatchInheritsAboveThreshold(t *testing.T) {
	// 9 of 10 shared: J = 9/11 ≈ 0.818 -- attaches but does not inherit.
	prev := seq(1, 10)
	cur := append(seq(1, 9), 11)
	got := Assign([][]int64{cur}, []Candidate{candidate(1, 3, "L", prev)}, defaults())
	if got[0].ThreadID != 3 || got[0].Label != "" || got[0].Transition != TransitionSurvived {
		t.Fatalf("9/10: got %+v", got[0])
	}

	// 19 of 20 shared: J = 19/21 ≈ 0.905 -- attaches and inherits.
	prev = seq(1, 20)
	cur = append(seq(1, 19), 21)
	got = Assign([][]int64{cur}, []Candidate{candidate(1, 3, "L", prev)}, defaults())
	if got[0].ThreadID != 3 || got[0].Label != "L" {
		t.Fatalf("19/20: got %+v", got[0])
	}
}

func TestAssignBelowAttachThresholdIsNew(t *testing.T) {
	prev := seq(1, 10)
	cur := append([]int64{1, 2}, seq(20, 27)...)
	got := Assign([][]int64{cur}, []Candidate{candidate(1, 3, "L", prev)}, defaults())
	want := Assignment{Hash: SetHash(cur), Transition: TransitionNew}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
}

func TestAssignContestedThreadGoesToLargestCluster(t *testing.T) {
	prev := seq(1, 10)
	small := seq(1, 5)             // J = 0.5
	large := append(seq(1, 7), 20) // J = 7/11 ≈ 0.636
	cands := []Candidate{candidate(1, 3, "L", prev)}

	// small listed first: processing order must not follow input order.
	got := Assign([][]int64{small, large}, cands, defaults())
	if got[1].ThreadID != 3 || got[1].Transition != TransitionSurvived {
		t.Fatalf("large cluster should own the thread: %+v", got[1])
	}
	if got[0].ThreadID != 0 || got[0].Transition != TransitionNew {
		t.Fatalf("small cluster should emerge: %+v", got[0])
	}
}

func TestAssignInheritDisabledStillAttaches(t *testing.T) {
	items := seq(1, 5)
	opts := defaults()
	opts.Inherit = false
	got := Assign([][]int64{items}, []Candidate{candidate(1, 9, "L", items)}, opts)
	if got[0].ThreadID != 9 || got[0].Jaccard != 1 || got[0].Label != "" {
		t.Fatalf("got %+v", got[0])
	}
}

func TestAssignIsDeterministic(t *testing.T) {
	cands := []Candidate{
		candidate(1, 1, "A", seq(1, 10)),
		candidate(2, 2, "B", seq(20, 30)),
		candidate(3, 3, "C", seq(40, 45)),
	}
	clusters := [][]int64{seq(1, 10), append(seq(20, 29), 99), seq(40, 45), seq(100, 105)}

	first := Assign(clusters, cands, defaults())
	second := Assign(clusters, cands, defaults())
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same input, different output:\n%+v\n%+v", first, second)
	}

	permuted := [][]int64{clusters[3], clusters[1], clusters[0], clusters[2]}
	got := Assign(permuted, cands, defaults())
	wantOrder := []Assignment{first[3], first[1], first[0], first[2]}
	if !reflect.DeepEqual(got, wantOrder) {
		t.Fatalf("permuting clusters changed per-cluster results:\n%+v\n%+v", got, wantOrder)
	}
}

func TestAssignPrefersMostRecentCandidateForHash(t *testing.T) {
	items := seq(1, 5)
	cands := []Candidate{
		candidate(5, 2, "Older", items),
		candidate(9, 2, "Newer", items),
	}
	got := Assign([][]int64{items}, cands, defaults())
	if got[0].Label != "Newer" {
		t.Fatalf("inherited %q, want the most recent candidate's label", got[0].Label)
	}
}
