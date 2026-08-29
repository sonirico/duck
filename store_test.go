package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestContainer(id, name string) Container {
	return Container{ID: id, Name: name, Image: "img", State: "running"}
}

func TestStore(t *testing.T) {
	t.Parallel()

	t.Run("List returns containers sorted by name", func(t *testing.T) {
		t.Parallel()
		s := NewStore()
		s.SetAll([]Container{newTestContainer("2", "beta"), newTestContainer("1", "alpha")})

		got := s.List()

		require.Len(t, got, 2)
		require.Equal(t, "alpha", got[0].Name)
		require.Equal(t, "beta", got[1].Name)
	})

	t.Run("Upsert replaces an existing container by ID", func(t *testing.T) {
		t.Parallel()
		s := NewStore()
		s.SetAll([]Container{newTestContainer("1", "alpha")})

		updated := newTestContainer("1", "alpha")
		updated.State = "exited"
		s.Upsert(updated)

		got := s.List()
		require.Len(t, got, 1)
		require.Equal(t, "exited", got[0].State)
	})

	t.Run("Delete removes a container by ID", func(t *testing.T) {
		t.Parallel()
		s := NewStore()
		s.SetAll([]Container{newTestContainer("1", "alpha"), newTestContainer("2", "beta")})

		s.Delete("1")

		got := s.List()
		require.Len(t, got, 1)
		require.Equal(t, "beta", got[0].Name)
	})

	t.Run("SetAll replaces the previous contents", func(t *testing.T) {
		t.Parallel()
		s := NewStore()
		s.SetAll([]Container{newTestContainer("1", "alpha")})

		s.SetAll([]Container{newTestContainer("2", "beta")})

		got := s.List()
		require.Len(t, got, 1)
		require.Equal(t, "beta", got[0].Name)
	})
}
