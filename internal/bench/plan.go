package bench

import "fmt"

// Sets are the two independent sets of a baseline campaign (DECISIONS.md
// D-007), run in this order.
var Sets = []string{"A", "B"}

// PlanEntry is one atlasbench invocation of a baseline campaign.
type PlanEntry struct {
	Set      string
	Round    int    // 1-based
	Position int    // 1-based position within the round
	Seed     uint64 // the round's shuffle seed
	Workload string
}

// Plan returns the run order of a baseline campaign (BENCHMARKS.md §5): for
// each set in Sets, rounds rounds, each running every name once in an order
// shuffled with its own seed. The round seeds are the successive outputs of
// splitmix64 started at master (set A's rounds, then set B's), so the whole
// plan is a function of names, rounds, and master. The generator and the
// shuffle are written out here, not taken from math/rand, so that a recorded
// seed reproduces its order under any Go version.
func Plan(names []string, rounds int, master uint64) []PlanEntry {
	seeds := splitmix64{master}
	var plan []PlanEntry
	for _, set := range Sets {
		for r := 1; r <= rounds; r++ {
			seed := seeds.next()
			for i, name := range shuffle(names, seed) {
				plan = append(plan, PlanEntry{Set: set, Round: r, Position: i + 1, Seed: seed, Workload: name})
			}
		}
	}
	return plan
}

// String formats e as one tab-separated plan line, in the column order of
// PlanHeader.
func (e PlanEntry) String() string {
	return fmt.Sprintf("%s\t%d\t%d\t%d\t%s", e.Set, e.Round, e.Position, e.Seed, e.Workload)
}

// PlanHeader names the columns of PlanEntry.String.
const PlanHeader = "set\tround\tposition\tseed\tworkload"

// shuffle returns a copy of names in a uniformly random order drawn from
// seed (Fisher–Yates).
func shuffle(names []string, seed uint64) []string {
	out := append([]string(nil), names...)
	g := splitmix64{seed}
	for i := len(out) - 1; i > 0; i-- {
		j := g.below(uint64(i) + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// splitmix64 is Steele, Lea, and Flood's SplitMix64 generator.
type splitmix64 struct{ state uint64 }

func (g *splitmix64) next() uint64 {
	g.state += 0x9e3779b97f4a7c15
	z := g.state
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

// below returns a uniform value in [0, n), n > 0, rejecting the 2^64 mod n
// lowest outputs so that every value is equally likely.
func (g *splitmix64) below(n uint64) uint64 {
	lo := -n % n
	for {
		if x := g.next(); x >= lo {
			return x % n
		}
	}
}
