package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moby/moby/api/types/container"
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
	containerOpErr   error
	containerInspect client.ContainerInspectResult
	imageInspect     client.ImageInspectResult
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

func newTestModel(streamer logRetargeter, resources resourceClient) Model {
	return NewModel(streamer, TmuxInfo{}, resources)
}

func newTestKeyMsg(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
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

	t.Run("right wraps from networks back to containers", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabNetworks
		m.rows = []row{{kind: rowContainer, key: "id:c1"}}

		got, cmd := m.updateKeys(newTestKeyMsg("right"))

		gotModel := got.(Model)
		assert.Equal(t, tabContainers, gotModel.tab)
		assert.Nil(t, gotModel.confirm)
		require.NotNil(t, cmd)
	})

	t.Run("left wraps from containers back to networks", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		m.focus = focusList
		m.tab = tabContainers

		got, cmd := m.updateKeys(newTestKeyMsg("left"))

		gotModel := got.(Model)
		assert.Equal(t, tabNetworks, gotModel.tab)
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

	t.Run("keys are a no-op over a stack row", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowStack, key: "stack:app"}}
		m.cursor = 0

		for _, key := range []string{"s", "S", "r", "K", "p"} {
			got, cmd := m.updateKeys(newTestKeyMsg(key))
			assert.Nil(t, cmd)
			m = got.(Model)
		}
		assert.Empty(t, resources.calls)
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

	t.Run("d over a stack row arms nothing", func(t *testing.T) {
		t.Parallel()

		resources := newTestResourceClient(nil)
		m := newTestModel(newTestLogRetargeter(), resources)
		m.tab = tabContainers
		m.focus = focusList
		m.rows = []row{{kind: rowStack, key: "stack:app"}}
		m.cursor = 0

		got, cmd := m.updateKeys(newTestKeyMsg("d"))

		assert.Nil(t, got.(Model).confirm)
		assert.Nil(t, cmd)
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
}

func TestResourceFooter(t *testing.T) {
	t.Parallel()

	const base = " j/k move  left/right tab  d delete  q quit"

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
			wantDim:    []string{styleDim.Render("2:volumes"), styleDim.Render("3:networks")},
		},
		{
			name:       "volumes tab active",
			tab:        tabVolumes,
			wantActive: styleSelected.Render(" 2:volumes "),
			wantDim:    []string{styleDim.Render("1:containers"), styleDim.Render("3:networks")},
		},
		{
			name:       "networks tab active",
			tab:        tabNetworks,
			wantActive: styleSelected.Render(" 3:networks "),
			wantDim:    []string{styleDim.Render("1:containers"), styleDim.Render("2:volumes")},
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
			{name: "volumes", tab: tabVolumes, wantLeft: " volumes", wantRight: " detail"},
			{name: "networks", tab: tabNetworks, wantLeft: " networks", wantRight: " detail"},
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

	t.Run("containers tab shows the default footer", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
		got, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
		m = got.(Model)
		m.tab = tabContainers

		gotView := m.View()

		require.Contains(t, gotView, "j/k move  tab focus  e exec  s/S stop/start  r restart  p pause  K kill  d delete  left/right tab  q quit")
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

type testResourceClientWithInspectErr struct {
	containerInspectErr error
	imageInspectErr     error
	calls               []testResourceCall
}

func newTestResourceClientWithContainerInspectErr(err error) *testResourceClientWithInspectErr {
	return &testResourceClientWithInspectErr{containerInspectErr: err}
}

func newTestResourceClientWithImageInspectErr(err error) *testResourceClientWithInspectErr {
	return &testResourceClientWithInspectErr{imageInspectErr: err}
}

func (c *testResourceClientWithInspectErr) VolumeRemove(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error) {
	return client.VolumeRemoveResult{}, nil
}

func (c *testResourceClientWithInspectErr) NetworkRemove(ctx context.Context, networkID string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error) {
	return client.NetworkRemoveResult{}, nil
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
