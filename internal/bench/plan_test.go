package bench

import (
	"slices"
	"strings"
	"testing"
)

// The golden values below were computed with an independent Python
// implementation of SplitMix64 and of the shuffle. The seed-0 outputs are
// also the published SplitMix64 reference values. A change here changes
// every recorded campaign order.
func TestSplitmix64Golden(t *testing.T) {
	g := splitmix64{0}
	for i, want := range []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f} {
		if got := g.next(); got != want {
			t.Errorf("output %d = %#x, want %#x", i, got, want)
		}
	}
}

func TestPlanGolden(t *testing.T) {
	plan := Plan([]string{"a", "b", "c", "d", "e"}, 2, 1)
	for _, c := range []struct {
		set   string
		round int
		seed  uint64
		order []string
	}{
		{"A", 1, 10451216379200822465, []string{"a", "e", "b", "c", "d"}},
		{"A", 2, 13757245211066428519, []string{"c", "a", "e", "d", "b"}},
	} {
		var got []string
		for _, e := range plan {
			if e.Set == c.set && e.Round == c.round {
				if e.Seed != c.seed {
					t.Errorf("%s%d seed %d, want %d", c.set, c.round, e.Seed, c.seed)
				}
				got = append(got, e.Workload)
			}
		}
		if !slices.Equal(got, c.order) {
			t.Errorf("%s%d order %v, want %v", c.set, c.round, got, c.order)
		}
	}
}

// Every round of every set runs each workload exactly once, positions are
// 1..n, the plan is a function of the master seed, and no two rounds share
// a seed or (for the real suite) an order.
func TestPlan(t *testing.T) {
	var names []string
	for _, w := range Workloads() {
		names = append(names, w.Name())
	}
	const rounds = 10
	plan := Plan(names, rounds, 20261014)
	if len(plan) != len(Sets)*rounds*len(names) {
		t.Fatalf("plan has %d entries, want %d", len(plan), len(Sets)*rounds*len(names))
	}
	if !slices.Equal(plan, Plan(names, rounds, 20261014)) {
		t.Error("same master seed, different plan")
	}
	if slices.Equal(plan, Plan(names, rounds, 20261015)) {
		t.Error("different master seeds, same plan")
	}
	seeds := map[uint64]bool{}
	orders := map[string]bool{}
	for i := 0; i < len(plan); i += len(names) {
		round := plan[i : i+len(names)]
		var order []string
		for p, e := range round {
			if e.Set != round[0].Set || e.Round != round[0].Round || e.Seed != round[0].Seed || e.Position != p+1 {
				t.Fatalf("malformed round at entry %d: %+v", i+p, e)
			}
			order = append(order, e.Workload)
		}
		if sorted := slices.Sorted(slices.Values(order)); !slices.Equal(sorted, slices.Sorted(slices.Values(names))) {
			t.Errorf("%s%d is not a permutation of the workloads: %v", round[0].Set, round[0].Round, order)
		}
		if seeds[round[0].Seed] {
			t.Errorf("%s%d reuses seed %d", round[0].Set, round[0].Round, round[0].Seed)
		}
		seeds[round[0].Seed] = true
		k := strings.Join(order, "\x00")
		if orders[k] {
			t.Errorf("%s%d repeats an earlier order", round[0].Set, round[0].Round)
		}
		orders[k] = true
	}
	if want := []string{"A", "B"}; plan[0].Set != want[0] || plan[len(plan)-1].Set != want[1] || plan[len(plan)-1].Round != rounds {
		t.Errorf("sets out of order: first %+v, last %+v", plan[0], plan[len(plan)-1])
	}
}

// Each of the 6 orders of 3 names is about equally likely.
func TestShuffleUniform(t *testing.T) {
	counts := map[string]int{}
	const n = 60000
	for s := range uint64(n) {
		o := shuffle([]string{"x", "y", "z"}, s)
		counts[o[0]+o[1]+o[2]]++
	}
	if len(counts) != 6 {
		t.Fatalf("orders seen: %v", counts)
	}
	for o, c := range counts {
		if c < n/6-500 || c > n/6+500 { // about 5 standard deviations
			t.Errorf("order %s seen %d times of %d", o, c, n)
		}
	}
}
