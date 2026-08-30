package main

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testLogRetargeter struct {
	targets []LogTarget
}

func newTestLogRetargeter() *testLogRetargeter { return &testLogRetargeter{} }

func (s *testLogRetargeter) SetTargets(ts []LogTarget) { s.targets = ts }

type testResourceClient struct {
	volumeRemove  func(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error)
	networkRemove func(ctx context.Context, networkID string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error)
}

func newTestResourceClient(
	volumeRemove func(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error),
) *testResourceClient {
	return &testResourceClient{volumeRemove: volumeRemove}
}

func (c *testResourceClient) VolumeRemove(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error) {
	return c.volumeRemove(ctx, volumeID, options)
}

func (c *testResourceClient) NetworkRemove(ctx context.Context, networkID string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error) {
	return c.networkRemove(ctx, networkID, options)
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
		m.confirm = &pendingDelete{kind: "volume", id: "data"}
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
		m.confirm = &pendingDelete{kind: "volume", id: "data"}

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
		m.confirm = &pendingDelete{kind: "network", id: "app-net"}

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
			assert.Equal(t, &pendingDelete{kind: "volume", id: "data", label: "data"}, gotModel.confirm)
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
			m.confirm = &pendingDelete{kind: "volume", id: "other", label: "other"}

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
			m.confirm = &pendingDelete{kind: "volume", id: "data", label: "data"}

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
			m.confirm = &pendingDelete{kind: "volume", id: "data", label: "data"}

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
				m.confirm = &pendingDelete{kind: "volume", id: "data", label: "data"}

				got, cmd := m.updateVolumeKeys(newTestKeyMsg(key))

				assert.Nil(t, got.(Model).confirm)
				assert.Nil(t, cmd)
			})
		}
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
			assert.Equal(t, &pendingDelete{kind: "network", id: "n1", label: "app-net"}, gotModel.confirm)
			assert.Nil(t, cmd)
		})

		t.Run("does nothing when a confirm is already pending", func(t *testing.T) {
			t.Parallel()

			m := newTestModel(newTestLogRetargeter(), newTestResourceClient(nil))
			m.networks = []Network{{ID: "n1", Name: "app-net", Driver: "bridge"}}
			m.netCursor = 0
			m.confirm = &pendingDelete{kind: "network", id: "other", label: "other"}

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
				m.confirm = &pendingDelete{kind: "network", id: "n1", label: "app-net"}

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
		m.confirm = &pendingDelete{kind: "network", id: "n1", label: "app-net"}

		got, cmd := m.updateNetworkKeys(newTestKeyMsg("y"))

		assert.Nil(t, got.(Model).confirm)
		require.NotNil(t, cmd)
		assert.Nil(t, cmd())
		assert.Equal(t, "n1", gotID)
	})
}
