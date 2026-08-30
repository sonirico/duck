package main

import (
	"github.com/moby/moby/api/types/network"
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

type Network struct {
	ID     string
	Name   string
	Driver string
	Subnet string
}

type networksMsg struct {
	networks []Network
}

func newNetworkFromSummary(s network.Summary) Network {
	subnet := ""
	if len(s.IPAM.Config) > 0 && s.IPAM.Config[0].Subnet.IsValid() {
		subnet = s.IPAM.Config[0].Subnet.String()
	}
	return Network{
		ID:     s.ID,
		Name:   s.Name,
		Driver: s.Driver,
		Subnet: subnet,
	}
}

func networkUsedBy(networks []Network, containers []Container) map[string]int {
	used := make(map[string]int, len(networks))
	for _, n := range networks {
		used[n.Name] = 0
	}
	for _, c := range containers {
		for _, name := range c.Networks {
			if _, ok := used[name]; ok {
				used[name]++
			}
		}
	}
	return used
}
