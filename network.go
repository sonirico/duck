package main

import (
	"github.com/moby/moby/api/types/network"
)

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
