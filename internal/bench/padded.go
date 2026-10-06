package bench

// PaddedGrid is GridN with a constant payload, for B8 ("effect of state size
// alone"). A state is one byte per coordinate followed by Pad payload bytes;
// the payload never changes, so states, transitions, and depths are exactly
// GridN's: K^D states and D*(K-1)*K^(D-1) transitions. The payload is part
// of the state and of its key, so every successor copies it, stores it, and
// hashes it: state size changes while the reachable state space does not.
// Pad = 0 is the baseline. Coordinates are bytes, so K <= 256.
type PaddedGrid struct{ D, K, Pad int }

func (g PaddedGrid) Init(emit func([]byte) bool) {
	s := make([]byte, g.D+g.Pad)
	for i := range g.Pad {
		s[g.D+i] = byte(i) // a fixed pattern, so the payload is not all zero
	}
	emit(s)
}

func (g PaddedGrid) Next(s []byte, emit func(int, []byte) bool) {
	for i := range g.D {
		if int(s[i]) < g.K-1 {
			t := append([]byte(nil), s...)
			t[i]++
			if !emit(i, t) {
				return
			}
		}
	}
}

func (PaddedGrid) AppendKey(buf []byte, s []byte) []byte { return append(buf, s...) }
