package main

import (
	"sort"
	"sync"
)

type Container struct {
	ID      string
	Name    string
	Image   string
	State   string
	Status  string
	Project string
	Service string
}

type Store struct {
	mu         sync.Mutex
	containers map[string]Container
}

func NewStore() *Store {
	return &Store{containers: make(map[string]Container)}
}

func (s *Store) SetAll(cs []Container) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containers = make(map[string]Container, len(cs))
	for _, c := range cs {
		s.containers[c.ID] = c
	}
}

func (s *Store) Upsert(c Container) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containers[c.ID] = c
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.containers, id)
}

func (s *Store) List() []Container {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Container, 0, len(s.containers))
	for _, c := range s.containers {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
