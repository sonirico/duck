package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsedByNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		keys       []string
		containers []Container
		want       map[string]int
	}{
		{
			name:       "key without usage",
			keys:       []string{"data"},
			containers: []Container{{ID: "1", Name: "app"}},
			want:       map[string]int{"data": 0},
		},
		{
			name: "key used by two containers",
			keys: []string{"data"},
			containers: []Container{
				{ID: "1", Name: "app", Volumes: []string{"data"}},
				{ID: "2", Name: "worker", Volumes: []string{"data"}},
			},
			want: map[string]int{"data": 2},
		},
		{
			name:       "name not in keys is ignored",
			keys:       []string{"data"},
			containers: []Container{{ID: "1", Name: "app", Volumes: []string{"other"}}},
			want:       map[string]int{"data": 0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := usedByNames(tc.keys, tc.containers, func(c Container) []string { return c.Volumes })

			assert.Equal(t, tc.want, got)
		})
	}
}
