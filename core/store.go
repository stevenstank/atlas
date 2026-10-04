package core

import "math"

// StateID is the dense index of an admitted state, in admission order.
type StateID uint32

// NoParent is the parent of an initial state.
const NoParent StateID = math.MaxUint32

// MaxStoreLen is the largest number of states a Store can hold.
const MaxStoreLen = math.MaxUint32

// Store is the baseline visited set (D-003): exact lookup by canonical key,
// plus parent, edge ordinal, and depth for each admitted state.
type Store struct {
	ids    map[string]StateID
	keys   []string
	parent []StateID
	edge   []uint32
	depth  []uint32
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{ids: make(map[string]StateID)}
}

// Len returns the number of admitted states.
func (s *Store) Len() int { return len(s.keys) }

// Lookup returns the ID of the state with the given key, if admitted.
func (s *Store) Lookup(key []byte) (StateID, bool) {
	id, ok := s.ids[string(key)]
	return id, ok
}

// Add admits a state with the given key and returns its ID. The caller must
// check Lookup first and must not exceed MaxStoreLen.
func (s *Store) Add(key []byte, parent StateID, edge, depth uint32) StateID {
	id := StateID(len(s.keys))
	k := string(key)
	s.ids[k] = id
	s.keys = append(s.keys, k)
	s.parent = append(s.parent, parent)
	s.edge = append(s.edge, edge)
	s.depth = append(s.depth, depth)
	return id
}

// Key returns the canonical key of id.
func (s *Store) Key(id StateID) string { return s.keys[id] }

// Parent returns the parent of id, or NoParent for an initial state.
func (s *Store) Parent(id StateID) StateID { return s.parent[id] }

// Edge returns the ordinal of the emission that admitted id, within its
// parent's Next call (or within Init for an initial state).
func (s *Store) Edge(id StateID) uint32 { return s.edge[id] }

// Depth returns the BFS depth of id.
func (s *Store) Depth(id StateID) uint32 { return s.depth[id] }
