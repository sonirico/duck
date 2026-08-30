package main

import (
	"github.com/moby/moby/api/types/volume"
)

type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	Created    string
	Labels     map[string]string
}

type volumesMsg struct {
	volumes []Volume
}

func newVolumeFromSummary(v volume.Volume) Volume {
	return Volume{
		Name:       v.Name,
		Driver:     v.Driver,
		Mountpoint: v.Mountpoint,
		Created:    v.CreatedAt,
		Labels:     v.Labels,
	}
}

func volumeUsedBy(volumes []Volume, containers []Container) map[string]int {
	used := make(map[string]int, len(volumes))
	for _, v := range volumes {
		used[v.Name] = 0
	}
	for _, c := range containers {
		for _, name := range c.Volumes {
			if _, ok := used[name]; ok {
				used[name]++
			}
		}
	}
	return used
}
