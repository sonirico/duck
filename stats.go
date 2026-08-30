package main

import (
	"fmt"

	"github.com/moby/moby/api/types/container"
)

type ContainerStat struct {
	ID         string
	CPUPercent float64
	MemUsage   uint64
	MemLimit   uint64
}

type statsMsg struct {
	stats []ContainerStat
}

func newContainerStatFromResponse(id string, r container.StatsResponse) ContainerStat {
	cpuPercent := 0.0

	cpuDelta := float64(r.CPUStats.CPUUsage.TotalUsage - r.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(r.CPUStats.SystemUsage - r.PreCPUStats.SystemUsage)

	if cpuDelta > 0 && sysDelta > 0 {
		cpuPercent = cpuDelta / sysDelta * float64(r.CPUStats.OnlineCPUs) * 100.0
	}

	return ContainerStat{
		ID:         id,
		CPUPercent: cpuPercent,
		MemUsage:   r.MemoryStats.Usage,
		MemLimit:   r.MemoryStats.Limit,
	}
}

func formatMemBytes(b uint64) string {
	return fmt.Sprintf("%.1fMB", float64(b)/1e6)
}
