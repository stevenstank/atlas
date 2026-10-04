package check

import (
	"context"
	"runtime/metrics"
	"testing"

	"github.com/stevenstank/atlas/models"
)

// An engine panic raised inside emit (here: a nil store) must propagate,
// not be reported as a ModelError, even though model code is on the stack.
func TestEnginePanicIsNotAModelError(t *testing.T) {
	e := &engine[models.Jug, string]{ctx: context.Background(), m: models.Jugs{}, k: 1}
	defer func() {
		if recover() == nil {
			t.Fatal("engine panic was swallowed")
		}
	}()
	res := e.run()
	t.Fatalf("run returned %v instead of panicking", res.Status)
}

// MaxHeapBytes depends on this metric; if it disappeared, the limit would
// silently never fire.
func TestHeapMetricExists(t *testing.T) {
	for _, d := range metrics.All() {
		if d.Name == "/memory/classes/heap/objects:bytes" {
			if d.Kind != metrics.KindUint64 {
				t.Fatalf("kind %v, want Uint64", d.Kind)
			}
			return
		}
	}
	t.Fatal("heap objects metric not found")
}
