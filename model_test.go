package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testLogRetargeter struct {
	targets []LogTarget
}

func newTestLogRetargeter() *testLogRetargeter { return &testLogRetargeter{} }

func (s *testLogRetargeter) SetTargets(ts []LogTarget) { s.targets = ts }

type testResourceCall struct {
	method string
	id     string
}

type testResourceClient struct {
	volumeRemove     func(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error)
	networkRemove    func(ctx context.Context, networkID string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error)
	imageRemove      func(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error)
	containerOpErr   error
	containerInspect client.ContainerInspectResult
	imageInspect     client.ImageInspectResult
	volumeInspect    client.VolumeInspectResult
	networkInspect   client.NetworkInspectResult
	containerStats   func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	calls            []testResourceCall
}

func newTestResourceClient(
	volumeRemove func(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error),
) *testResourceClient {
	return &testResourceClient{
		volumeRemove: volumeRemove,
		containerInspect: client.ContainerInspectResult{
			Container: container.InspectResponse{
				Name: "/web",
				Config: &container.Config{
					Image:  "nginx:latest",
					Labels: map[string]string{"com.docker.compose.service": "web"},
					Env:    []string{"FOO=bar"},
				},
			},
		},
		imageInspect: client.ImageInspectResult{},
	}
}

func newTestResourceClientWithContainerOpErr(err error) *testResourceClient {
	return &testResourceClient{containerOpErr: err}
}

func (c *testResourceClient) VolumeRemove(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error) {
	return c.volumeRemove(ctx, volumeID, options)
}

func (c *testResourceClient) NetworkRemove(ctx context.Context, networkID string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error) {
	return c.networkRemove(ctx, networkID, options)
}

func (c *testResourceClient) VolumeInspect(ctx context.Context, volumeID string, options client.VolumeInspectOptions) (client.VolumeInspectResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "volume-inspect", id: volumeID})
	return c.volumeInspect, nil
}

func (c *testResourceClient) NetworkInspect(ctx context.Context, networkID string, options client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "network-inspect", id: networkID})
	return c.networkInspect, nil
}

func (c *testResourceClient) ImageRemove(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	return c.imageRemove(ctx, imageID, options)
}

func (c *testResourceClient) ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "start", id: containerID})
	return client.ContainerStartResult{}, c.containerOpErr
}

func (c *testResourceClient) ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "stop", id: containerID})
	return client.ContainerStopResult{}, c.containerOpErr
}

func (c *testResourceClient) ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "restart", id: containerID})
	return client.ContainerRestartResult{}, c.containerOpErr
}

func (c *testResourceClient) ContainerKill(ctx context.Context, containerID string, options client.ContainerKillOptions) (client.ContainerKillResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "kill", id: containerID})
	return client.ContainerKillResult{}, c.containerOpErr
}

func (c *testResourceClient) ContainerPause(ctx context.Context, containerID string, options client.ContainerPauseOptions) (client.ContainerPauseResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "pause", id: containerID})
	return client.ContainerPauseResult{}, c.containerOpErr
}

func (c *testResourceClient) ContainerUnpause(ctx context.Context, containerID string, options client.ContainerUnpauseOptions) (client.ContainerUnpauseResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "unpause", id: containerID})
	return client.ContainerUnpauseResult{}, c.containerOpErr
}

func (c *testResourceClient) ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "remove", id: containerID})
	return client.ContainerRemoveResult{}, c.containerOpErr
}

func (c *testResourceClient) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "inspect", id: containerID})
	return c.containerInspect, nil
}

func (c *testResourceClient) ImageInspect(ctx context.Context, imageID string, inspectOpts ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "image-inspect", id: imageID})
	return c.imageInspect, nil
}

func (c *testResourceClient) ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "stats", id: containerID})
	return c.containerStats(ctx, containerID, options)
}

func (c *testResourceClient) ContainerTop(ctx context.Context, containerID string, options client.ContainerTopOptions) (client.ContainerTopResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "top", id: containerID})
	return client.ContainerTopResult{
		Titles:    []string{"PID", "CMD"},
		Processes: [][]string{{"123", "nginx"}},
	}, nil
}

func (c *testResourceClient) ContainerPrune(ctx context.Context, opts client.ContainerPruneOptions) (client.ContainerPruneResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "prune-containers"})
	return client.ContainerPruneResult{}, c.containerOpErr
}

func (c *testResourceClient) ImagePrune(ctx context.Context, opts client.ImagePruneOptions) (client.ImagePruneResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "prune-images"})
	return client.ImagePruneResult{}, c.containerOpErr
}

func (c *testResourceClient) VolumePrune(ctx context.Context, options client.VolumePruneOptions) (client.VolumePruneResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "prune-volumes"})
	return client.VolumePruneResult{}, c.containerOpErr
}

func (c *testResourceClient) NetworkPrune(ctx context.Context, opts client.NetworkPruneOptions) (client.NetworkPruneResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "prune-networks"})
	return client.NetworkPruneResult{}, c.containerOpErr
}

func newTestModel(streamer logRetargeter, resources resourceClient) Model {
	return NewModel(streamer, TmuxInfo{}, resources)
}

func newTestKeyMsg(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeFilter(m Model, text string) Model {
	got, _ := m.updateKeys(newTestKeyMsg("/"))
	m = got.(Model)
	for _, ch := range text {
		got, _ := m.updateKeys(newTestKeyMsg(string(ch)))
		m = got.(Model)
	}
	return m
}

func containerRowNames(rows []row) []string {
	var names []string
	for _, r := range rows {
		if r.kind == rowContainer {
			names = append(names, r.container.Name)
		}
	}
	return names
}

func TestFormatVolumeRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vol  Volume
		used int
		want string
	}{
		{
			name: "unused volume",
			vol:  Volume{Name: "data", Driver: "local"},
			used: 0,
			want: "data  local  used-by:0",
		},
		{
			name: "volume used by two containers",
			vol:  Volume{Name: "cache", Driver: "local"},
			used: 2,
			want: "cache  local  used-by:2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatVolumeRow(tc.vol, tc.used)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestFormatNetworkRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		net  Network
		used int
		want string
	}{
		{
			name: "network with a subnet",
			net:  Network{Name: "app-net", Driver: "bridge", Subnet: "172.18.0.0/16"},
			used: 0,
			want: "app-net  bridge  172.18.0.0/16  used-by:0",
		},
		{
			name: "network without a subnet",
			net:  Network{Name: "host", Driver: "host"},
			used: 2,
			want: "host  host  used-by:2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatNetworkRow(tc.net, tc.used)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestFormatImageRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		img  Image
		used int
		want string
	}{
		{
			name: "unused image",
			img:  Image{RepoTag: "nginx:latest", Size: 125_300_000},
			used: 0,
			want: "nginx:latest  125.3MB  used-by:0",
		},
		{
			name: "image used by two containers",
			img:  Image{RepoTag: "redis:7", Size: 2_000_000},
			used: 2,
			want: "redis:7  2.0MB  used-by:2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatImageRow(tc.img, tc.used)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestNewModel(t *testing.T) {
	t.Parallel()

	streamer := newTestLogRetargeter()
	resources := newTestResourceClient(nil)
	tmux := TmuxInfo{Present: true}

	got := NewModel(streamer, tmux, resources)

	assert.True(t, got.follow)
	assert.Equal(t, tmux, got.tmux)
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	t.Run("volumesMsg clamps the cursor to a valid range", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			volCursor  int
			volumes    []Volume
			wantCursor int
		}{
			{
				name:       "cursor within bounds is kept",
				volCursor:  1,
				volumes:    []Volume{{Name: "a"}, {Name: "b"}, {Name: "c"}},
				wantCursor: 1,
			},
			{
				name:       "cursor past the end clamps to the last volume",
				volCursor:  5,
				volumes:    []Volume{{Name: "a"}, {Name: "b"}},
				wantCursor: 1,
			},
			{
				name:       "cursor clamps to zero when volumes become empty",
				volCursor:  2,
				volumes:    nil,
				wantCursor: 0,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.volCursor = tc.volCursor

				got, cmd := m.Update(volumesMsg{volumes: tc.volumes})

				gotModel := got.(Model)
				assert.Equal(t, tc.volumes, gotModel.volumes)
				assert.Equal(t, tc.wantCursor, gotModel.volCursor)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("networksMsg clamps the cursor to a valid range", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			netCursor  int
			networks   []Network
			wantCursor int
		}{
			{
				name:       "cursor within bounds is kept",
				netCursor:  1,
				networks:   []Network{{Name: "a"}, {Name: "b"}, {Name: "c"}},
				wantCursor: 1,
			},
			{
				name:       "cursor past the end clamps to the last network",
				netCursor:  5,
				networks:   []Network{{Name: "a"}, {Name: "b"}},
				wantCursor: 1,
			},
			{
				name:       "cursor clamps to zero when networks become empty",
				netCursor:  2,
				networks:   nil,
				wantCursor: 0,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.netCursor = tc.netCursor

				got, cmd := m.Update(networksMsg{networks: tc.networks})

				gotModel := got.(Model)
				assert.Equal(t, tc.networks, gotModel.networks)
				assert.Equal(t, tc.wantCursor, gotModel.netCursor)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("imagesMsg clamps the cursor to a valid range", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			imgCursor  int
			images     []Image
			wantCursor int
		}{
			{
				name:       "cursor within bounds is kept",
				imgCursor:  1,
				images:     []Image{{ID: "a"}, {ID: "b"}, {ID: "c"}},
				wantCursor: 1,
			},
			{
				name:       "cursor past the end clamps to the last image",
				imgCursor:  5,
				images:     []Image{{ID: "a"}, {ID: "b"}},
				wantCursor: 1,
			},
			{
				name:       "cursor clamps to zero when images become empty",
				imgCursor:  2,
				images:     nil,
				wantCursor: 0,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.imgCursor = tc.imgCursor

				got, cmd := m.Update(imagesMsg{images: tc.images})

				gotModel := got.(Model)
				assert.Equal(t, tc.images, gotModel.images)
				assert.Equal(t, tc.wantCursor, gotModel.imgCursor)
				assert.Nil(t, cmd)
			})
		}
	})
}

func TestUpdateKeys(t *testing.T) {
	t.Parallel()

	t.Run("1 switches focus-list to the containers tab and resets confirm", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabVolumes
		m.confirm = &pendingDelete{kind: deleteVolume, id: "data"}
		m.rows = []row{{kind: rowContainer, key: "id:c1"}}

		got, cmd := m.updateKeys(newTestKeyMsg("1"))

		gotModel := got.(Model)
		assert.Equal(t, tabContainers, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		require.NotNil(t, cmd)
	})

	t.Run("2 switches focus-list to the volumes tab and resets confirm", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers
		m.confirm = &pendingDelete{kind: deleteVolume, id: "data"}

		got, cmd := m.updateKeys(newTestKeyMsg("2"))

		gotModel := got.(Model)
		assert.Equal(t, tabVolumes, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		assert.Nil(t, cmd)
	})

	t.Run("3 switches focus-list to the networks tab and resets confirm", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers
		m.confirm = &pendingDelete{kind: deleteNetwork, id: "app-net"}

		got, cmd := m.updateKeys(newTestKeyMsg("3"))

		gotModel := got.(Model)
		assert.Equal(t, tabNetworks, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		assert.Nil(t, cmd)
	})

	t.Run("4 switches focus-list to the images tab and resets confirm", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers
		m.confirm = &pendingDelete{kind: deleteImage, id: "img1"}

		got, cmd := m.updateKeys(newTestKeyMsg("4"))

		gotModel := got.(Model)
		assert.Equal(t, tabImages, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		assert.Nil(t, cmd)
	})

	t.Run("j moves the cursor down in the containers list", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers
		m.rows = []row{
			{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}},
			{kind: rowContainer, key: "id:c2", container: Container{ID: "c2"}},
		}
		m.cursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("j"))

		assert.Equal(t, 1, got.(Model).cursor)
		require.NotNil(t, cmd)
	})

	t.Run("g jumps to the first row in the containers list", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers
		m.rows = []row{
			{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}},
			{kind: rowContainer, key: "id:c2", container: Container{ID: "c2"}},
		}
		m.cursor = 1

		got, cmd := m.updateKeys(newTestKeyMsg("g"))

		assert.Equal(t, 0, got.(Model).cursor)
		require.NotNil(t, cmd)
	})

	t.Run("G jumps to the last row in the containers list", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers
		m.rows = []row{
			{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}},
			{kind: rowContainer, key: "id:c2", container: Container{ID: "c2"}},
		}
		m.cursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("G"))

		assert.Equal(t, 1, got.(Model).cursor)
		require.NotNil(t, cmd)
	})

	t.Run("volumes tab delegates other keys to updateVolumeKeys", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.tab = tabVolumes
		m.volumes = []Volume{{Name: "a"}, {Name: "b"}}
		m.volCursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("j"))

		assert.Equal(t, 1, got.(Model).volCursor)
		assert.Nil(t, cmd)
	})

	t.Run("l cycles resSubtab to inspect in tabVolumes", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabVolumes
		m.volumes = []Volume{{Name: "data"}}
		m.volCursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("l"))

		gotModel := got.(Model)
		assert.Equal(t, subInspect, gotModel.resSubtab)
		assert.NotNil(t, cmd)
		titlesRow := strings.Split(gotModel.View(), "\n")[1]
		assert.Contains(t, titlesRow, "inspect")
	})

	t.Run("h cycles resSubtab back to inspect in tabVolumes", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabVolumes
		m.volumes = []Volume{{Name: "data"}}
		m.volCursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("h"))

		gotModel := got.(Model)
		assert.Equal(t, subInspect, gotModel.resSubtab)
		assert.NotNil(t, cmd)
	})

	t.Run("l shows cached inspect content when resInspect already has the key", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabVolumes
		m.volumes = []Volume{{Name: "data"}}
		m.volCursor = 0
		m.resInspect = map[string]string{"volume:data": "cached inspect output"}

		got, cmd := m.updateKeys(newTestKeyMsg("l"))

		gotModel := got.(Model)
		assert.Equal(t, subInspect, gotModel.resSubtab)
		assert.Nil(t, cmd)
		assert.Contains(t, gotModel.subVP.View(), "cached inspect output")
	})

	t.Run("networks tab delegates other keys to updateNetworkKeys", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.tab = tabNetworks
		m.networks = []Network{{Name: "a"}, {Name: "b"}}
		m.netCursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("j"))

		assert.Equal(t, 1, got.(Model).netCursor)
		assert.Nil(t, cmd)
	})

	t.Run("images tab delegates other keys to updateImageKeys", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.tab = tabImages
		m.images = []Image{{ID: "a"}, {ID: "b"}}
		m.imgCursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("j"))

		assert.Equal(t, 1, got.(Model).imgCursor)
		assert.Nil(t, cmd)
	})

	t.Run("right moves from containers to volumes tab", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers

		got, cmd := m.updateKeys(newTestKeyMsg("right"))

		gotModel := got.(Model)
		assert.Equal(t, tabVolumes, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		assert.Nil(t, cmd)
	})

	t.Run("right moves from networks to images tab", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabNetworks

		got, cmd := m.updateKeys(newTestKeyMsg("right"))

		gotModel := got.(Model)
		assert.Equal(t, tabImages, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		assert.Nil(t, cmd)
	})

	t.Run("right wraps from images back to containers", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabImages
		m.rows = []row{{kind: rowContainer, key: "id:c1"}}

		got, cmd := m.updateKeys(newTestKeyMsg("right"))

		gotModel := got.(Model)
		assert.Equal(t, tabContainers, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		require.NotNil(t, cmd)
	})

	t.Run("left wraps from containers back to images", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers

		got, cmd := m.updateKeys(newTestKeyMsg("left"))

		gotModel := got.(Model)
		assert.Equal(t, tabImages, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		assert.Nil(t, cmd)
	})

	t.Run("right clears a pending confirm", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers
		m.confirm = &pendingDelete{kind: deleteVolume, id: "data"}

		got, _ := m.updateKeys(newTestKeyMsg("right"))

		assert.Nil(t, got.(Model).confirm)
	})

	t.Run("left wraps from volumes back to containers", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabVolumes
		m.rows = []row{{kind: rowContainer, key: "id:c1"}}

		got, cmd := m.updateKeys(newTestKeyMsg("left"))

		gotModel := got.(Model)
		assert.Equal(t, tabContainers, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		require.NotNil(t, cmd)
	})
}

func TestContainerOpKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		key        string
		state      string
		wantMethod string
	}{
		{name: "s stops the container", key: "s", state: "running", wantMethod: "stop"},
		{name: "S starts the container", key: "S", state: "exited", wantMethod: "start"},
		{name: "r restarts the container", key: "r", state: "running", wantMethod: "restart"},
		{name: "K kills the container", key: "K", state: "running", wantMethod: "kill"},
		{name: "p pauses a running container", key: "p", state: "running", wantMethod: "pause"},
		{name: "p unpauses a paused container", key: "p", state: "paused", wantMethod: "unpause"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resources := newTestResourceClient(nil)
			m := newTestModel(newTestLogRetargeter(), resources)
			m.tab = tabContainers
			m.focus = focusList
			m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: tc.state}}}
			m.cursor = 0

			_, cmd := m.updateKeys(newTestKeyMsg(tc.key))

			require.NotNil(t, cmd)
			assert.Nil(t, cmd())
			require.Len(t, resources.calls, 1)
			assert.Equal(t, testResourceCall{method: tc.wantMethod, id: "c1"}, resources.calls[0])
		})
	}

	t.Run("e execs into the container and returns a command", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("e"))

		require.NotNil(t, cmd)
		assert.Empty(t, resources.calls)
	})

	t.Run("p is a no-op over a stack row", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
		m.cursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("p"))

		assert.Nil(t, cmd)
		_ = got.(Model)
		assert.Empty(t, resources.calls)
	})

	t.Run("s/S/r/K over a stack row apply to every container in the project", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			key        string
			wantMethod string
		}{
			{name: "s stops every container", key: "s", wantMethod: "stop"},
			{name: "S starts every container", key: "S", wantMethod: "start"},
			{name: "r restarts every container", key: "r", wantMethod: "restart"},
			{name: "K kills every container", key: "K", wantMethod: "kill"},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				resources := newTestResourceClient(nil)
				m := newTestModel(newTestLogRetargeter(), resources)
				m.tab = tabContainers
				m.focus = focusList
				m.containers = []Container{
					{ID: "c1", Project: "app"},
					{ID: "c2", Project: "app"},
					{ID: "c3", Project: "other"},
				}
				m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
				m.cursor = 0

				_, cmd := m.updateKeys(newTestKeyMsg(tc.key))

				require.NotNil(t, cmd)
				assert.Nil(t, cmd())
				require.Len(t, resources.calls, 2)
				assert.Equal(t, testResourceCall{method: tc.wantMethod, id: "c1"}, resources.calls[0])
				assert.Equal(t, testResourceCall{method: tc.wantMethod, id: "c2"}, resources.calls[1])
			})
		}
	})

	t.Run("s/S/r/K over a stack row with a pending confirm do nothing", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"s", "S", "r", "K"} {
			t.Run(key, func(t *testing.T) {
				t.Parallel()

				resources := newTestResourceClient(nil)
				m := newTestModel(newTestLogRetargeter(), resources)
				m.tab = tabContainers
				m.focus = focusList
				m.containers = []Container{{ID: "c1", Project: "app"}}
				m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
				m.cursor = 0
				m.confirm = &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}

				got, cmd := m.updateKeys(newTestKeyMsg(key))

				assert.Equal(t, &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}, got.(Model).confirm)
				assert.Nil(t, cmd)
				assert.Empty(t, resources.calls)
			})
		}
	})

	t.Run("returns a watcherErrMsg when the container op fails", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		resources := newTestResourceClientWithContainerOpErr(wantErr)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("s"))

		require.NotNil(t, cmd)
		assert.Equal(t, watcherErrMsg{err: wantErr}, cmd())
	})

	t.Run("d arms confirm for a container row", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web"}}}
		m.cursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("d"))

		assert.Equal(t, &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}, got.(Model).confirm)
		assert.Nil(t, cmd)
	})

	t.Run("d over a stack row arms confirm with the project label and its container ids", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.containers = []Container{
			{ID: "c1", Project: "app"},
			{ID: "c2", Project: "app"},
			{ID: "c3", Project: "other"},
		}
		m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
		m.cursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("d"))

		assert.Equal(t, &pendingDelete{kind: deleteStack, ids: []string{"c1", "c2"}, label: "app"}, got.(Model).confirm)
		assert.Nil(t, cmd)
	})

	t.Run("d over the standalone stack row arms confirm with the standalone label", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.containers = []Container{{ID: "c1", Project: ""}}
		m.rows = []row{{kind: rowStack, key: "stack:"}}
		m.cursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("d"))

		assert.Equal(t, &pendingDelete{kind: deleteStack, ids: []string{"c1"}, label: "standalone"}, got.(Model).confirm)
		assert.Nil(t, cmd)
	})

	t.Run("y with a pending stack confirm calls ContainerRemove for each id", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.confirm = &pendingDelete{kind: deleteStack, ids: []string{"c1", "c2"}, label: "app"}

		got, cmd := m.updateKeys(newTestKeyMsg("y"))

		assert.Nil(t, got.(Model).confirm)
		require.NotNil(t, cmd)
		assert.Nil(t, cmd())
		require.Len(t, resources.calls, 2)
		assert.Equal(t, testResourceCall{method: "remove", id: "c1"}, resources.calls[0])
		assert.Equal(t, testResourceCall{method: "remove", id: "c2"}, resources.calls[1])
	})

	t.Run("n and esc clear a pending stack confirm without calling ContainerRemove", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"n", "esc"} {
			t.Run(key, func(t *testing.T) {
				t.Parallel()

				resources := newTestResourceClient(nil)
				m := newTestModel(newTestLogRetargeter(), resources)
				m.tab = tabContainers
				m.focus = focusList
				m.confirm = &pendingDelete{kind: deleteStack, ids: []string{"c1", "c2"}, label: "app"}

				got, cmd := m.updateKeys(newTestKeyMsg(key))

				assert.Nil(t, got.(Model).confirm)
				assert.Nil(t, cmd)
				assert.Empty(t, resources.calls)
			})
		}
	})

	t.Run("y with a pending stack confirm returns a watcherErrMsg when a ContainerRemove fails", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		resources := newTestResourceClientWithContainerOpErr(wantErr)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.confirm = &pendingDelete{kind: deleteStack, ids: []string{"c1", "c2"}, label: "app"}

		got, cmd := m.updateKeys(newTestKeyMsg("y"))

		assert.Nil(t, got.(Model).confirm)
		require.NotNil(t, cmd)
		assert.Equal(t, watcherErrMsg{err: wantErr}, cmd())
	})

	t.Run("y with a pending confirm calls ContainerRemove with the id", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.confirm = &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}

		got, cmd := m.updateKeys(newTestKeyMsg("y"))

		assert.Nil(t, got.(Model).confirm)
		require.NotNil(t, cmd)
		assert.Nil(t, cmd())
		require.Len(t, resources.calls, 1)
		assert.Equal(t, testResourceCall{method: "remove", id: "c1"}, resources.calls[0])
	})

	t.Run("n and esc clear a pending confirm without calling ContainerRemove", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"n", "esc"} {
			t.Run(key, func(t *testing.T) {
				t.Parallel()

				resources := newTestResourceClient(nil)
				m := newTestModel(newTestLogRetargeter(), resources)
				m.tab = tabContainers
				m.focus = focusList
				m.confirm = &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}

				got, cmd := m.updateKeys(newTestKeyMsg(key))

				assert.Nil(t, got.(Model).confirm)
				assert.Nil(t, cmd)
				assert.Empty(t, resources.calls)
			})
		}
	})

	t.Run("s with a pending confirm does nothing", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.confirm = &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}

		got, cmd := m.updateKeys(newTestKeyMsg("s"))

		assert.Equal(t, &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}, got.(Model).confirm)
		assert.Nil(t, cmd)
		assert.Empty(t, resources.calls)
	})
}

func assertScrollForwardsToSubVP(t *testing.T, newModel func() Model, updateFn func(Model, string) (Model, tea.Cmd), getCursor func(Model) int, wantCursor int) {
	t.Helper()

	t.Run("g and G scroll the subVP without moving the cursor when focus is on the right pane", func(t *testing.T) {
		t.Parallel()

		t.Run("g goes to the top", func(t *testing.T) {
			t.Parallel()

			m := newModel()
			m.subVP.YOffset = 5

			gotModel, cmd := updateFn(m, "g")

			assert.Equal(t, wantCursor, getCursor(gotModel))
			assert.Nil(t, cmd)
			assert.Equal(t, 0, gotModel.subVP.YOffset)
		})

		t.Run("G goes to the bottom", func(t *testing.T) {
			t.Parallel()

			m := newModel()

			gotModel, cmd := updateFn(m, "G")

			assert.Equal(t, wantCursor, getCursor(gotModel))
			assert.Nil(t, cmd)
			assert.Greater(t, gotModel.subVP.YOffset, 0)
		})
	})

	t.Run("an unrecognized key while focus is on the right pane forwards to the subVP without moving the cursor", func(t *testing.T) {
		t.Parallel()

		m := newModel()

		gotModel, cmd := updateFn(m, "down")

		assert.Equal(t, wantCursor, getCursor(gotModel))
		assert.Nil(t, cmd)
		assert.Greater(t, gotModel.subVP.YOffset, 0)
	})
}

func assertEscReturnsToInfo(t *testing.T, m Model, updateFn func(Model) (Model, tea.Cmd)) {
	t.Helper()

	got, cmd := updateFn(m)

	assert.Equal(t, subInfo, got.resSubtab)
	assert.Nil(t, cmd)
}

func TestUpdateVolumeKeys(t *testing.T) {
	t.Parallel()

	t.Run("j moves the cursor down until the last volume", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			volCursor  int
			wantCursor int
		}{
			{name: "advances from the first volume", volCursor: 0, wantCursor: 1},
			{name: "stays at the last volume", volCursor: 1, wantCursor: 1},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.volumes = []Volume{{Name: "a"}, {Name: "b"}}
				m.volCursor = tc.volCursor

				got, cmd := m.updateVolumeKeys(newTestKeyMsg("j"))

				assert.Equal(t, tc.wantCursor, got.(Model).volCursor)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("k moves the cursor up until the first volume", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			volCursor  int
			wantCursor int
		}{
			{name: "retreats from the last volume", volCursor: 1, wantCursor: 0},
			{name: "stays at the first volume", volCursor: 0, wantCursor: 0},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.volumes = []Volume{{Name: "a"}, {Name: "b"}}
				m.volCursor = tc.volCursor

				got, cmd := m.updateVolumeKeys(newTestKeyMsg("k"))

				assert.Equal(t, tc.wantCursor, got.(Model).volCursor)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("g jumps to the first volume when volumes exist", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volumes = []Volume{{Name: "a"}, {Name: "b"}}
		m.volCursor = 1

		got, cmd := m.updateVolumeKeys(newTestKeyMsg("g"))

		assert.Equal(t, 0, got.(Model).volCursor)
		assert.Nil(t, cmd)
	})

	t.Run("g is a no-op with no volumes", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volCursor = 0

		got, cmd := m.updateVolumeKeys(newTestKeyMsg("g"))

		assert.Equal(t, 0, got.(Model).volCursor)
		assert.Nil(t, cmd)
	})

	t.Run("G jumps to the last volume when volumes exist", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volumes = []Volume{{Name: "a"}, {Name: "b"}, {Name: "c"}}
		m.volCursor = 0

		got, cmd := m.updateVolumeKeys(newTestKeyMsg("G"))

		assert.Equal(t, 2, got.(Model).volCursor)
		assert.Nil(t, cmd)
	})

	t.Run("G is a no-op with no volumes", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volCursor = 0

		got, cmd := m.updateVolumeKeys(newTestKeyMsg("G"))

		assert.Equal(t, 0, got.(Model).volCursor)
		assert.Nil(t, cmd)
	})

	t.Run("d", func(t *testing.T) {
		t.Parallel()

		t.Run("arms confirm for an unused volume", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.volumes = []Volume{{Name: "data"}}
			m.volCursor = 0

			got, cmd := m.updateVolumeKeys(newTestKeyMsg("d"))

			gotModel := got.(Model)
			assert.Equal(t, &pendingDelete{kind: deleteVolume, id: "data", label: "data"}, gotModel.confirm)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing for a volume in use", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.volumes = []Volume{{Name: "data"}}
			m.containers = []Container{{ID: "c1", Volumes: []string{"data"}}}
			m.volCursor = 0

			got, cmd := m.updateVolumeKeys(newTestKeyMsg("d"))

			assert.Nil(t, got.(Model).confirm)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing when a confirm is already pending", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.volumes = []Volume{{Name: "data"}}
			m.volCursor = 0
			m.confirm = &pendingDelete{kind: deleteVolume, id: "other", label: "other"}

			got, cmd := m.updateVolumeKeys(newTestKeyMsg("d"))

			assert.Equal(t, "other", got.(Model).confirm.id)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing with an out-of-range cursor", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.volCursor = -1

			got, cmd := m.updateVolumeKeys(newTestKeyMsg("d"))

			assert.Nil(t, got.(Model).confirm)
			assert.Nil(t, cmd)
		})
	})

	t.Run("y", func(t *testing.T) {
		t.Parallel()

		t.Run("is a no-op without a pending confirm", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))

			got, cmd := m.updateVolumeKeys(newTestKeyMsg("y"))

			assert.Nil(t, got.(Model).confirm)
			assert.Nil(t, cmd)
		})

		t.Run("removes the volume and clears confirm on success", func(t *testing.T) {
			t.Parallel()

			var gotID string
			resources := newTestResourceClient(func(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error) {
				gotID = volumeID
				return client.VolumeRemoveResult{}, nil
			})
			m := newTestModel(newTestLogRetargeter(), resources)
			m.confirm = &pendingDelete{kind: deleteVolume, id: "data", label: "data"}

			got, cmd := m.updateVolumeKeys(newTestKeyMsg("y"))

			assert.Nil(t, got.(Model).confirm)
			require.NotNil(t, cmd)
			assert.Nil(t, cmd())
			assert.Equal(t, "data", gotID)
		})

		t.Run("returns a watcherErrMsg when removal fails", func(t *testing.T) {
			t.Parallel()

			wantErr := errors.New("boom")
			resources := newTestResourceClient(func(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error) {
				return client.VolumeRemoveResult{}, wantErr
			})
			m := newTestModel(newTestLogRetargeter(), resources)
			m.confirm = &pendingDelete{kind: deleteVolume, id: "data", label: "data"}

			_, cmd := m.updateVolumeKeys(newTestKeyMsg("y"))

			require.NotNil(t, cmd)
			assert.Equal(t, watcherErrMsg{err: wantErr}, cmd())
		})
	})

	t.Run("an unrecognized key is a no-op", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volumes = []Volume{{Name: "a"}}
		m.volCursor = 0

		got, cmd := m.updateVolumeKeys(newTestKeyMsg("x"))

		assert.Equal(t, 0, got.(Model).volCursor)
		assert.Nil(t, cmd)
	})

	t.Run("n and esc clear a pending confirm", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"n", "esc"} {
			t.Run(key, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.confirm = &pendingDelete{kind: deleteVolume, id: "data", label: "data"}

				got, cmd := m.updateVolumeKeys(newTestKeyMsg(key))

				assert.Nil(t, got.(Model).confirm)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("esc returns resSubtab to info", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volumes = []Volume{{Name: "data"}}
		m.volCursor = 0
		m.resSubtab = subInspect

		assertEscReturnsToInfo(t, m, func(m Model) (Model, tea.Cmd) {
			got, cmd := m.updateVolumeKeys(newTestKeyMsg("esc"))
			return got.(Model), cmd
		})
	})

	t.Run("j refreshes the subVP with the newly selected volume", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabVolumes
		m.volumes = []Volume{{Name: "data"}, {Name: "cache"}}
		m.volCursor = 0

		got, cmd := m.updateVolumeKeys(newTestKeyMsg("j"))

		gotModel := got.(Model)
		assert.Equal(t, 1, gotModel.volCursor)
		assert.Nil(t, cmd)
		assert.Contains(t, gotModel.subVP.View(), "cache")
	})

	assertScrollForwardsToSubVP(t,
		func() Model {
			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.focus = focusLogs
			m.volumes = []Volume{{Name: "data"}, {Name: "cache"}}
			m.volCursor = 1
			m.subVP.Height = 2
			m.subVP.SetContent(strings.Repeat("line\n", 20))
			return m
		},
		func(m Model, key string) (Model, tea.Cmd) {
			got, cmd := m.updateVolumeKeys(newTestKeyMsg(key))
			return got.(Model), cmd
		},
		func(m Model) int { return m.volCursor },
		1,
	)
}

func TestUpdateImageKeys(t *testing.T) {
	t.Parallel()

	t.Run("j moves the cursor down until the last image", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			imgCursor  int
			wantCursor int
		}{
			{name: "advances from the first image", imgCursor: 0, wantCursor: 1},
			{name: "stays at the last image", imgCursor: 1, wantCursor: 1},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.images = []Image{{ID: "a"}, {ID: "b"}}
				m.imgCursor = tc.imgCursor

				got, cmd := m.updateImageKeys(newTestKeyMsg("j"))

				assert.Equal(t, tc.wantCursor, got.(Model).imgCursor)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("k moves the cursor up until the first image", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			imgCursor  int
			wantCursor int
		}{
			{name: "retreats from the last image", imgCursor: 1, wantCursor: 0},
			{name: "stays at the first image", imgCursor: 0, wantCursor: 0},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.images = []Image{{ID: "a"}, {ID: "b"}}
				m.imgCursor = tc.imgCursor

				got, cmd := m.updateImageKeys(newTestKeyMsg("k"))

				assert.Equal(t, tc.wantCursor, got.(Model).imgCursor)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("g jumps to the first image when images exist", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.images = []Image{{ID: "a"}, {ID: "b"}}
		m.imgCursor = 1

		got, cmd := m.updateImageKeys(newTestKeyMsg("g"))

		assert.Equal(t, 0, got.(Model).imgCursor)
		assert.Nil(t, cmd)
	})

	t.Run("g is a no-op with no images", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.imgCursor = 0

		got, cmd := m.updateImageKeys(newTestKeyMsg("g"))

		assert.Equal(t, 0, got.(Model).imgCursor)
		assert.Nil(t, cmd)
	})

	t.Run("G jumps to the last image when images exist", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.images = []Image{{ID: "a"}, {ID: "b"}, {ID: "c"}}
		m.imgCursor = 0

		got, cmd := m.updateImageKeys(newTestKeyMsg("G"))

		assert.Equal(t, 2, got.(Model).imgCursor)
		assert.Nil(t, cmd)
	})

	t.Run("G is a no-op with no images", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.imgCursor = 0

		got, cmd := m.updateImageKeys(newTestKeyMsg("G"))

		assert.Equal(t, 0, got.(Model).imgCursor)
		assert.Nil(t, cmd)
	})

	t.Run("d", func(t *testing.T) {
		t.Parallel()

		t.Run("arms confirm for an unused image", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.images = []Image{{ID: "img1", RepoTag: "nginx:latest"}}
			m.imgCursor = 0

			got, cmd := m.updateImageKeys(newTestKeyMsg("d"))

			gotModel := got.(Model)
			assert.Equal(t, &pendingDelete{kind: deleteImage, id: "img1", label: "nginx:latest"}, gotModel.confirm)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing for an image in use", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.images = []Image{{ID: "img1", RepoTag: "nginx:latest"}}
			m.containers = []Container{{ID: "c1", ImageID: "img1"}}
			m.imgCursor = 0

			got, cmd := m.updateImageKeys(newTestKeyMsg("d"))

			assert.Nil(t, got.(Model).confirm)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing when a confirm is already pending", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.images = []Image{{ID: "img1", RepoTag: "nginx:latest"}}
			m.imgCursor = 0
			m.confirm = &pendingDelete{kind: deleteImage, id: "other", label: "other"}

			got, cmd := m.updateImageKeys(newTestKeyMsg("d"))

			assert.Equal(t, "other", got.(Model).confirm.id)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing with an out-of-range cursor", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.imgCursor = -1

			got, cmd := m.updateImageKeys(newTestKeyMsg("d"))

			assert.Nil(t, got.(Model).confirm)
			assert.Nil(t, cmd)
		})
	})

	t.Run("y", func(t *testing.T) {
		t.Parallel()

		t.Run("is a no-op without a pending confirm", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))

			got, cmd := m.updateImageKeys(newTestKeyMsg("y"))

			assert.Nil(t, got.(Model).confirm)
			assert.Nil(t, cmd)
		})

		t.Run("removes the image and clears confirm on success", func(t *testing.T) {
			t.Parallel()

			var gotID string
			resources := newTestResourceClient(nil)
			resources.imageRemove = func(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
				gotID = imageID
				return client.ImageRemoveResult{}, nil
			}
			m := newTestModel(newTestLogRetargeter(), resources)
			m.confirm = &pendingDelete{kind: deleteImage, id: "img1", label: "nginx:latest"}

			got, cmd := m.updateImageKeys(newTestKeyMsg("y"))

			assert.Nil(t, got.(Model).confirm)
			require.NotNil(t, cmd)
			assert.Nil(t, cmd())
			assert.Equal(t, "img1", gotID)
		})

		t.Run("returns a watcherErrMsg when removal fails", func(t *testing.T) {
			t.Parallel()

			wantErr := errors.New("boom")
			resources := newTestResourceClient(nil)
			resources.imageRemove = func(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
				return client.ImageRemoveResult{}, wantErr
			}
			m := newTestModel(newTestLogRetargeter(), resources)
			m.confirm = &pendingDelete{kind: deleteImage, id: "img1", label: "nginx:latest"}

			_, cmd := m.updateImageKeys(newTestKeyMsg("y"))

			require.NotNil(t, cmd)
			assert.Equal(t, watcherErrMsg{err: wantErr}, cmd())
		})
	})

	t.Run("an unrecognized key is a no-op", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.images = []Image{{ID: "a"}}
		m.imgCursor = 0

		got, cmd := m.updateImageKeys(newTestKeyMsg("x"))

		assert.Equal(t, 0, got.(Model).imgCursor)
		assert.Nil(t, cmd)
	})

	t.Run("n and esc clear a pending confirm", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"n", "esc"} {
			t.Run(key, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.confirm = &pendingDelete{kind: deleteImage, id: "img1", label: "nginx:latest"}

				got, cmd := m.updateImageKeys(newTestKeyMsg(key))

				assert.Nil(t, got.(Model).confirm)
				assert.Nil(t, cmd)
			})
		}
	})

	assertScrollForwardsToSubVP(t,
		func() Model {
			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.focus = focusLogs
			m.images = []Image{{ID: "a"}, {ID: "b"}}
			m.imgCursor = 1
			m.subVP.Height = 2
			m.subVP.SetContent(strings.Repeat("line\n", 20))
			return m
		},
		func(m Model, key string) (Model, tea.Cmd) {
			got, cmd := m.updateImageKeys(newTestKeyMsg(key))
			return got.(Model), cmd
		},
		func(m Model) int { return m.imgCursor },
		1,
	)

	t.Run("esc returns resSubtab to info", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.images = []Image{{ID: "a"}}
		m.imgCursor = 0
		m.resSubtab = subInspect

		assertEscReturnsToInfo(t, m, func(m Model) (Model, tea.Cmd) {
			got, cmd := m.updateImageKeys(newTestKeyMsg("esc"))
			return got.(Model), cmd
		})
	})
}

func TestPruneKeys(t *testing.T) {
	t.Parallel()

	callKeys := func(m Model, tab tabID, msg tea.KeyMsg) (Model, tea.Cmd) {
		switch tab {
		case tabVolumes:
			got, cmd := m.updateVolumeKeys(msg)
			return got.(Model), cmd
		case tabNetworks:
			got, cmd := m.updateNetworkKeys(msg)
			return got.(Model), cmd
		case tabImages:
			got, cmd := m.updateImageKeys(msg)
			return got.(Model), cmd
		default:
			got, cmd := m.updateKeys(msg)
			return got.(Model), cmd
		}
	}

	tests := []struct {
		name      string
		tab       tabID
		kind      deleteKind
		label     string
		pruneCall string
	}{
		{name: "containers", tab: tabContainers, kind: pruneContainers, label: "stopped containers", pruneCall: "prune-containers"},
		{name: "volumes", tab: tabVolumes, kind: pruneVolumes, label: "unused volumes", pruneCall: "prune-volumes"},
		{name: "networks", tab: tabNetworks, kind: pruneNetworks, label: "unused networks", pruneCall: "prune-networks"},
		{name: "images", tab: tabImages, kind: pruneImages, label: "dangling images", pruneCall: "prune-images"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Run("P arms the confirm with the prune kind and label", func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.tab = tc.tab
				m.focus = focusList

				got, cmd := callKeys(m, tc.tab, newTestKeyMsg("P"))

				assert.Equal(t, &pendingDelete{kind: tc.kind, label: tc.label}, got.confirm)
				assert.Nil(t, cmd)
			})

			t.Run("y invokes the prune method exactly once", func(t *testing.T) {
				t.Parallel()

				resources := newTestResourceClient(nil)
				m := newTestModel(newTestLogRetargeter(), resources)
				m.tab = tc.tab
				m.focus = focusList
				m.confirm = &pendingDelete{kind: tc.kind, label: tc.label}

				got, cmd := callKeys(m, tc.tab, newTestKeyMsg("y"))

				assert.Nil(t, got.confirm)
				require.NotNil(t, cmd)
				assert.Nil(t, cmd())
				require.Len(t, resources.calls, 1)
				assert.Equal(t, testResourceCall{method: tc.pruneCall}, resources.calls[0])
			})

			t.Run("n clears the confirm without calling the prune method", func(t *testing.T) {
				t.Parallel()

				resources := newTestResourceClient(nil)
				m := newTestModel(newTestLogRetargeter(), resources)
				m.tab = tc.tab
				m.focus = focusList
				m.confirm = &pendingDelete{kind: tc.kind, label: tc.label}

				got, cmd := callKeys(m, tc.tab, newTestKeyMsg("n"))

				assert.Nil(t, got.confirm)
				assert.Nil(t, cmd)
				assert.Empty(t, resources.calls)
			})

			t.Run("P with a confirm already armed does not overwrite it", func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.tab = tc.tab
				m.focus = focusList
				m.confirm = &pendingDelete{kind: deleteVolume, id: "other", label: "other"}

				got, cmd := callKeys(m, tc.tab, newTestKeyMsg("P"))

				assert.Equal(t, &pendingDelete{kind: deleteVolume, id: "other", label: "other"}, got.confirm)
				assert.Nil(t, cmd)
			})
		})
	}
}

func TestResourceFooter(t *testing.T) {
	t.Parallel()

	const base = " j/k move  h/l view  tab focus  left/right tab  d delete  P prune  q quit"

	tests := []struct {
		name    string
		confirm *pendingDelete
		hint    string
		want    string
	}{
		{
			name:    "no confirm no hint",
			confirm: nil,
			hint:    "",
			want:    base,
		},
		{
			name:    "confirm set",
			confirm: &pendingDelete{kind: deleteVolume, id: "data", label: "data"},
			hint:    "",
			want:    base + "  delete data? y/n",
		},
		{
			name:    "no confirm with hint",
			confirm: nil,
			hint:    "d: volume in use",
			want:    base + "  d: volume in use",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := resourceFooter(tc.confirm, tc.hint)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestRenderTabBar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tab        tabID
		wantActive string
		wantDim    []string
	}{
		{
			name:       "containers tab active",
			tab:        tabContainers,
			wantActive: styleSelected.Render(" 1:containers "),
			wantDim:    []string{styleDim.Render("2:volumes"), styleDim.Render("3:networks"), styleDim.Render("4:images")},
		},
		{
			name:       "volumes tab active",
			tab:        tabVolumes,
			wantActive: styleSelected.Render(" 2:volumes "),
			wantDim:    []string{styleDim.Render("1:containers"), styleDim.Render("3:networks"), styleDim.Render("4:images")},
		},
		{
			name:       "networks tab active",
			tab:        tabNetworks,
			wantActive: styleSelected.Render(" 3:networks "),
			wantDim:    []string{styleDim.Render("1:containers"), styleDim.Render("2:volumes"), styleDim.Render("4:images")},
		},
		{
			name:       "images tab active",
			tab:        tabImages,
			wantActive: styleSelected.Render(" 4:images "),
			wantDim:    []string{styleDim.Render("1:containers"), styleDim.Render("2:volumes"), styleDim.Render("3:networks")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := renderTabBar(tc.tab)

			assert.Contains(t, got, tc.wantActive)
			for _, dim := range tc.wantDim {
				assert.Contains(t, got, dim)
			}
		})
	}
}

func TestRenderResourceRows(t *testing.T) {
	t.Parallel()

	t.Run("empty rows renders the empty label", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)

		gotRows := m.renderResourceRows(nil, 0, "no things")

		require.Equal(t, styleDim.Render("no things"), gotRows)
	})

	t.Run("cursor row is the selected-styled one", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		w := m.listWidth()
		rows := []string{"row0", "row1"}

		gotRows := m.renderResourceRows(rows, 1, "no things")

		wantLine0 := truncate("row0", w)
		wantLine1 := styleSelected.Render(fmt.Sprintf("%-*s", w, truncate("row1", w)))
		require.Equal(t, wantLine0+"\n"+wantLine1, gotRows)
	})

	t.Run("long row is truncated", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		longRow := strings.Repeat("x", m.listWidth()+20)

		gotRows := m.renderResourceRows([]string{longRow}, -1, "no things")

		require.Equal(t, truncate(longRow, m.listWidth()), gotRows)
		require.Contains(t, gotRows, "…")
	})
}

func TestVisibleWindow(t *testing.T) {
	t.Parallel()

	lines := []string{"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7", "l9"}

	tests := []struct {
		name   string
		lines  []string
		cursor int
		height int
		want   []string
	}{
		{
			name:   "list shorter than height is not clipped",
			lines:  lines[:3],
			cursor: 1,
			height: 5,
			want:   lines[:3],
		},
		{
			name:   "cursor 0 shows the first page",
			lines:  lines,
			cursor: 0,
			height: 3,
			want:   lines[0:3],
		},
		{
			name:   "cursor beyond height slides the window",
			lines:  lines,
			cursor: 5,
			height: 3,
			want:   lines[3:6],
		},
		{
			name:   "cursor on the last row shows a full last page",
			lines:  lines,
			cursor: len(lines) - 1,
			height: 3,
			want:   lines[len(lines)-3:],
		},
		{
			name:   "height 0 is not clipped",
			lines:  lines,
			cursor: 4,
			height: 0,
			want:   lines,
		},
		{
			name:   "cursor past the end clamps the window to the last page",
			lines:  lines,
			cursor: len(lines),
			height: 3,
			want:   lines[len(lines)-3:],
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := visibleWindow(tc.lines, tc.cursor, tc.height)

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResourceKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tab   tabID
		model func(m Model) Model
		want  string
	}{
		{
			name: "tabVolumes returns a volume-prefixed key",
			tab:  tabVolumes,
			model: func(m Model) Model {
				m.volumes = []Volume{{Name: "data"}}
				m.volCursor = 0
				return m
			},
			want: "volume:data",
		},
		{
			name: "tabVolumes with an out-of-range cursor returns empty",
			tab:  tabVolumes,
			model: func(m Model) Model {
				m.volCursor = -1
				return m
			},
			want: "",
		},
		{
			name: "tabNetworks returns a network-prefixed key",
			tab:  tabNetworks,
			model: func(m Model) Model {
				m.networks = []Network{{Name: "app-net"}}
				m.netCursor = 0
				return m
			},
			want: "network:app-net",
		},
		{
			name: "tabNetworks with an out-of-range cursor returns empty",
			tab:  tabNetworks,
			model: func(m Model) Model {
				m.netCursor = -1
				return m
			},
			want: "",
		},
		{
			name: "tabImages returns an image-prefixed key",
			tab:  tabImages,
			model: func(m Model) Model {
				m.images = []Image{{ID: "img1"}}
				m.imgCursor = 0
				return m
			},
			want: "image:img1",
		},
		{
			name: "tabImages with an out-of-range cursor returns empty",
			tab:  tabImages,
			model: func(m Model) Model {
				m.imgCursor = -1
				return m
			},
			want: "",
		},
		{
			name:  "tabContainers returns empty",
			tab:   tabContainers,
			model: func(m Model) Model { return m },
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.tab = tc.tab
			m = tc.model(m)

			assert.Equal(t, tc.want, m.resourceKey())
		})
	}
}

func TestRenderVolumeList(t *testing.T) {
	t.Parallel()

	t.Run("renders a row per volume", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.volumes = []Volume{{Name: "data", Driver: "local"}}

		gotList := m.renderVolumeList()

		require.Contains(t, gotList, formatVolumeRow(m.volumes[0], 0))
	})

	t.Run("renders the empty label with no volumes", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)

		gotList := m.renderVolumeList()

		require.Contains(t, gotList, "no volumes")
	})
}

func TestRenderNetworkList(t *testing.T) {
	t.Parallel()

	t.Run("renders a row per network", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.networks = []Network{{Name: "app-net", Driver: "bridge"}}

		gotList := m.renderNetworkList()

		require.Contains(t, gotList, formatNetworkRow(m.networks[0], 0))
	})

	t.Run("renders the empty label with no networks", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)

		gotList := m.renderNetworkList()

		require.Contains(t, gotList, "no networks")
	})
}

func TestRenderImageList(t *testing.T) {
	t.Parallel()

	t.Run("renders a row per image", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.images = []Image{{ID: "img1", RepoTag: "nginx", Size: 1_000_000}}

		gotList := m.renderImageList()

		require.Contains(t, gotList, formatImageRow(m.images[0], 0))
	})

	t.Run("renders the empty label with no images", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)

		gotList := m.renderImageList()

		require.Contains(t, gotList, "no images")
	})
}

func TestRenderImageDetail(t *testing.T) {
	t.Parallel()

	t.Run("contains id, repo:tag and used by", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.images = []Image{{ID: "img1", RepoTag: "nginx:latest", Size: 125_300_000}}
		m.containers = []Container{{ID: "c1", Name: "web", ImageID: "img1"}}
		m.imgCursor = 0

		gotDetail := m.renderImageDetail()

		assert.Contains(t, gotDetail, renderKV("id", shortID("img1")))
		assert.Contains(t, gotDetail, renderKV("repo:tag", "nginx:latest"))
		assert.Contains(t, gotDetail, "USED BY")
		assert.Contains(t, gotDetail, "web")
	})

	t.Run("shows none when no container uses the image", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.images = []Image{{ID: "img1", RepoTag: "nginx:latest", Size: 125_300_000}}
		m.imgCursor = 0

		gotDetail := m.renderImageDetail()

		assert.Contains(t, gotDetail, "USED BY")
		assert.Contains(t, gotDetail, styleDim.Render("none"))
	})
}

func TestRenderVolumeDetail(t *testing.T) {
	t.Parallel()

	t.Run("contains name, driver, mount, created and used by", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volumes = []Volume{{Name: "data", Driver: "local", Mountpoint: "/var/lib/docker/volumes/data", Created: "2024-01-01"}}
		m.containers = []Container{{ID: "c1", Name: "web", Volumes: []string{"data"}}}
		m.volCursor = 0

		gotDetail := m.renderVolumeDetail()

		assert.Contains(t, gotDetail, renderKV("name", "data"))
		assert.Contains(t, gotDetail, renderKV("driver", "local"))
		assert.Contains(t, gotDetail, renderKV("mount", "/var/lib/docker/volumes/data"))
		assert.Contains(t, gotDetail, renderKV("created", "2024-01-01"))
		assert.Contains(t, gotDetail, "USED BY")
		assert.Contains(t, gotDetail, "web")
	})

	t.Run("shows none when no container uses the volume", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volumes = []Volume{{Name: "data", Driver: "local"}}
		m.volCursor = 0

		gotDetail := m.renderVolumeDetail()

		assert.Contains(t, gotDetail, "USED BY")
		assert.Contains(t, gotDetail, styleDim.Render("none"))
	})

	t.Run("renders labels section sorted alphabetically", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.volumes = []Volume{{Name: "data", Driver: "local", Labels: map[string]string{"b": "2", "a": "1"}}}
		m.volCursor = 0

		gotDetail := m.renderVolumeDetail()

		assert.Contains(t, gotDetail, "LABELS")
		assert.Regexp(t, "a=1[\\s\\S]*b=2", gotDetail)
	})
}

func TestRenderNetworkDetail(t *testing.T) {
	t.Parallel()

	t.Run("contains id, driver, subnet and used by", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.networks = []Network{{ID: "net1", Name: "app_net", Driver: "bridge", Subnet: "172.18.0.0/16"}}
		m.containers = []Container{{ID: "c1", Name: "web", Networks: []string{"app_net"}}}
		m.netCursor = 0

		gotDetail := m.renderNetworkDetail()

		assert.Contains(t, gotDetail, renderKV("id", shortID("net1")))
		assert.Contains(t, gotDetail, renderKV("driver", "bridge"))
		assert.Contains(t, gotDetail, renderKV("subnet", "172.18.0.0/16"))
		assert.Contains(t, gotDetail, "USED BY")
		assert.Contains(t, gotDetail, "web")
	})

	t.Run("shows none when no container uses the network", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.networks = []Network{{ID: "net1", Name: "app_net", Driver: "bridge"}}
		m.netCursor = 0

		gotDetail := m.renderNetworkDetail()

		assert.Contains(t, gotDetail, "USED BY")
		assert.Contains(t, gotDetail, styleDim.Render("none"))
	})
}

func TestViewLayout(t *testing.T) {
	t.Parallel()

	t.Run("view fits the terminal height", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)

		gotView := m.View()

		require.Equal(t, 30, lipgloss.Height(gotView))
	})

	t.Run("header carries the duck", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)

		gotView := m.View()

		require.Contains(t, gotView, "🦆 duck")
	})

	t.Run("empty tabs carry the duck", func(t *testing.T) {
		t.Parallel()

		type testCase struct {
			name string
			tab  tabID
			want string
		}
		tests := []testCase{
			{name: "containers", tab: tabContainers, want: "no containers 🦆"},
			{name: "volumes", tab: tabVolumes, want: "no volumes 🦆"},
			{name: "networks", tab: tabNetworks, want: "no networks 🦆"},
			{name: "images", tab: tabImages, want: "no images 🦆"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
				m = got.(Model)
				m.tab = tc.tab

				gotView := m.View()

				require.Contains(t, gotView, tc.want)
			})
		}
	})

	t.Run("titles row follows the active tab", func(t *testing.T) {
		t.Parallel()

		type testCase struct {
			name      string
			tab       tabID
			wantLeft  string
			wantRight string
		}
		tests := []testCase{
			{name: "containers", tab: tabContainers, wantLeft: " containers", wantRight: " logs"},
			{name: "volumes", tab: tabVolumes, wantLeft: " volumes", wantRight: " info"},
			{name: "networks", tab: tabNetworks, wantLeft: " networks", wantRight: " info"},
			{name: "images", tab: tabImages, wantLeft: " images", wantRight: " info"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
				m = got.(Model)
				m.tab = tc.tab

				titlesRow := strings.Split(m.View(), "\n")[1]

				require.Contains(t, titlesRow, tc.wantLeft)
				require.Contains(t, titlesRow, tc.wantRight)
			})
		}
	})
}

func TestViewResourceFooter(t *testing.T) {
	t.Parallel()

	t.Run("volumes tab shows the in-use hint", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabVolumes
		m.volumes = []Volume{{Name: "data"}}
		m.containers = []Container{{ID: "c1", Volumes: []string{"data"}}}
		m.volCursor = 0

		gotView := m.View()

		require.Contains(t, gotView, "d: volume in use")
	})

	t.Run("volumes tab shows the confirm prompt", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabVolumes
		m.volumes = []Volume{{Name: "data"}}
		m.volCursor = 0
		m.confirm = &pendingDelete{kind: deleteVolume, id: "data", label: "data"}

		gotView := m.View()

		require.Contains(t, gotView, "? y/n")
	})

	t.Run("networks tab shows the in-use hint", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabNetworks
		m.networks = []Network{{ID: "n1", Name: "app-net", Driver: "bridge"}}
		m.containers = []Container{{ID: "c1", Networks: []string{"app-net"}}}
		m.netCursor = 0

		gotView := m.View()

		require.Contains(t, gotView, "d: network in use")
	})

	t.Run("networks tab shows the builtin hint", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabNetworks
		m.networks = []Network{{ID: "n1", Name: "bridge", Driver: "bridge"}}
		m.netCursor = 0

		gotView := m.View()

		require.Contains(t, gotView, "d: builtin network")
	})

	t.Run("networks tab shows the confirm prompt", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabNetworks
		m.networks = []Network{{ID: "n1", Name: "app-net", Driver: "bridge"}}
		m.netCursor = 0
		m.confirm = &pendingDelete{kind: deleteNetwork, id: "n1", label: "app-net"}

		gotView := m.View()

		require.Contains(t, gotView, "? y/n")
	})

	t.Run("images tab shows the in-use hint", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabImages
		m.images = []Image{{ID: "img1", RepoTag: "nginx:latest"}}
		m.containers = []Container{{ID: "c1", ImageID: "img1"}}
		m.imgCursor = 0

		gotView := m.View()

		require.Contains(t, gotView, "d: image in use")
	})

	t.Run("images tab shows the confirm prompt", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabImages
		m.images = []Image{{ID: "img1", RepoTag: "nginx:latest"}}
		m.imgCursor = 0
		m.confirm = &pendingDelete{kind: deleteImage, id: "img1", label: "nginx:latest"}

		gotView := m.View()

		require.Contains(t, gotView, "? y/n")
	})

	t.Run("containers tab shows the default footer", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers

		gotView := m.View()

		require.Contains(t, gotView, "j/k move  tab focus  enter detail  y compose  e exec  s/S stop/start  r restart  p pause  K kill  d delete  P prune  left/right tab  q quit")
	})

	t.Run("containers tab shows the confirm prompt", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.confirm = &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}

		gotView := m.View()

		require.Contains(t, gotView, "? y/n")
	})
}

func TestUpdateNetworkKeys(t *testing.T) {
	t.Parallel()

	t.Run("j moves the cursor down until the last network", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			netCursor  int
			wantCursor int
		}{
			{name: "advances from the first network", netCursor: 0, wantCursor: 1},
			{name: "stays at the last network", netCursor: 1, wantCursor: 1},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.networks = []Network{{Name: "a"}, {Name: "b"}}
				m.netCursor = tc.netCursor

				got, cmd := m.updateNetworkKeys(newTestKeyMsg("j"))

				assert.Equal(t, tc.wantCursor, got.(Model).netCursor)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("k moves the cursor up until the first network", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			netCursor  int
			wantCursor int
		}{
			{name: "retreats from the last network", netCursor: 1, wantCursor: 0},
			{name: "stays at the first network", netCursor: 0, wantCursor: 0},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.networks = []Network{{Name: "a"}, {Name: "b"}}
				m.netCursor = tc.netCursor

				got, cmd := m.updateNetworkKeys(newTestKeyMsg("k"))

				assert.Equal(t, tc.wantCursor, got.(Model).netCursor)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("g jumps to the first network when networks exist", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.networks = []Network{{Name: "a"}, {Name: "b"}}
		m.netCursor = 1

		got, cmd := m.updateNetworkKeys(newTestKeyMsg("g"))

		assert.Equal(t, 0, got.(Model).netCursor)
		assert.Nil(t, cmd)
	})

	t.Run("g is a no-op with no networks", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.netCursor = 0

		got, cmd := m.updateNetworkKeys(newTestKeyMsg("g"))

		assert.Equal(t, 0, got.(Model).netCursor)
		assert.Nil(t, cmd)
	})

	t.Run("G jumps to the last network when networks exist", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.networks = []Network{{Name: "a"}, {Name: "b"}, {Name: "c"}}
		m.netCursor = 0

		got, cmd := m.updateNetworkKeys(newTestKeyMsg("G"))

		assert.Equal(t, 2, got.(Model).netCursor)
		assert.Nil(t, cmd)
	})

	t.Run("G is a no-op with no networks", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.netCursor = 0

		got, cmd := m.updateNetworkKeys(newTestKeyMsg("G"))

		assert.Equal(t, 0, got.(Model).netCursor)
		assert.Nil(t, cmd)
	})

	t.Run("d", func(t *testing.T) {
		t.Parallel()

		t.Run("does nothing for a builtin network", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.networks = []Network{{ID: "n1", Name: "bridge", Driver: "bridge"}}
			m.netCursor = 0

			got, cmd := m.updateNetworkKeys(newTestKeyMsg("d"))

			assert.Nil(t, got.(Model).confirm)
			assert.Nil(t, cmd)
		})

		t.Run("arms confirm for an unused, non-builtin network", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.networks = []Network{{ID: "n1", Name: "app-net", Driver: "bridge"}}
			m.netCursor = 0

			got, cmd := m.updateNetworkKeys(newTestKeyMsg("d"))

			gotModel := got.(Model)
			assert.Equal(t, &pendingDelete{kind: deleteNetwork, id: "n1", label: "app-net"}, gotModel.confirm)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing when a confirm is already pending", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.networks = []Network{{ID: "n1", Name: "app-net", Driver: "bridge"}}
			m.netCursor = 0
			m.confirm = &pendingDelete{kind: deleteNetwork, id: "other", label: "other"}

			got, cmd := m.updateNetworkKeys(newTestKeyMsg("d"))

			assert.Equal(t, "other", got.(Model).confirm.id)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing with an out-of-range cursor", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.netCursor = -1

			got, cmd := m.updateNetworkKeys(newTestKeyMsg("d"))

			assert.Nil(t, got.(Model).confirm)
			assert.Nil(t, cmd)
		})
	})

	t.Run("n and esc clear a pending confirm", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"n", "esc"} {
			t.Run(key, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.confirm = &pendingDelete{kind: deleteNetwork, id: "n1", label: "app-net"}

				got, cmd := m.updateNetworkKeys(newTestKeyMsg(key))

				assert.Nil(t, got.(Model).confirm)
				assert.Nil(t, cmd)
			})
		}
	})

	t.Run("an unrecognized key is a no-op", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.networks = []Network{{Name: "a"}}
		m.netCursor = 0

		got, cmd := m.updateNetworkKeys(newTestKeyMsg("x"))

		assert.Equal(t, 0, got.(Model).netCursor)
		assert.Nil(t, cmd)
	})

	t.Run("y removes the network and clears confirm on success", func(t *testing.T) {
		t.Parallel()

		var gotID string
		resources := newTestResourceClient(nil)
		resources.networkRemove = func(ctx context.Context, networkID string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error) {
			gotID = networkID
			return client.NetworkRemoveResult{}, nil
		}
		m := newTestModel(newTestLogRetargeter(), resources)
		m.confirm = &pendingDelete{kind: deleteNetwork, id: "n1", label: "app-net"}

		got, cmd := m.updateNetworkKeys(newTestKeyMsg("y"))

		assert.Nil(t, got.(Model).confirm)
		require.NotNil(t, cmd)
		assert.Nil(t, cmd())
		assert.Equal(t, "n1", gotID)
	})

	assertScrollForwardsToSubVP(t,
		func() Model {
			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.focus = focusLogs
			m.networks = []Network{{Name: "a"}, {Name: "b"}}
			m.netCursor = 1
			m.subVP.Height = 2
			m.subVP.SetContent(strings.Repeat("line\n", 20))
			return m
		},
		func(m Model, key string) (Model, tea.Cmd) {
			got, cmd := m.updateNetworkKeys(newTestKeyMsg(key))
			return got.(Model), cmd
		},
		func(m Model) int { return m.netCursor },
		1,
	)

	t.Run("esc returns resSubtab to info", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.networks = []Network{{Name: "a"}}
		m.netCursor = 0
		m.resSubtab = subInspect

		assertEscReturnsToInfo(t, m, func(m Model) (Model, tea.Cmd) {
			got, cmd := m.updateNetworkKeys(newTestKeyMsg("esc"))
			return got.(Model), cmd
		})
	})
}

func TestComposeKey(t *testing.T) {
	t.Parallel()

	t.Run("y over a container row produces a composeMsg with the service", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web"}}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("y"))

		require.NotNil(t, cmd)
		got, ok := cmd().(composeMsg)
		require.True(t, ok)
		assert.Contains(t, got.yaml, "services:")
		assert.Contains(t, got.yaml, "web:")
	})

	t.Run("composeMsg assigns m.compose", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))

		got, cmd := m.Update(composeMsg{yaml: "services:\n  web:\n"})

		assert.Equal(t, "services:\n  web:\n", got.(Model).compose)
		assert.Nil(t, cmd)
	})

	t.Run("compose active shows the compose title and esc clears it", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.compose = "services:\n  web:\n"
		m.composeVP.SetContent(m.compose)

		gotView := m.View()

		assert.Contains(t, gotView, " compose")

		got, cmd := m.updateKeys(newTestKeyMsg("esc"))

		assert.Equal(t, "", got.(Model).compose)
		assert.Nil(t, cmd)
	})

	t.Run("y with a pending confirm does not trigger inspect", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.confirm = &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web"}}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("y"))

		require.NotNil(t, cmd)
		assert.Nil(t, cmd())
		require.Len(t, resources.calls, 1)
		assert.Equal(t, testResourceCall{method: "remove", id: "c1"}, resources.calls[0])
	})

	t.Run("y with an invalid cursor does not produce a command", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.tab = tabContainers
		m.focus = focusList
		m.rows = nil
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("y"))

		assert.Nil(t, cmd)
	})

	t.Run("y over a stack row inspects every container of the project", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.containers = []Container{
			{ID: "c1", Project: "app"},
			{ID: "c2", Project: "app"},
			{ID: "c3", Project: "other"},
		}
		m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("y"))

		require.NotNil(t, cmd)
		got, ok := cmd().(composeMsg)
		require.True(t, ok)
		assert.Contains(t, got.yaml, "services:")
		assert.Contains(t, resources.calls, testResourceCall{method: "inspect", id: "c1"})
		assert.Contains(t, resources.calls, testResourceCall{method: "inspect", id: "c2"})
		assert.NotContains(t, resources.calls, testResourceCall{method: "inspect", id: "c3"})
	})

	t.Run("y returns watcherErrMsg when ContainerInspect fails", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("inspect boom")
		resources := newTestResourceClientWithContainerInspectErr(wantErr)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web"}}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("y"))

		require.NotNil(t, cmd)
		got, ok := cmd().(watcherErrMsg)
		require.True(t, ok)
		assert.Equal(t, wantErr, got.err)
	})

	t.Run("y returns watcherErrMsg when ImageInspect fails", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("image inspect boom")
		resources := newTestResourceClientWithImageInspectErr(wantErr)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web"}}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("y"))

		require.NotNil(t, cmd)
		got, ok := cmd().(watcherErrMsg)
		require.True(t, ok)
		assert.Equal(t, wantErr, got.err)
	})

	t.Run("compose active forwards j to the compose viewport", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 10})
		m = got.(Model)
		m.composeVP.Height = 2
		m.compose = strings.Repeat("line\n", 20)
		m.composeVP.SetContent(m.compose)

		gotModel, cmd := m.updateKeys(newTestKeyMsg("j"))

		assert.Equal(t, 1, gotModel.(Model).composeVP.YOffset)
		assert.Nil(t, cmd)
	})
}

func TestDetailView(t *testing.T) {
	t.Parallel()

	t.Run("enter over a container row fills the info subtab with its fields", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.focus = focusList
		c := Container{
			ID:       "c1",
			Name:     "web",
			Image:    "nginx:latest",
			Project:  "app",
			Service:  "web",
			Ports:    []string{"80:8080/tcp"},
			Volumes:  []string{"data"},
			Networks: []string{"app_net"},
		}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: c}}
		m.cursor = 0

		gotModel, cmd := m.updateKeys(newTestKeyMsg("enter"))

		assert.Nil(t, cmd)
		got2 := gotModel.(Model)
		assert.Equal(t, subInfo, got2.subtab)
		detail := got2.subVP.View()
		assert.Contains(t, detail, "web")
		assert.Contains(t, detail, "nginx:latest")
		assert.Contains(t, detail, "80:8080/tcp")
		assert.Contains(t, detail, renderKV("project:", "app"))
		assert.Contains(t, detail, renderKV("service:", "web"))
		assert.Contains(t, detail, "MOUNTS")
		assert.Contains(t, detail, "NETWORKS")
		assert.Contains(t, detail, "app_net")
	})

	t.Run("enter over a stack row fills the info subtab with services and aggregated volumes/networks", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.focus = focusList
		m.containers = []Container{
			{ID: "c1", Name: "app_web_1", Project: "app", Service: "web", State: "running", Volumes: []string{"data"}, Networks: []string{"app_net"}, Ports: []string{"80:8080/tcp"}},
			{ID: "c2", Name: "app_db_1", Project: "app", Volumes: []string{"db-data"}, Networks: []string{"app_net"}},
		}
		m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
		m.cursor = 0

		gotModel, cmd := m.updateKeys(newTestKeyMsg("enter"))

		assert.NotNil(t, cmd)
		got2 := gotModel.(Model)
		assert.Equal(t, subInfo, got2.subtab)
		detail := got2.subVP.View()
		assert.Contains(t, detail, "SERVICES")
		assert.Contains(t, detail, "web")
		assert.Contains(t, detail, "app_db_1")
		assert.Contains(t, detail, "data")
		assert.Contains(t, detail, "db-data")
		assert.Contains(t, detail, "app_net")
		assert.Contains(t, detail, "PORTS")
		assert.Contains(t, detail, "80:8080/tcp")
	})

	t.Run("enter over a stack row with an empty project renders standalone", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.focus = focusList
		m.containers = []Container{{ID: "c1", Name: "standalone_web"}}
		m.rows = []row{{kind: rowStack, key: "stack:", project: ""}}
		m.cursor = 0

		gotModel, cmd := m.updateKeys(newTestKeyMsg("enter"))

		assert.Nil(t, cmd)
		detail := gotModel.(Model).subVP.View()
		assert.Contains(t, detail, renderKV("project:", "standalone"))
	})

	t.Run("statsMsg for a stack row's info subtab includes per-container cpu and mem", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.focus = focusList
		m.containers = []Container{{ID: "c1", Name: "app_web_1", Project: "app", Service: "web", State: "running"}}
		m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
		m.cursor = 0

		gotModel, _ := m.updateKeys(newTestKeyMsg("enter"))
		m = gotModel.(Model)
		gotModel, _ = m.Update(statsMsg{stats: []ContainerStat{{ID: "c1", CPUPercent: 12.3, MemUsage: 1024, MemLimit: 4096}}})

		detail := gotModel.(Model).subVP.View()
		assert.Contains(t, detail, "cpu 12.3%")
		assert.Contains(t, detail, "mem "+formatMemBytes(1024))
	})

	t.Run("info subtab active shows the subtab title and scroll footer, esc returns to logs", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web"}}}
		m.cursor = 0
		m.subtab = subInfo
		m.syncSubVP()

		gotView := m.View()

		assert.Contains(t, gotView, "info")
		assert.Contains(t, gotView, "h/l view  j/k scroll  esc logs  q quit")

		got, cmd := m.updateKeys(newTestKeyMsg("esc"))

		gotModel := got.(Model)
		assert.Equal(t, subLogs, gotModel.subtab)
		assert.Nil(t, cmd)
		assert.Contains(t, gotModel.View(), " logs")
	})

	t.Run("non-esc key while a subtab is active and logs are focused is forwarded to the subtab viewport", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.focus = focusLogs
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web"}}}
		m.cursor = 0
		m.subtab = subInfo
		m.syncSubVP()

		got, _ = m.updateKeys(newTestKeyMsg("down"))

		gotModel := got.(Model)
		assert.Equal(t, subInfo, gotModel.subtab)
	})

	t.Run("enter with a pending confirm does not switch subtabs", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.tab = tabContainers
		m.focus = focusList
		m.confirm = &pendingDelete{kind: deleteContainer, id: "c1", label: "web"}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web"}}}
		m.cursor = 0

		gotModel, cmd := m.updateKeys(newTestKeyMsg("enter"))

		assert.Equal(t, subLogs, gotModel.(Model).subtab)
		assert.Nil(t, cmd)
	})

	t.Run("default footer mentions enter detail and y compose", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers

		gotView := m.View()

		assert.Contains(t, gotView, "enter detail")
		assert.Contains(t, gotView, "y compose")
	})

	t.Run("enter on a running container returns a batched cmd producing statsMsg with the container stat", func(t *testing.T) {
		t.Parallel()

		statsJSON := `{"cpu_stats":{"cpu_usage":{"total_usage":150},"system_cpu_usage":2000,"online_cpus":4},"precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},"memory_stats":{"usage":1024,"limit":4096}}`
		resources := newTestResourceClient(nil)
		resources.containerStats = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
			return client.ContainerStatsResult{Body: io.NopCloser(strings.NewReader(statsJSON))}, nil
		}
		m := newTestModel(newTestLogRetargeter(), resources)
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.focus = focusList
		c := Container{ID: "c1", Name: "web", State: "running"}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: c}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("enter"))

		require.NotNil(t, cmd)
		batch, ok := cmd().(tea.BatchMsg)
		require.True(t, ok)
		want := statsMsg{stats: []ContainerStat{{ID: "c1", CPUPercent: 20.0, MemUsage: 1024, MemLimit: 4096}}}
		var gotStats tea.Msg
		for _, sub := range batch {
			if msg, ok := sub().(statsMsg); ok {
				gotStats = msg
			}
		}
		assert.Equal(t, want, gotStats)
	})

	t.Run("enter on a running container returns a batched cmd producing detailExtraMsg with env and procs", func(t *testing.T) {
		t.Parallel()

		statsJSON := `{"cpu_stats":{"cpu_usage":{"total_usage":150},"system_cpu_usage":2000,"online_cpus":4},"precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},"memory_stats":{"usage":1024,"limit":4096}}`
		resources := newTestResourceClient(nil)
		resources.containerStats = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
			return client.ContainerStatsResult{Body: io.NopCloser(strings.NewReader(statsJSON))}, nil
		}
		m := newTestModel(newTestLogRetargeter(), resources)
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.focus = focusList
		c := Container{ID: "c1", Name: "web", State: "running"}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: c}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("enter"))

		require.NotNil(t, cmd)
		batch, ok := cmd().(tea.BatchMsg)
		require.True(t, ok)
		var gotExtra tea.Msg
		for _, sub := range batch {
			if msg, ok := sub().(detailExtraMsg); ok {
				gotExtra = msg
			}
		}
		require.IsType(t, detailExtraMsg{}, gotExtra)
		gotDetail := gotExtra.(detailExtraMsg)
		assert.Equal(t, "c1", gotDetail.id)
		assert.Equal(t, []string{"FOO=bar"}, gotDetail.extra.env)
		assert.Equal(t, []string{"PID", "CMD"}, gotDetail.extra.titles)
		assert.Equal(t, [][]string{{"123", "nginx"}}, gotDetail.extra.procs)
		assert.NotEmpty(t, gotDetail.extra.inspect)
	})

	t.Run("enter on a stopped container returns no stats cmd", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.focus = focusList
		c := Container{ID: "c1", Name: "web", State: "exited"}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: c}}
		m.cursor = 0

		_, cmd := m.updateKeys(newTestKeyMsg("enter"))

		assert.Nil(t, cmd)
	})

	t.Run("esc returns the subtab to logs", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}}}
		m.cursor = 0
		m.subtab = subInfo
		m.syncSubVP()

		got, _ = m.updateKeys(newTestKeyMsg("esc"))

		assert.Equal(t, subLogs, got.(Model).subtab)
	})

	t.Run("statsMsg with the info subtab open re-renders it with cpu and mem", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		c := Container{ID: "c1", Name: "web", State: "running"}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: c}}
		m.cursor = 0
		m.subtab = subInfo
		m.syncSubVP()

		got, _ = m.Update(statsMsg{stats: []ContainerStat{{ID: "c1", CPUPercent: 20.0, MemUsage: 1024, MemLimit: 4096}}})

		gotModel := got.(Model)
		detail := gotModel.subVP.View()
		assert.Contains(t, detail, "cpu: ")
		assert.Contains(t, detail, "mem: ")
	})

	t.Run("statsMsg with the logs subtab open only merges stats without touching the subtab", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)

		got, _ = m.Update(statsMsg{stats: []ContainerStat{{ID: "c1", CPUPercent: 20.0, MemUsage: 1024, MemLimit: 4096}}})

		gotModel := got.(Model)
		assert.Equal(t, subLogs, gotModel.subtab)
		assert.Equal(t, ContainerStat{ID: "c1", CPUPercent: 20.0, MemUsage: 1024, MemLimit: 4096}, gotModel.stats["c1"])
	})

	t.Run("detailExtraMsg with the info subtab open does not render env or top", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		c := Container{ID: "c1", Name: "web", State: "running"}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: c}}
		m.cursor = 0
		m.subtab = subInfo
		m.syncSubVP()
		extra := detailExtra{
			env:    []string{"FOO=bar"},
			titles: []string{"PID", "CMD"},
			procs:  [][]string{{"123", "nginx"}},
		}

		got, _ = m.Update(detailExtraMsg{id: "c1", extra: extra})

		gotModel := got.(Model)
		detail := gotModel.subVP.View()
		assert.NotContains(t, detail, "env:")
		assert.NotContains(t, detail, "top:")
		assert.NotContains(t, detail, "FOO=bar")
	})

	t.Run("container detail MOUNTS section uses extra.mounts when present", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		c := Container{ID: "c1", Name: "web", State: "running", Volumes: []string{"data"}}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: c}}
		m.cursor = 0
		m.subtab = subInfo
		m.syncSubVP()
		extra := detailExtra{mounts: []string{"data -> /var/lib/data"}}

		got, _ = m.Update(detailExtraMsg{id: "c1", extra: extra})

		detail := got.(Model).subVP.View()
		assert.Contains(t, detail, "MOUNTS")
		assert.Contains(t, detail, "data -> /var/lib/data")
	})

	t.Run("container detail MOUNTS section falls back to c.Volumes without extra", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		c := Container{ID: "c1", Name: "web", State: "running", Volumes: []string{"data"}}
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: c}}
		m.cursor = 0
		m.subtab = subInfo
		m.syncSubVP()

		detail := m.subVP.View()
		assert.Contains(t, detail, "MOUNTS")
		assert.Contains(t, detail, "data")
	})

	t.Run("detailExtraMsg with a stack's info subtab open re-renders the stack detail", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
		m.cursor = 0
		m.subtab = subInfo
		m.syncSubVP()
		extra := detailExtra{
			env:    []string{"FOO=bar"},
			titles: []string{"PID", "CMD"},
			procs:  [][]string{{"123", "nginx"}},
		}

		got, _ = m.Update(detailExtraMsg{id: "c1", extra: extra})

		gotModel := got.(Model)
		assert.Contains(t, gotModel.subVP.View(), renderKV("project:", "app"))
	})

	t.Run("detailExtraMsg with the logs subtab open only merges extras without touching the subtab", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		extra := detailExtra{
			env:    []string{"FOO=bar"},
			titles: []string{"PID", "CMD"},
			procs:  [][]string{{"123", "nginx"}},
		}

		got, _ = m.Update(detailExtraMsg{id: "c1", extra: extra})

		gotModel := got.(Model)
		assert.Equal(t, subLogs, gotModel.subtab)
		assert.Equal(t, extra, gotModel.extras["c1"])
	})
}

func newTestSubtabModel(t *testing.T) Model {
	t.Helper()

	m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
	got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = got.(Model)
	m.tab = tabContainers
	return m
}

func newTestSubtabViewModel(t *testing.T, subtab subtabID) Model {
	t.Helper()

	m := newTestSubtabModel(t)
	m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
	m.cursor = 0
	m.subtab = subtab
	m.syncSubVP()
	return m
}

func TestSubtabs(t *testing.T) {
	t.Parallel()

	t.Run("] cycles logs->info->env", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}}}
		m.cursor = 0

		got, _ := m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)
		assert.Equal(t, subInfo, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)
		assert.Equal(t, subEnv, m.subtab)
	})

	t.Run("l cycles logs->info->env like ]", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}}}
		m.cursor = 0

		got, _ := m.updateKeys(newTestKeyMsg("l"))
		m = got.(Model)
		assert.Equal(t, subInfo, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("l"))
		m = got.(Model)
		assert.Equal(t, subEnv, m.subtab)
	})

	t.Run("h steps back with wrap like [", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}}}
		m.cursor = 0
		m.subtab = subEnv

		got, _ := m.updateKeys(newTestKeyMsg("h"))
		m = got.(Model)
		assert.Equal(t, subInfo, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("h"))
		m = got.(Model)
		assert.Equal(t, subLogs, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("h"))
		m = got.(Model)
		assert.Equal(t, subInspect, m.subtab)
	})

	t.Run("[ steps back with wrap", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}}}
		m.cursor = 0
		m.subtab = subEnv

		got, _ := m.updateKeys(newTestKeyMsg("["))
		m = got.(Model)
		assert.Equal(t, subInfo, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("["))
		m = got.(Model)
		assert.Equal(t, subLogs, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("["))
		m = got.(Model)
		assert.Equal(t, subInspect, m.subtab)
	})

	t.Run("] is a no-op with an out-of-range cursor", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = nil
		m.cursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("]"))

		assert.Equal(t, subLogs, got.(Model).subtab)
		assert.Nil(t, cmd)
	})

	t.Run("over a stack row ] only alternates logs and info", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
		m.cursor = 0

		got, _ := m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)
		assert.Equal(t, subInfo, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)
		assert.Equal(t, subLogs, m.subtab)
	})

	t.Run("moving the cursor from a container to a stack while in env clamps to info", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{
			{kind: rowStack, key: "stack:app", project: "app"},
			{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}},
		}
		m.cursor = 1
		m.subtab = subEnv

		got, _ := m.updateKeys(newTestKeyMsg("k"))

		gotModel := got.(Model)
		assert.Equal(t, 0, gotModel.cursor)
		assert.Equal(t, subInfo, gotModel.subtab)
	})

	t.Run("the right panel title contains the active subtab's label", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}}}
		m.cursor = 0
		m.subtab = subEnv

		titlesRow := strings.Split(m.View(), "\n")[1]

		assert.Contains(t, titlesRow, "env")
	})

	t.Run("the footer in a non-logs subtab contains h/l view", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.subtab = subInfo

		gotView := m.View()

		assert.Contains(t, gotView, "h/l view")
	})

	t.Run("subEnv without a cached extra shows loading", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subEnv
		m.syncSubVP()

		assert.Contains(t, m.View(), "loading...")
	})

	t.Run("subEnv with an injected extra shows the env var and the ENV header", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subEnv
		m.syncSubVP()

		got, _ := m.Update(detailExtraMsg{id: "c1", extra: detailExtra{env: []string{"FOO=bar"}}})

		gotView := got.(Model).View()
		assert.Contains(t, gotView, "FOO=bar")
		assert.Contains(t, gotView, "ENV")
	})

	t.Run("subEnv with an injected empty extra shows no env", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subEnv
		m.syncSubVP()

		got, _ := m.Update(detailExtraMsg{id: "c1", extra: detailExtra{env: nil}})

		gotView := got.(Model).View()
		assert.Contains(t, gotView, "no env")
	})

	t.Run("subTop with data aligns columns and shows PROCESSES", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subTop
		m.syncSubVP()
		extra := detailExtra{titles: []string{"PID", "CMD"}, procs: [][]string{{"123", "nginx"}}}

		got, _ := m.Update(detailExtraMsg{id: "c1", extra: extra})

		gotView := got.(Model).View()
		assert.Contains(t, gotView, "PROCESSES")
		assert.Contains(t, gotView, "123")
		assert.Contains(t, gotView, "nginx")
	})

	t.Run("subTop over a stopped container shows container not running", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "exited"}}}
		m.cursor = 0
		m.subtab = subTop
		m.syncSubVP()

		assert.Contains(t, m.View(), "container not running")
	})

	t.Run("subInspect without a cached extra shows loading", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subInspect
		m.syncSubVP()

		assert.Contains(t, m.View(), "loading...")
	})

	t.Run("subInspect with an injected extra shows the container JSON", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subInspect
		m.syncSubVP()

		got, _ := m.Update(detailExtraMsg{id: "c1", extra: detailExtra{inspect: `{
  "Name": "c1"
}`}})

		gotView := got.(Model).View()
		assert.Contains(t, gotView, `"Name"`)
	})

	t.Run("subStats over a stopped container shows container not running", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web", State: "exited"}}}
		m.cursor = 0
		m.subtab = subStats
		m.syncSubVP()

		assert.Contains(t, m.View(), "container not running")
	})

	t.Run("subStats without a sample shows sampling, then a statsMsg fills in cpu", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", Name: "web", State: "running"}}}
		m.cursor = 0
		m.subtab = subStats
		m.syncSubVP()

		assert.Contains(t, m.View(), "sampling...")

		got, _ := m.Update(statsMsg{stats: []ContainerStat{{ID: "c1", CPUPercent: 12.5, MemUsage: 1e6, MemLimit: 2e6}}})

		assert.Contains(t, got.(Model).View(), "cpu")
	})

	t.Run("entering subStats over a running container starts the tick loop", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subTop

		got, cmd := m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)

		assert.Equal(t, subStats, m.subtab)
		assert.True(t, m.statsTicking)
		assert.NotNil(t, cmd)
	})

	t.Run("statsTickStartCmd with cursor out of range returns nil", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 1

		cmd := m.statsTickStartCmd()

		assert.Nil(t, cmd)
	})

	t.Run("statsTickStartCmd while already ticking returns a cmd without re-batching the tick", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subStats
		m.statsTicking = true

		cmd := m.statsTickStartCmd()

		require.NotNil(t, cmd)
		assert.True(t, m.statsTicking)
	})

	t.Run("statsTickMsg with subStats active over a running container returns a non-nil cmd", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subStats
		m.statsTicking = true

		got, cmd := m.Update(statsTickMsg{})

		assert.True(t, got.(Model).statsTicking)
		assert.NotNil(t, cmd)
	})

	t.Run("statsTickMsg after leaving subStats turns off statsTicking and returns a nil cmd", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.subtab = subInfo
		m.statsTicking = true

		got, cmd := m.Update(statsTickMsg{})

		assert.False(t, got.(Model).statsTicking)
		assert.Nil(t, cmd)
	})

	t.Run("entering subEnv without a cached extra dispatches a fetch cmd", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0

		got, _ := m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)
		got, cmd := m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)

		assert.Equal(t, subEnv, m.subtab)
		assert.NotNil(t, cmd)
	})

	t.Run("entering subEnv with a cached extra does not dispatch a fetch cmd", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1", State: "running"}}}
		m.cursor = 0
		m.extras = map[string]detailExtra{"c1": {env: []string{"FOO=bar"}}}

		got, _ := m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)
		got, cmd := m.updateKeys(newTestKeyMsg("]"))
		m = got.(Model)

		assert.Equal(t, subEnv, m.subtab)
		assert.Nil(t, cmd)
	})

	t.Run("l cycles through subVols and subNets before subInspect", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabModel(t)
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}}}
		m.cursor = 0
		m.subtab = subStats

		got, _ := m.updateKeys(newTestKeyMsg("l"))
		m = got.(Model)
		assert.Equal(t, subVols, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("l"))
		m = got.(Model)
		assert.Equal(t, subNets, m.subtab)

		got, _ = m.updateKeys(newTestKeyMsg("l"))
		m = got.(Model)
		assert.Equal(t, subInspect, m.subtab)
	})

	t.Run("subVols without a cached extra shows loading", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabViewModel(t, subVols)

		assert.Contains(t, m.View(), "loading...")
	})

	t.Run("subNets without a cached extra shows loading", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabViewModel(t, subNets)

		assert.Contains(t, m.View(), "loading...")
	})

	t.Run("subVols with an injected empty extra shows no mounts", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabViewModel(t, subVols)

		got, _ := m.Update(detailExtraMsg{id: "c1", extra: detailExtra{mountDetails: nil}})

		assert.Contains(t, got.(Model).View(), "no mounts")
	})

	t.Run("subNets with an injected empty extra shows no networks", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabViewModel(t, subNets)

		got, _ := m.Update(detailExtraMsg{id: "c1", extra: detailExtra{netDetails: nil}})

		assert.Contains(t, got.(Model).View(), "no networks")
	})

	t.Run("subVols with an injected extra shows the mount name, destination and mode", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabViewModel(t, subVols)

		extra := detailExtra{mountDetails: []mountDetail{{name: "data", destination: "/data", kind: "volume", rw: true}}}
		got, _ := m.Update(detailExtraMsg{id: "c1", extra: extra})

		gotView := got.(Model).View()
		assert.Contains(t, gotView, "MOUNTS")
		assert.Contains(t, gotView, "data -> /data")
		assert.Contains(t, gotView, "(volume,rw)")
	})

	t.Run("subNets with an injected extra shows the network section and ip", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabViewModel(t, subNets)

		extra := detailExtra{netDetails: []netDetail{{name: "app_default", ip: "172.18.0.2", gateway: "172.18.0.1"}}}
		got, _ := m.Update(detailExtraMsg{id: "c1", extra: extra})

		gotView := got.(Model).View()
		assert.Contains(t, gotView, "APP_DEFAULT")
		assert.Contains(t, gotView, "ip:")
		assert.Contains(t, gotView, "172.18.0.2")
	})

	t.Run("subNets with an injected extra shows the aliases", func(t *testing.T) {
		t.Parallel()

		m := newTestSubtabViewModel(t, subNets)

		extra := detailExtra{netDetails: []netDetail{{name: "app_default", ip: "172.18.0.2", aliases: []string{"web", "web-1"}}}}
		got, _ := m.Update(detailExtraMsg{id: "c1", extra: extra})

		gotView := got.(Model).View()
		assert.Contains(t, gotView, "aliases:")
		assert.Contains(t, gotView, "web, web-1")
	})
}

type testResourceClientWithInspectErr struct {
	containerInspectErr error
	imageInspectErr     error
	volumeInspectErr    error
	networkInspectErr   error
	containerTopErr     error
	calls               []testResourceCall
}

func newTestResourceClientWithContainerInspectErr(err error) *testResourceClientWithInspectErr {
	return &testResourceClientWithInspectErr{containerInspectErr: err}
}

func newTestResourceClientWithImageInspectErr(err error) *testResourceClientWithInspectErr {
	return &testResourceClientWithInspectErr{imageInspectErr: err}
}

func newTestResourceClientWithVolumeInspectErr(err error) *testResourceClientWithInspectErr {
	return &testResourceClientWithInspectErr{volumeInspectErr: err}
}

func newTestResourceClientWithNetworkInspectErr(err error) *testResourceClientWithInspectErr {
	return &testResourceClientWithInspectErr{networkInspectErr: err}
}

func newTestResourceClientWithContainerTopErr(err error) *testResourceClientWithInspectErr {
	return &testResourceClientWithInspectErr{containerTopErr: err}
}

func (c *testResourceClientWithInspectErr) VolumeRemove(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error) {
	return client.VolumeRemoveResult{}, nil
}

func (c *testResourceClientWithInspectErr) NetworkRemove(ctx context.Context, networkID string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error) {
	return client.NetworkRemoveResult{}, nil
}

func (c *testResourceClientWithInspectErr) VolumeInspect(ctx context.Context, volumeID string, options client.VolumeInspectOptions) (client.VolumeInspectResult, error) {
	return client.VolumeInspectResult{}, c.volumeInspectErr
}

func (c *testResourceClientWithInspectErr) NetworkInspect(ctx context.Context, networkID string, options client.NetworkInspectOptions) (client.NetworkInspectResult, error) {
	return client.NetworkInspectResult{}, c.networkInspectErr
}

func (c *testResourceClientWithInspectErr) ImageRemove(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	return client.ImageRemoveResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error) {
	return client.ContainerStartResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
	return client.ContainerStopResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	return client.ContainerRestartResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerKill(ctx context.Context, containerID string, options client.ContainerKillOptions) (client.ContainerKillResult, error) {
	return client.ContainerKillResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerPause(ctx context.Context, containerID string, options client.ContainerPauseOptions) (client.ContainerPauseResult, error) {
	return client.ContainerPauseResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerUnpause(ctx context.Context, containerID string, options client.ContainerUnpauseOptions) (client.ContainerUnpauseResult, error) {
	return client.ContainerUnpauseResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	return client.ContainerRemoveResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "inspect", id: containerID})
	if c.containerInspectErr != nil {
		return client.ContainerInspectResult{}, c.containerInspectErr
	}
	return client.ContainerInspectResult{
		Container: container.InspectResponse{
			Name: "/web",
			Config: &container.Config{
				Image:  "nginx:latest",
				Labels: map[string]string{"com.docker.compose.service": "web"},
			},
		},
	}, nil
}

func (c *testResourceClientWithInspectErr) ImageInspect(ctx context.Context, imageID string, inspectOpts ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	c.calls = append(c.calls, testResourceCall{method: "image-inspect", id: imageID})
	if c.imageInspectErr != nil {
		return client.ImageInspectResult{}, c.imageInspectErr
	}
	return client.ImageInspectResult{}, nil
}

// testCloseErrReader wraps a string body but returns a caller-supplied error
// from Close, for exercising statsCmd's close-error branch.
type testCloseErrReader struct {
	io.Reader
	closeErr error
}

func newTestCloseErrReader(body string, closeErr error) *testCloseErrReader {
	return &testCloseErrReader{Reader: strings.NewReader(body), closeErr: closeErr}
}

func (r *testCloseErrReader) Close() error { return r.closeErr }

func TestStatsCmd(t *testing.T) {
	t.Parallel()

	t.Run("ContainerStats error returns watcherErrMsg", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("stats boom")
		resources := newTestResourceClient(nil)
		resources.containerStats = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
			return client.ContainerStatsResult{}, wantErr
		}
		m := newTestModel(newTestLogRetargeter(), resources)

		cmd := m.statsCmd([]string{"c1"})

		require.NotNil(t, cmd)
		assert.Equal(t, watcherErrMsg{err: wantErr}, cmd())
	})

	t.Run("decode error returns watcherErrMsg", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		resources.containerStats = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
			return client.ContainerStatsResult{Body: io.NopCloser(strings.NewReader("not json"))}, nil
		}
		m := newTestModel(newTestLogRetargeter(), resources)

		cmd := m.statsCmd([]string{"c1"})

		require.NotNil(t, cmd)
		got, ok := cmd().(watcherErrMsg)
		require.True(t, ok)
		assert.Error(t, got.err)
	})

	t.Run("body close error returns watcherErrMsg", func(t *testing.T) {
		t.Parallel()

		statsJSON := `{"cpu_stats":{"cpu_usage":{"total_usage":150},"system_cpu_usage":2000,"online_cpus":4},"precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},"memory_stats":{"usage":1024,"limit":4096}}`
		wantErr := errors.New("close boom")
		resources := newTestResourceClient(nil)
		resources.containerStats = func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
			return client.ContainerStatsResult{Body: newTestCloseErrReader(statsJSON, wantErr)}, nil
		}
		m := newTestModel(newTestLogRetargeter(), resources)

		cmd := m.statsCmd([]string{"c1"})

		require.NotNil(t, cmd)
		assert.Equal(t, watcherErrMsg{err: wantErr}, cmd())
	})
}

func TestDetailExtraCmd(t *testing.T) {
	t.Parallel()

	t.Run("ContainerInspect error returns watcherErrMsg", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("inspect boom")
		m := newTestModel(newTestLogRetargeter(), newTestResourceClientWithContainerInspectErr(wantErr))

		cmd := m.detailExtraCmd("c1", true)

		require.NotNil(t, cmd)
		assert.Equal(t, watcherErrMsg{err: wantErr}, cmd())
	})

	t.Run("ContainerTop error returns watcherErrMsg", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("top boom")
		m := newTestModel(newTestLogRetargeter(), newTestResourceClientWithContainerTopErr(wantErr))

		cmd := m.detailExtraCmd("c1", true)

		require.NotNil(t, cmd)
		assert.Equal(t, watcherErrMsg{err: wantErr}, cmd())
	})
}

func TestDetailExtraCmdMounts(t *testing.T) {
	t.Parallel()

	client := newTestResourceClient(nil)
	client.containerInspect.Container.Mounts = []container.MountPoint{
		{Type: mount.TypeBind, Source: "/host/path", Destination: "/data", RW: false},
		{Type: mount.TypeVolume, Name: "db-data", Destination: "/var/lib/data", RW: true},
	}
	client.containerInspect.Container.NetworkSettings = &container.NetworkSettings{
		Networks: map[string]*network.EndpointSettings{
			"bridge": {
				IPAddress: netip.MustParseAddr("172.17.0.2"),
				Gateway:   netip.MustParseAddr("172.17.0.1"),
				Aliases:   []string{"web", "web-1"},
			},
			"empty": {},
		},
	}
	m := newTestModel(newTestLogRetargeter(), client)

	cmd := m.detailExtraCmd("c1", true)

	require.NotNil(t, cmd)
	got, ok := cmd().(detailExtraMsg)
	require.True(t, ok)
	assert.Equal(t, []string{"/host/path -> /data", "db-data -> /var/lib/data"}, got.extra.mounts)
	assert.Equal(t, []mountDetail{
		{name: "/host/path", destination: "/data", kind: "bind", rw: false},
		{name: "db-data", destination: "/var/lib/data", kind: "volume", rw: true},
	}, got.extra.mountDetails)
	assert.Equal(t, []netDetail{
		{name: "bridge", ip: "172.17.0.2", gateway: "172.17.0.1", aliases: []string{"web", "web-1"}},
		{name: "empty", ip: "", gateway: "", aliases: nil},
	}, got.extra.netDetails)
}

func TestLazyExtraCmd(t *testing.T) {
	t.Parallel()

	t.Run("cursor out of bounds returns nil", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.subtab = subEnv
		m.rows = []row{{kind: rowContainer, key: "id:c1", container: Container{ID: "c1"}}}
		m.cursor = 1

		cmd := m.lazyExtraCmd()

		assert.Nil(t, cmd)
	})

	t.Run("non-container row returns nil", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.subtab = subEnv
		m.rows = []row{{kind: rowStack, key: "stack:app", project: "app"}}
		m.cursor = 0

		cmd := m.lazyExtraCmd()

		assert.Nil(t, cmd)
	})
}

func TestResourceInspectCmd(t *testing.T) {
	t.Parallel()

	newVolumeInspectModel := func(resources resourceClient) Model {
		m := newTestModel(newTestLogRetargeter(), resources)
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabVolumes
		m.resSubtab = subInspect
		m.volumes = []Volume{{Name: "data-vol"}}
		m.volCursor = 0
		return m
	}
	newNetworkInspectModel := func(resources resourceClient) Model {
		m := newTestModel(newTestLogRetargeter(), resources)
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabNetworks
		m.resSubtab = subInspect
		m.networks = []Network{{Name: "net1"}}
		m.netCursor = 0
		return m
	}
	newImageInspectModel := func(resources resourceClient) Model {
		m := newTestModel(newTestLogRetargeter(), resources)
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabImages
		m.resSubtab = subInspect
		m.images = []Image{{ID: "sha256:abc"}}
		m.imgCursor = 0
		return m
	}

	inspectTests := []struct {
		name     string
		setup    func() (*testResourceClient, Model)
		wantKey  string
		wantJSON string
		wantCall testResourceCall
	}{
		{
			name: "volume",
			setup: func() (*testResourceClient, Model) {
				resources := newTestResourceClient(nil)
				resources.volumeInspect = client.VolumeInspectResult{Volume: volume.Volume{Name: "data-vol"}}
				return resources, newVolumeInspectModel(resources)
			},
			wantKey:  "volume:data-vol",
			wantJSON: `"Name": "data-vol"`,
			wantCall: testResourceCall{method: "volume-inspect", id: "data-vol"},
		},
		{
			name: "network",
			setup: func() (*testResourceClient, Model) {
				resources := newTestResourceClient(nil)
				resources.networkInspect = client.NetworkInspectResult{Network: network.Inspect{Network: network.Network{Name: "net1"}}}
				return resources, newNetworkInspectModel(resources)
			},
			wantKey:  "network:net1",
			wantJSON: `"Name": "net1"`,
			wantCall: testResourceCall{method: "network-inspect", id: "net1"},
		},
		{
			name: "image",
			setup: func() (*testResourceClient, Model) {
				resources := newTestResourceClient(nil)
				resources.imageInspect = client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: "sha256:abc"}}
				return resources, newImageInspectModel(resources)
			},
			wantKey:  "image:sha256:abc",
			wantJSON: `"Id": "sha256:abc"`,
			wantCall: testResourceCall{method: "image-inspect", id: "sha256:abc"},
		},
	}

	t.Run("entering inspect triggers the SDK inspect call and returns the resource's JSON", func(t *testing.T) {
		t.Parallel()

		for _, tc := range inspectTests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				resources, m := tc.setup()

				cmd := m.resourceInspectCmd()

				require.NotNil(t, cmd)
				msg, ok := cmd().(resourceInspectMsg)
				require.True(t, ok)
				assert.Equal(t, tc.wantKey, msg.key)
				assert.Contains(t, msg.inspect, tc.wantJSON)
				assert.Equal(t, []testResourceCall{tc.wantCall}, resources.calls)
			})
		}
	})

	t.Run("Update with the resourceInspectMsg renders the JSON into the subVP", func(t *testing.T) {
		t.Parallel()

		for _, tc := range inspectTests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				_, m := tc.setup()
				cmd := m.resourceInspectCmd()
				require.NotNil(t, cmd)
				msg, ok := cmd().(resourceInspectMsg)
				require.True(t, ok)

				got, _ := m.Update(msg)
				gotModel := got.(Model)
				assert.Contains(t, gotModel.subVP.View(), tc.wantJSON)
			})
		}
	})

	t.Run("cached key returns nil cmd and does not call the SDK again", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		resources.volumeInspect = client.VolumeInspectResult{Volume: volume.Volume{Name: "data-vol"}}
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabVolumes
		m.resSubtab = subInspect
		m.volumes = []Volume{{Name: "data-vol"}}
		m.volCursor = 0
		m.resInspect = map[string]string{"volume:data-vol": "cached inspect output"}

		cmd := m.resourceInspectCmd()

		assert.Nil(t, cmd)
		assert.Empty(t, resources.calls)
	})

	t.Run("inspect error renders error: ... in the panel", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			setup func(err error) Model
		}{
			{
				name: "image",
				setup: func(err error) Model {
					return newImageInspectModel(newTestResourceClientWithImageInspectErr(err))
				},
			},
			{
				name: "volume",
				setup: func(err error) Model {
					return newVolumeInspectModel(newTestResourceClientWithVolumeInspectErr(err))
				},
			},
			{
				name: "network",
				setup: func(err error) Model {
					return newNetworkInspectModel(newTestResourceClientWithNetworkInspectErr(err))
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				wantErr := errors.New(tc.name + " inspect boom")
				m := tc.setup(wantErr)

				cmd := m.resourceInspectCmd()

				require.NotNil(t, cmd)
				msg, ok := cmd().(resourceInspectMsg)
				require.True(t, ok)
				assert.Equal(t, "error: "+wantErr.Error(), msg.inspect)

				got, _ := m.Update(msg)
				gotModel := got.(Model)
				assert.Contains(t, gotModel.subVP.View(), "error: "+wantErr.Error())
			})
		}
	})

	t.Run("empty resourceKey returns nil cmd", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.tab = tabVolumes
		m.resSubtab = subInspect
		m.volCursor = -1

		cmd := m.resourceInspectCmd()

		assert.Nil(t, cmd)
	})

	t.Run("marshal error renders error: ... in the panel", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		resources.volumeInspect = client.VolumeInspectResult{
			Volume: volume.Volume{Name: "data-vol", Status: map[string]any{"bad": math.NaN()}},
		}
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabVolumes
		m.resSubtab = subInspect
		m.volumes = []Volume{{Name: "data-vol"}}
		m.volCursor = 0

		cmd := m.resourceInspectCmd()

		require.NotNil(t, cmd)
		msg, ok := cmd().(resourceInspectMsg)
		require.True(t, ok)
		assert.Contains(t, msg.inspect, "error: ")
	})
}

func (c *testResourceClientWithInspectErr) ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	return client.ContainerStatsResult{Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func (c *testResourceClientWithInspectErr) ContainerTop(ctx context.Context, containerID string, options client.ContainerTopOptions) (client.ContainerTopResult, error) {
	if c.containerTopErr != nil {
		return client.ContainerTopResult{}, c.containerTopErr
	}
	return client.ContainerTopResult{}, nil
}

func (c *testResourceClientWithInspectErr) ContainerPrune(ctx context.Context, opts client.ContainerPruneOptions) (client.ContainerPruneResult, error) {
	return client.ContainerPruneResult{}, nil
}

func (c *testResourceClientWithInspectErr) ImagePrune(ctx context.Context, opts client.ImagePruneOptions) (client.ImagePruneResult, error) {
	return client.ImagePruneResult{}, nil
}

func (c *testResourceClientWithInspectErr) VolumePrune(ctx context.Context, options client.VolumePruneOptions) (client.VolumePruneResult, error) {
	return client.VolumePruneResult{}, nil
}

func (c *testResourceClientWithInspectErr) NetworkPrune(ctx context.Context, opts client.NetworkPruneOptions) (client.NetworkPruneResult, error) {
	return client.NetworkPruneResult{}, nil
}

func TestFilter(t *testing.T) {
	t.Parallel()

	newTestContainers := func() []Container {
		return []Container{
			{ID: "c1", Name: "web", Service: "web", Project: "app", Image: "nginx:latest"},
			{ID: "c2", Name: "db", Service: "database", Project: "app", Image: "postgres:15"},
			{ID: "c3", Name: "cache", Service: "cache", Project: "app", Image: "redis:7"},
		}
	}

	t.Run("typing after / narrows rows to matching containers", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			filterText string
			wantNames  []string
		}{
			{name: "match by name", filterText: "web", wantNames: []string{"web"}},
			{name: "match by service", filterText: "data", wantNames: []string{"db"}},
			{name: "match by image", filterText: "REDIS", wantNames: []string{"cache"}},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
				m.containers = newTestContainers()
				m.rows = newRows(m.containers)

				m = typeFilter(m, tc.filterText)

				assert.True(t, m.filtering)
				assert.Equal(t, tc.filterText, m.filter)
				assert.Equal(t, tc.wantNames, containerRowNames(m.rows))
			})
		}
	})

	t.Run("backspace re-expands the filtered rows", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.containers = newTestContainers()
		m.rows = newRows(m.containers)

		m = typeFilter(m, "web")
		require.Equal(t, []string{"web"}, containerRowNames(m.rows))

		got, _ := m.updateKeys(newTestKeyMsg("backspace"))
		got, _ = got.(Model).updateKeys(newTestKeyMsg("backspace"))
		got, _ = got.(Model).updateKeys(newTestKeyMsg("backspace"))
		m = got.(Model)

		assert.Equal(t, "", m.filter)
		assert.ElementsMatch(t, []string{"web", "db", "cache"}, containerRowNames(m.rows))
	})

	t.Run("esc while filtering clears the filter and restores all rows", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.containers = newTestContainers()
		m.rows = newRows(m.containers)

		m = typeFilter(m, "web")
		got, _ := m.updateKeys(newTestKeyMsg("esc"))
		m = got.(Model)

		assert.False(t, m.filtering)
		assert.Equal(t, "", m.filter)
		assert.ElementsMatch(t, []string{"web", "db", "cache"}, containerRowNames(m.rows))
	})

	t.Run("enter applies the filter and a later esc clears it", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.containers = newTestContainers()
		m.rows = newRows(m.containers)

		m = typeFilter(m, "web")
		got, _ = m.updateKeys(newTestKeyMsg("enter"))
		m = got.(Model)

		assert.False(t, m.filtering)
		assert.Equal(t, "web", m.filter)
		titlesRow := strings.Split(m.View(), "\n")[1]
		assert.Contains(t, titlesRow, "/web")

		got, _ = m.updateKeys(newTestKeyMsg("esc"))
		m = got.(Model)

		assert.Equal(t, "", m.filter)
		assert.ElementsMatch(t, []string{"web", "db", "cache"}, containerRowNames(m.rows))
	})

	t.Run("footer shows the filter prompt while filtering", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.containers = newTestContainers()
		m.rows = newRows(m.containers)

		m = typeFilter(m, "w")

		assert.Contains(t, m.View(), "filter: /w")
	})

	t.Run("containersMsg respects an active filter", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.filter = "web"

		got, _ := m.Update(containersMsg{containers: newTestContainers()})
		m = got.(Model)

		assert.Equal(t, []string{"web"}, containerRowNames(m.rows))
	})

	t.Run("resource tabs", func(t *testing.T) {
		t.Parallel()

		type tabCase struct {
			name       string
			tab        tabID
			setup      func(m Model) Model
			filterText string
			wantLabel  string
			wantKind   deleteKind
			filtered   func(m Model) int
			label      func(m Model, cursor int) string
			updateKeys func(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
		}

		tests := []tabCase{
			{
				name: "volumes",
				tab:  tabVolumes,
				setup: func(m Model) Model {
					m.volumes = []Volume{{Name: "data"}, {Name: "cache"}, {Name: "logs"}}
					return m
				},
				filterText: "ca",
				wantLabel:  "cache",
				wantKind:   deleteVolume,
				filtered:   func(m Model) int { return len(m.filteredVolumes()) },
				label:      func(m Model, cursor int) string { return m.filteredVolumes()[cursor].Name },
				updateKeys: func(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
					got, cmd := m.updateVolumeKeys(msg)
					return got.(Model), cmd
				},
			},
			{
				name: "networks",
				tab:  tabNetworks,
				setup: func(m Model) Model {
					m.networks = []Network{{Name: "app-net"}, {Name: "cache-net"}, {Name: "db-net"}}
					return m
				},
				filterText: "cache",
				wantLabel:  "cache-net",
				wantKind:   deleteNetwork,
				filtered:   func(m Model) int { return len(m.filteredNetworks()) },
				label:      func(m Model, cursor int) string { return m.filteredNetworks()[cursor].Name },
				updateKeys: func(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
					got, cmd := m.updateNetworkKeys(msg)
					return got.(Model), cmd
				},
			},
			{
				name: "images",
				tab:  tabImages,
				setup: func(m Model) Model {
					m.images = []Image{{RepoTag: "nginx:latest"}, {RepoTag: "redis:7"}, {RepoTag: "postgres:15"}}
					return m
				},
				filterText: "redis",
				wantLabel:  "redis:7",
				wantKind:   deleteImage,
				filtered:   func(m Model) int { return len(m.filteredImages()) },
				label:      func(m Model, cursor int) string { return m.filteredImages()[cursor].RepoTag },
				updateKeys: func(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
					got, cmd := m.updateImageKeys(msg)
					return got.(Model), cmd
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				t.Run("filter narrows the list to the matching resource", func(t *testing.T) {
					t.Parallel()

					m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
					m.tab = tc.tab
					m = tc.setup(m)

					m = typeFilter(m, tc.filterText)

					require.Equal(t, 1, tc.filtered(m))
					assert.Equal(t, tc.wantLabel, tc.label(m, 0))
				})

				t.Run("d with an active filter arms confirm for the visible resource, not the unfiltered index", func(t *testing.T) {
					t.Parallel()

					m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
					m.tab = tc.tab
					m = tc.setup(m)
					m.filter = tc.filterText

					got, cmd := tc.updateKeys(m, newTestKeyMsg("d"))

					require.NotNil(t, got.confirm)
					assert.Equal(t, tc.wantKind, got.confirm.kind)
					assert.Equal(t, tc.wantLabel, got.confirm.label)
					assert.Nil(t, cmd)
				})

				t.Run("esc clears the filter and restores the list", func(t *testing.T) {
					t.Parallel()

					m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
					m.tab = tc.tab
					m = tc.setup(m)
					m.filter = tc.filterText

					got, _ := tc.updateKeys(m, newTestKeyMsg("esc"))

					assert.Equal(t, "", got.filter)
					assert.Equal(t, 3, tc.filtered(got))
				})
			})
		}
	})
}
