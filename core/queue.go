package core

// Queue is a FIFO queue. The zero value is an empty queue.
type Queue[T any] struct {
	items []T
	head  int
}

// Len returns the number of queued items.
func (q *Queue[T]) Len() int { return len(q.items) - q.head }

// Push appends v.
func (q *Queue[T]) Push(v T) { q.items = append(q.items, v) }

// Pop removes and returns the oldest item. It panics if q is empty.
func (q *Queue[T]) Pop() T {
	v := q.items[q.head]
	var zero T
	q.items[q.head] = zero // release the reference for the GC
	q.head++
	switch {
	case q.head == len(q.items):
		q.items, q.head = q.items[:0], 0
	case q.head >= 1024 && 2*q.head >= len(q.items):
		n := copy(q.items, q.items[q.head:])
		clear(q.items[n:])
		q.items, q.head = q.items[:n], 0
	}
	return v
}
