package main

import (
	"testing"

	"github.com/moby/moby/api/types/volume"
	"github.com/stretchr/testify/require"
)

func TestNewVolumeFromSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   volume.Volume
		want Volume
	}{
		{
			name: "with labels",
			in: volume.Volume{
				Name:       "data",
				Driver:     "local",
				Mountpoint: "/var/lib/docker/volumes/data/_data",
				CreatedAt:  "2024-01-01T00:00:00Z",
				Labels:     map[string]string{"env": "prod"},
			},
			want: Volume{
				Name:       "data",
				Driver:     "local",
				Mountpoint: "/var/lib/docker/volumes/data/_data",
				Created:    "2024-01-01T00:00:00Z",
				Labels:     map[string]string{"env": "prod"},
			},
		},
		{
			name: "without labels",
			in: volume.Volume{
				Name:       "cache",
				Driver:     "local",
				Mountpoint: "/var/lib/docker/volumes/cache/_data",
				CreatedAt:  "2024-02-01T00:00:00Z",
			},
			want: Volume{
				Name:       "cache",
				Driver:     "local",
				Mountpoint: "/var/lib/docker/volumes/cache/_data",
				Created:    "2024-02-01T00:00:00Z",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newVolumeFromSummary(tc.in)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestVolumeUsedBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		volumes    []Volume
		containers []Container
		want       map[string]int
	}{
		{
			name:       "volume without usage",
			volumes:    []Volume{{Name: "data"}},
			containers: []Container{{ID: "1", Name: "app"}},
			want:       map[string]int{"data": 0},
		},
		{
			name:       "volume used by one container",
			volumes:    []Volume{{Name: "data"}},
			containers: []Container{{ID: "1", Name: "app", Volumes: []string{"data"}}},
			want:       map[string]int{"data": 1},
		},
		{
			name:    "volume used by two containers",
			volumes: []Volume{{Name: "data"}},
			containers: []Container{
				{ID: "1", Name: "app", Volumes: []string{"data"}},
				{ID: "2", Name: "worker", Volumes: []string{"data"}},
			},
			want: map[string]int{"data": 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := volumeUsedBy(tc.volumes, tc.containers)

			require.Equal(t, tc.want, got)
		})
	}
}
