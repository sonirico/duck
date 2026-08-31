package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/moby/moby/api/types/container"
)

const statsInterval = 2 * time.Second

type ContainerStat struct {
	ID         string
	CPUPercent float64
	MemUsage   uint64
	MemLimit   uint64
}

type statsMsg struct {
	stats []ContainerStat
}

type statsTickMsg struct{}

func statsTickCmd() tea.Cmd {
	return tea.Tick(statsInterval, func(time.Time) tea.Msg { return statsTickMsg{} })
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
