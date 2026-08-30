package main

import (
	"net/netip"
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
				Ports:   []string{},
				Volumes: []string{"data"},
			},
		},
		{
			name: "without name",
			in:   container.Summary{ID: "c2"},
			want: Container{ID: "c2", Name: "", Ports: []string{}, Volumes: []string{}},
		},
		{
			name: "without mounts",
			in:   container.Summary{ID: "c3", Names: []string{"/db"}},
			want: Container{ID: "c3", Name: "db", Ports: []string{}, Volumes: []string{}},
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
			want: Container{ID: "c4", Name: "api", Ports: []string{}, Volumes: []string{}, Networks: []string{"backend", "web"}},
		},
		{
			name: "with published port",
			in: container.Summary{
				ID:    "c5",
				Names: []string{"/pub"},
				Ports: []container.PortSummary{
					{PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
				},
			},
			want: Container{ID: "c5", Name: "pub", Ports: []string{"8080:80/tcp"}, Volumes: []string{}},
		},
		{
			name: "with unpublished port",
			in: container.Summary{
				ID:    "c6",
				Names: []string{"/unpub"},
				Ports: []container.PortSummary{
					{PrivatePort: 80, Type: "tcp"},
				},
			},
			want: Container{ID: "c6", Name: "unpub", Ports: []string{"80/tcp"}, Volumes: []string{}},
		},
		{
			name: "with duplicate port binding across IPs",
			in: container.Summary{
				ID:    "c7",
				Names: []string{"/dup"},
				Ports: []container.PortSummary{
					{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
					{IP: netip.MustParseAddr("::"), PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
				},
			},
			want: Container{ID: "c7", Name: "dup", Ports: []string{"8080:80/tcp"}, Volumes: []string{}},
		},
		{
			name: "without ports",
			in:   container.Summary{ID: "c8", Names: []string{"/noport"}},
			want: Container{ID: "c8", Name: "noport", Ports: []string{}, Volumes: []string{}},
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
