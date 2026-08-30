package main

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
)

func TestNewContainerStatFromResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		in   container.StatsResponse
		want ContainerStat
	}{
		{
			name: "known deltas yield expected percent",
			id:   "c1",
			in: container.StatsResponse{
				CPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 150},
					SystemUsage: 2000,
					OnlineCPUs:  4,
				},
				PreCPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 100},
					SystemUsage: 1000,
				},
				MemoryStats: container.MemoryStats{Usage: 1024, Limit: 4096},
			},
			want: ContainerStat{ID: "c1", CPUPercent: 20.0, MemUsage: 1024, MemLimit: 4096},
		},
		{
			name: "sysDelta zero yields zero percent",
			id:   "c2",
			in: container.StatsResponse{
				CPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 150},
					SystemUsage: 1000,
					OnlineCPUs:  4,
				},
				PreCPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 100},
					SystemUsage: 1000,
				},
				MemoryStats: container.MemoryStats{Usage: 512, Limit: 2048},
			},
			want: ContainerStat{ID: "c2", CPUPercent: 0, MemUsage: 512, MemLimit: 2048},
		},
		{
			name: "empty pre cpu stats yields zero percent",
			id:   "c3",
			in: container.StatsResponse{
				MemoryStats: container.MemoryStats{Usage: 256, Limit: 1024},
			},
			want: ContainerStat{ID: "c3", CPUPercent: 0, MemUsage: 256, MemLimit: 1024},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newContainerStatFromResponse(tc.id, tc.in)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestFormatMemBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   uint64
		want string
	}{
		{name: "45.2MB", in: 45_200_000, want: "45.2MB"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatMemBytes(tc.in)

			require.Equal(t, tc.want, got)
		})
	}
}
