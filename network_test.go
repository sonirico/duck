package main

import (
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func TestNewNetworkFromSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   network.Summary
		want Network
	}{
		{
			name: "with subnet",
			in: network.Summary{
				Network: network.Network{
					ID:     "n1",
					Name:   "app-net",
					Driver: "bridge",
					IPAM: network.IPAM{
						Config: []network.IPAMConfig{
							{Subnet: netip.MustParsePrefix("172.18.0.0/16")},
						},
					},
				},
			},
			want: Network{
				ID:     "n1",
				Name:   "app-net",
				Driver: "bridge",
				Subnet: "172.18.0.0/16",
			},
		},
		{
			name: "without subnet",
			in: network.Summary{
				Network: network.Network{
					ID:     "n2",
					Name:   "host",
					Driver: "host",
				},
			},
			want: Network{
				ID:     "n2",
				Name:   "host",
				Driver: "host",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newNetworkFromSummary(tc.in)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestNetworkUsedBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		networks   []Network
		containers []Container
		want       map[string]int
	}{
		{
			name:       "network without usage",
			networks:   []Network{{Name: "app-net"}},
			containers: []Container{{ID: "1", Name: "app"}},
			want:       map[string]int{"app-net": 0},
		},
		{
			name:       "network used by one container",
			networks:   []Network{{Name: "app-net"}},
			containers: []Container{{ID: "1", Name: "app", Networks: []string{"app-net"}}},
			want:       map[string]int{"app-net": 1},
		},
		{
			name:     "network used by two containers",
			networks: []Network{{Name: "app-net"}},
			containers: []Container{
				{ID: "1", Name: "app", Networks: []string{"app-net"}},
				{ID: "2", Name: "worker", Networks: []string{"app-net"}},
			},
			want: map[string]int{"app-net": 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := networkUsedBy(tc.networks, tc.containers)

			require.Equal(t, tc.want, got)
		})
	}
}
