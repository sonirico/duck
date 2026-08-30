package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

type composeService struct {
	name        string
	image       string
	restart     string
	command     []string
	entrypoint  []string
	environment []string
	ports       []string
	volumes     []string
	networks    []string
	dependsOn   []string
}

type composeFile struct {
	services []composeService
	volumes  map[string]string
	networks map[string]string
}

func newComposeFile(containers []container.InspectResponse, images map[string]image.InspectResponse, project string) composeFile {
	f := composeFile{
		volumes:  make(map[string]string),
		networks: make(map[string]string),
	}

	for _, c := range containers {
		f.services = append(f.services, newComposeService(c, images, project, f.volumes, f.networks))
	}

	sort.Slice(f.services, func(i, j int) bool { return f.services[i].name < f.services[j].name })

	return f
}

func newComposeService(c container.InspectResponse, images map[string]image.InspectResponse, project string, volumes, networks map[string]string) composeService {
	svc := composeService{
		name:  composeServiceName(c),
		image: c.Config.Image,
	}

	img := images[c.Config.Image]

	svc.command = composeDiff(c.Config.Cmd, img)
	svc.entrypoint = composeDiffEntrypoint(c.Config.Entrypoint, img)
	svc.environment = composeEnvDiff(c.Config.Env, img)
	svc.ports = composePorts(c.HostConfig)
	svc.volumes = composeVolumes(c.Mounts, project, volumes)
	svc.networks = composeNetworks(c.NetworkSettings, project, networks)
	svc.dependsOn = composeDependsOn(c.Config.Labels)
	svc.restart = composeRestart(c.HostConfig)

	return svc
}

func composeServiceName(c container.InspectResponse) string {
	if name := c.Config.Labels["com.docker.compose.service"]; name != "" {
		return name
	}
	return strings.TrimPrefix(c.Name, "/")
}

func composeDiff(cmd []string, img image.InspectResponse) []string {
	var imgCmd []string
	if img.Config != nil {
		imgCmd = img.Config.Cmd
	}
	if slices.Equal(cmd, imgCmd) {
		return nil
	}
	return cmd
}

func composeDiffEntrypoint(entrypoint []string, img image.InspectResponse) []string {
	var imgEntrypoint []string
	if img.Config != nil {
		imgEntrypoint = img.Config.Entrypoint
	}
	if slices.Equal(entrypoint, imgEntrypoint) {
		return nil
	}
	return entrypoint
}

func composeEnvDiff(env []string, img image.InspectResponse) []string {
	var imgEnv []string
	if img.Config != nil {
		imgEnv = img.Config.Env
	}
	seen := make(map[string]struct{}, len(imgEnv))
	for _, e := range imgEnv {
		seen[e] = struct{}{}
	}
	var result []string
	for _, e := range env {
		if _, ok := seen[e]; !ok {
			result = append(result, e)
		}
	}
	return result
}

func composePorts(hostConfig *container.HostConfig) []string {
	if hostConfig == nil {
		return nil
	}
	keys := make([]network.Port, 0, len(hostConfig.PortBindings))
	for k := range hostConfig.PortBindings {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })

	var ports []string
	for _, key := range keys {
		for _, binding := range hostConfig.PortBindings[key] {
			port := fmt.Sprintf("%s:%s", binding.HostPort, key.Port())
			if key.Proto() != network.TCP {
				port += "/" + string(key.Proto())
			}
			if binding.HostIP.IsValid() {
				port = binding.HostIP.String() + ":" + port
			}
			ports = append(ports, port)
		}
	}
	return ports
}

func composeVolumes(mounts []container.MountPoint, project string, namedVolumes map[string]string) []string {
	var volumes []string
	for _, m := range mounts {
		switch m.Type {
		case mount.TypeVolume:
			short := strings.TrimPrefix(m.Name, project+"_")
			v := fmt.Sprintf("%s:%s", short, m.Destination)
			if !m.RW {
				v += ":ro"
			}
			volumes = append(volumes, v)
			namedVolumes[short] = m.Name
		case mount.TypeBind:
			v := fmt.Sprintf("%s:%s", m.Source, m.Destination)
			if !m.RW {
				v += ":ro"
			}
			volumes = append(volumes, v)
		}
	}
	return volumes
}

func composeNetworks(networkSettings *container.NetworkSettings, project string, namedNetworks map[string]string) []string {
	if networkSettings == nil {
		return nil
	}
	keys := make([]string, 0, len(networkSettings.Networks))
	for k := range networkSettings.Networks {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var networks []string
	for _, k := range keys {
		if k == "bridge" {
			continue
		}
		var short string
		if k == project+"_default" {
			short = "default"
		} else {
			short = strings.TrimPrefix(k, project+"_")
		}
		networks = append(networks, short)
		if short != "default" {
			namedNetworks[short] = k
		}
	}
	return networks
}

func composeDependsOn(labels map[string]string) []string {
	dep := labels["com.docker.compose.depends_on"]
	if dep == "" {
		return nil
	}
	var dependsOn []string
	for _, entry := range strings.Split(dep, ",") {
		name, _, _ := strings.Cut(entry, ":")
		dependsOn = append(dependsOn, name)
	}
	return dependsOn
}

func composeRestart(hostConfig *container.HostConfig) string {
	if hostConfig == nil {
		return ""
	}
	name := string(hostConfig.RestartPolicy.Name)
	if name == "" || name == "no" {
		return ""
	}
	return name
}

func (f composeFile) render() string {
	var b strings.Builder

	b.WriteString("services:\n")
	for _, svc := range f.services {
		fmt.Fprintf(&b, "  %s:\n", svc.name)
		fmt.Fprintf(&b, "    image: %s\n", svc.image)
		writeComposeFlowList(&b, "command", svc.command)
		writeComposeFlowList(&b, "entrypoint", svc.entrypoint)
		writeComposeBlockList(&b, "environment", svc.environment)
		writeComposeBlockList(&b, "ports", svc.ports)
		writeComposeBlockList(&b, "volumes", svc.volumes)
		writeComposeBlockList(&b, "networks", svc.networks)
		writeComposeBlockList(&b, "depends_on", svc.dependsOn)
		if svc.restart != "" {
			fmt.Fprintf(&b, "    restart: %s\n", svc.restart)
		}
	}

	writeComposeNamedBlock(&b, "volumes", f.volumes)
	writeComposeNamedBlock(&b, "networks", f.networks)

	return b.String()
}

func writeComposeFlowList(b *strings.Builder, key string, items []string) {
	if len(items) == 0 {
		return
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = fmt.Sprintf("%q", item)
	}
	fmt.Fprintf(b, "    %s: [%s]\n", key, strings.Join(quoted, ", "))
}

func writeComposeBlockList(b *strings.Builder, key string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "    %s:\n", key)
	for _, item := range items {
		fmt.Fprintf(b, "      - %s\n", item)
	}
}

func writeComposeNamedBlock(b *strings.Builder, key string, entries map[string]string) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", key)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		real := entries[name]
		if real == name {
			fmt.Fprintf(b, "  %s: {}\n", name)
		} else {
			fmt.Fprintf(b, "  %s:\n    name: %s\n", name, real)
		}
	}
}
