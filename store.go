package main

import (
	"sort"
	"sync"
)

type Store[T any] struct {
	mu    sync.Mutex
	items map[string]T
	key   func(T) string
	less  func(a, b T) bool
}

func NewStore[T any](key func(T) string, less func(a, b T) bool) *Store[T] {
	return &Store[T]{items: make(map[string]T), key: key, less: less}
}

func (s *Store[T]) SetAll(ts []T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]T, len(ts))
	for _, t := range ts {
		s.items[s.key(t)] = t
	}
}

func (s *Store[T]) Upsert(t T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[s.key(t)] = t
}

func (s *Store[T]) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
}

func (s *Store[T]) List() []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]T, 0, len(s.items))
	for _, t := range s.items {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return s.less(out[i], out[j]) })
	return out
}
