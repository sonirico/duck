package main

import (
	"sort"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

type Container struct {
	ID       string
	Name     string
	Image    string
	State    string
	Status   string
	Project  string
	Service  string
	Volumes  []string
	Networks []string
}

func newContainerFromSummary(s container.Summary) Container {
	name := ""
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	}
	volumes := make([]string, 0)
	for _, m := range s.Mounts {
		if m.Type == mount.TypeVolume {
			volumes = append(volumes, m.Name)
		}
	}
	var networks []string
	if s.NetworkSettings != nil {
		networks = make([]string, 0, len(s.NetworkSettings.Networks))
		for name := range s.NetworkSettings.Networks {
			networks = append(networks, name)
		}
		sort.Strings(networks)
	}
	return Container{
		ID:       s.ID,
		Name:     name,
		Image:    s.Image,
		State:    string(s.State),
		Status:   s.Status,
		Project:  s.Labels["com.docker.compose.project"],
		Service:  s.Labels["com.docker.compose.service"],
		Volumes:  volumes,
		Networks: networks,
	}
}
