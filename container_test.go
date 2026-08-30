package main

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func TestNewContainerFromSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   container.Summary
		want Container
	}{
		{
			name: "with name and volume mount",
			in: container.Summary{
				ID:     "c1",
				Names:  []string{"/web"},
				Image:  "nginx",
				State:  "running",
				Status: "Up 2 minutes",
				Labels: map[string]string{
					"com.docker.compose.project": "proj",
					"com.docker.compose.service": "svc",
				},
				Mounts: []container.MountPoint{
					{Type: mount.TypeVolume, Name: "data"},
					{Type: mount.TypeBind, Name: "ignored"},
				},
			},
			want: Container{
				ID:      "c1",
				Name:    "web",
				Image:   "nginx",
				State:   "running",
				Status:  "Up 2 minutes",
				Project: "proj",
				Service: "svc",
				Volumes: []string{"data"},
			},
		},
		{
			name: "without name",
			in:   container.Summary{ID: "c2"},
			want: Container{ID: "c2", Name: "", Volumes: []string{}},
		},
		{
			name: "without mounts",
			in:   container.Summary{ID: "c3", Names: []string{"/db"}},
			want: Container{ID: "c3", Name: "db", Volumes: []string{}},
		},
		{
			name: "with network settings",
			in: container.Summary{
				ID:    "c4",
				Names: []string{"/api"},
				NetworkSettings: &container.NetworkSettingsSummary{
					Networks: map[string]*network.EndpointSettings{
						"web":     {},
						"backend": {},
					},
				},
			},
			want: Container{ID: "c4", Name: "api", Volumes: []string{}, Networks: []string{"backend", "web"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newContainerFromSummary(tc.in)

			require.Equal(t, tc.want, got)
		})
	}
}
