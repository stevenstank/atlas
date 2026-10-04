package check

import (
	"testing"

	"github.com/stevenstank/atlas/core"
)

func TestAtCapacity(t *testing.T) {
	for _, c := range []struct {
		n    int
		want bool
	}{{0, false}, {core.MaxStoreLen - 1, false}, {core.MaxStoreLen, true}} {
		if got := atCapacity(c.n); got != c.want {
			t.Errorf("atCapacity(%d) = %v, want %v", c.n, got, c.want)
		}
	}
	// The last admissible ID must not collide with NoParent.
	if core.StateID(core.MaxStoreLen-1) == core.NoParent {
		t.Error("last StateID equals NoParent")
	}
}
