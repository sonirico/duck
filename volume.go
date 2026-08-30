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
	keys := make([]string, 0, len(volumes))
	for _, v := range volumes {
		keys = append(keys, v.Name)
	}
	return usedByNames(keys, containers, func(c Container) []string { return c.Volumes })
}
