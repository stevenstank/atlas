package core_test

import (
	"bytes"
	"testing"

	"github.com/stevenstank/atlas/core"
)

func TestQueueFIFOAcrossGrowthAndCompaction(t *testing.T) {
	var q core.Queue[int] // the zero value is usable
	next, want := 0, 0
	// Interleave pushes and pops so the head passes the compaction threshold
	// several times with items still queued.
	for round := range 50 {
		for range 100 + round {
			q.Push(next)
			next++
		}
		for range 90 {
			if got := q.Pop(); got != want {
				t.Fatalf("Pop = %d, want %d", got, want)
			}
			want++
		}
		if q.Len() != next-want {
			t.Fatalf("Len = %d, want %d", q.Len(), next-want)
		}
	}
	for q.Len() > 0 {
		if got := q.Pop(); got != want {
			t.Fatalf("drain: Pop = %d, want %d", got, want)
		}
		want++
	}
	if want != next {
		t.Fatalf("popped %d, pushed %d", want, next)
	}
}

func TestStoreRecordsAndCopiesKeys(t *testing.T) {
	s := core.NewStore()
	buf := []byte("a")
	a := s.Add(buf, core.NoParent, 3, 0)
	buf[0] = 'b' // the store must have copied the key
	b := s.Add(buf, a, 1, 1)
	if s.Len() != 2 || a != 0 || b != 1 {
		t.Fatalf("Len %d, ids %d %d", s.Len(), a, b)
	}
	if id, ok := s.Lookup([]byte("a")); !ok || id != a || s.Key(a) != "a" {
		t.Errorf("lookup a: %d %v %q", id, ok, s.Key(a))
	}
	if _, ok := s.Lookup([]byte("c")); ok {
		t.Error("found a key never added")
	}
	if s.Parent(a) != core.NoParent || s.Edge(a) != 3 || s.Depth(a) != 0 ||
		s.Parent(b) != a || s.Edge(b) != 1 || s.Depth(b) != 1 {
		t.Error("parent, edge, or depth not recorded")
	}
}

type fields struct {
	i int64
	u uint64
	s string
	b bool
}

func (f fields) key() []byte {
	k := core.AppendInt(nil, f.i)
	k = core.AppendString(k, f.s)
	k = core.AppendUint(k, f.u)
	return core.AppendBool(k, f.b)
}

// FuzzKeyHelpersInjective checks that a key built from several fields never
// collides for different field values (TESTING.md §9).
func FuzzKeyHelpersInjective(f *testing.F) {
	f.Add(int64(0), uint64(0), "", false, int64(0), uint64(0), "", true)
	f.Add(int64(1), uint64(1), "ab", false, int64(1), uint64(1), "a", false)
	f.Add(int64(-1), uint64(300), "a\x01", true, int64(-1), uint64(300), "a", true)
	f.Add(int64(128), uint64(2), "", false, int64(-129), uint64(2), "", false)
	f.Fuzz(func(t *testing.T, i1 int64, u1 uint64, s1 string, b1 bool, i2 int64, u2 uint64, s2 string, b2 bool) {
		x, y := fields{i1, u1, s1, b1}, fields{i2, u2, s2, b2}
		if (x == y) != bytes.Equal(x.key(), y.key()) {
			t.Fatalf("%+v and %+v: equal fields %v, equal keys %v", x, y, x == y, bytes.Equal(x.key(), y.key()))
		}
	})
}
