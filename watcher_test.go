package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testWatcherClient struct {
	containerList func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error)
	volumeList    func(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error)
	networkList   func(ctx context.Context, opts client.NetworkListOptions) (client.NetworkListResult, error)
	events        func(ctx context.Context, opts client.EventsListOptions) client.EventsResult
}

func newTestWatcherClient(
	containerList func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error),
	volumeList func(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error),
	networkList func(ctx context.Context, opts client.NetworkListOptions) (client.NetworkListResult, error),
	events func(ctx context.Context, opts client.EventsListOptions) client.EventsResult,
) *testWatcherClient {
	return &testWatcherClient{containerList: containerList, volumeList: volumeList, networkList: networkList, events: events}
}

func (c *testWatcherClient) ContainerList(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
	return c.containerList(ctx, opts)
}

func (c *testWatcherClient) VolumeList(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error) {
	return c.volumeList(ctx, opts)
}

func (c *testWatcherClient) NetworkList(ctx context.Context, opts client.NetworkListOptions) (client.NetworkListResult, error) {
	return c.networkList(ctx, opts)
}

func (c *testWatcherClient) Events(ctx context.Context, opts client.EventsListOptions) client.EventsResult {
	return c.events(ctx, opts)
}

func containerListOK(items ...container.Summary) func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
	return func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: items}, nil
	}
}

func containerListErr(err error) func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
	return func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{}, err
	}
}

func volumeListOK(items ...volume.Volume) func(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error) {
	return func(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error) {
		return client.VolumeListResult{Items: items}, nil
	}
}

func volumeListErr(err error) func(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error) {
	return func(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error) {
		return client.VolumeListResult{}, err
	}
}

func networkListOK(items ...network.Summary) func(ctx context.Context, opts client.NetworkListOptions) (client.NetworkListResult, error) {
	return func(ctx context.Context, opts client.NetworkListOptions) (client.NetworkListResult, error) {
		return client.NetworkListResult{Items: items}, nil
	}
}

func networkListErr(err error) func(ctx context.Context, opts client.NetworkListOptions) (client.NetworkListResult, error) {
	return func(ctx context.Context, opts client.NetworkListOptions) (client.NetworkListResult, error) {
		return client.NetworkListResult{}, err
	}
}

func newTestWatcher(cli watcherClient) (w *Watcher, msgs chan any, store *Store[Container], volumes *Store[Volume], networks *Store[Network]) {
	msgs = make(chan any, testMsgBufSize)
	store = NewStore[Container](func(c Container) string { return c.ID }, func(a, b Container) bool { return a.Name < b.Name })
	volumes = NewStore[Volume](func(v Volume) string { return v.Name }, func(a, b Volume) bool { return a.Name < b.Name })
	networks = NewStore[Network](func(n Network) string { return n.Name }, func(a, b Network) bool { return a.Name < b.Name })
	w = NewWatcher(cli, store, volumes, networks, func(msg any) { msgs <- msg })
	return
}

func recvContainers(t *testing.T, msgs chan any) containersMsg {
	t.Helper()
	return recvMsg[containersMsg](t, msgs)
}

func recvVolumes(t *testing.T, msgs chan any) volumesMsg {
	t.Helper()
	return recvMsg[volumesMsg](t, msgs)
}

func recvNetworks(t *testing.T, msgs chan any) networksMsg {
	t.Helper()
	return recvMsg[networksMsg](t, msgs)
}

func recvWatcherErr(t *testing.T, msgs chan any) watcherErrMsg {
	t.Helper()
	return recvMsg[watcherErrMsg](t, msgs)
}

func TestWatcherSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("success populates both stores", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(
			containerListOK(container.Summary{ID: "c1", Names: []string{"/web"}}),
			volumeListOK(volume.Volume{Name: "data"}),
			networkListOK(),
			nil,
		)
		w, _, store, volumes, _ := newTestWatcher(cli)

		err := w.snapshot(context.Background())

		require.NoError(t, err)
		assert.Equal(t, []Container{{ID: "c1", Name: "web", Volumes: []string{}}}, store.List())
		assert.Equal(t, []Volume{{Name: "data"}}, volumes.List())
	})

	t.Run("container list error", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		cli := newTestWatcherClient(containerListErr(wantErr), nil, nil, nil)
		w, _, store, volumes, _ := newTestWatcher(cli)

		err := w.snapshot(context.Background())

		assert.Equal(t, wantErr, err)
		assert.Empty(t, store.List())
		assert.Empty(t, volumes.List())
	})

	t.Run("volume list error", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		cli := newTestWatcherClient(containerListOK(container.Summary{ID: "c1"}), volumeListErr(wantErr), nil, nil)
		w, _, store, volumes, _ := newTestWatcher(cli)

		err := w.snapshot(context.Background())

		assert.Equal(t, wantErr, err)
		assert.Len(t, store.List(), 1)
		assert.Empty(t, volumes.List())
	})

	t.Run("network list error", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		cli := newTestWatcherClient(containerListOK(), volumeListOK(), networkListErr(wantErr), nil)
		w, _, _, _, networks := newTestWatcher(cli)

		err := w.snapshot(context.Background())

		assert.Equal(t, wantErr, err)
		assert.Empty(t, networks.List())
	})

	t.Run("success populates networks store", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(
			containerListOK(),
			volumeListOK(),
			networkListOK(network.Summary{Network: network.Network{Name: "bridge"}}),
			nil,
		)
		w, _, _, _, networks := newTestWatcher(cli)

		err := w.snapshot(context.Background())

		require.NoError(t, err)
		assert.Equal(t, []Network{{Name: "bridge"}}, networks.List())
	})
}

func TestWatcherApply(t *testing.T) {
	t.Parallel()

	t.Run("container destroy removes from store", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(
			func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
				t.Fatal("ContainerList should not be called on destroy")
				return client.ContainerListResult{}, nil
			},
			nil,
			nil,
			nil,
		)
		w, msgs, store, _, _ := newTestWatcher(cli)
		store.SetAll([]Container{{ID: "c1", Name: "web"}})
		msg := events.Message{Type: events.ContainerEventType, Action: events.ActionDestroy, Actor: events.Actor{ID: "c1"}}

		w.apply(context.Background(), msg)

		assert.Empty(t, store.List())
		got := recvContainers(t, msgs)
		assert.Empty(t, got.containers)
	})

	t.Run("container update upserts from lookup", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(
			containerListOK(container.Summary{ID: "c1", Names: []string{"/web"}}),
			nil,
			nil,
			nil,
		)
		w, msgs, store, _, _ := newTestWatcher(cli)
		msg := events.Message{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "c1"}}

		w.apply(context.Background(), msg)

		assert.Equal(t, []Container{{ID: "c1", Name: "web", Volumes: []string{}}}, store.List())
		got := recvContainers(t, msgs)
		assert.Equal(t, store.List(), got.containers)
	})

	t.Run("container update lookup error deletes from store", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(containerListErr(errors.New("boom")), nil, nil, nil)
		w, msgs, store, _, _ := newTestWatcher(cli)
		store.SetAll([]Container{{ID: "c1", Name: "web"}})
		msg := events.Message{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "c1"}}

		w.apply(context.Background(), msg)

		assert.Empty(t, store.List())
		recvContainers(t, msgs)
	})

	t.Run("container update lookup empty deletes from store", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(containerListOK(), nil, nil, nil)
		w, msgs, store, _, _ := newTestWatcher(cli)
		store.SetAll([]Container{{ID: "c1", Name: "web"}})
		msg := events.Message{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "c1"}}

		w.apply(context.Background(), msg)

		assert.Empty(t, store.List())
		recvContainers(t, msgs)
	})

	t.Run("volume event success replaces volumes", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(nil, volumeListOK(volume.Volume{Name: "data"}), nil, nil)
		w, msgs, _, volumes, _ := newTestWatcher(cli)
		msg := events.Message{Type: events.VolumeEventType}

		w.apply(context.Background(), msg)

		assert.Equal(t, []Volume{{Name: "data"}}, volumes.List())
		got := recvVolumes(t, msgs)
		assert.Equal(t, volumes.List(), got.volumes)
	})

	t.Run("volume event error sends no message", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(nil, volumeListErr(errors.New("boom")), nil, nil)
		w, msgs, _, volumes, _ := newTestWatcher(cli)
		msg := events.Message{Type: events.VolumeEventType}

		w.apply(context.Background(), msg)

		assert.Empty(t, volumes.List())
		select {
		case got := <-msgs:
			t.Fatalf("expected no message, got %#v", got)
		default:
		}
	})

	t.Run("network event success replaces networks", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(nil, nil, networkListOK(network.Summary{Network: network.Network{Name: "bridge"}}), nil)
		w, msgs, _, _, networks := newTestWatcher(cli)
		msg := events.Message{Type: events.NetworkEventType}

		w.apply(context.Background(), msg)

		assert.Equal(t, []Network{{Name: "bridge"}}, networks.List())
		got := recvNetworks(t, msgs)
		assert.Equal(t, networks.List(), got.networks)
	})

	t.Run("network event error sends no message", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(nil, nil, networkListErr(errors.New("boom")), nil)
		w, msgs, _, _, networks := newTestWatcher(cli)
		msg := events.Message{Type: events.NetworkEventType}

		w.apply(context.Background(), msg)

		assert.Empty(t, networks.List())
		select {
		case got := <-msgs:
			t.Fatalf("expected no message, got %#v", got)
		default:
		}
	})
}

func TestWatcherRunLoop(t *testing.T) {
	t.Parallel()

	t.Run("snapshot error sends err and stops without opening the event stream", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		cli := newTestWatcherClient(
			containerListErr(wantErr),
			nil,
			nil,
			func(ctx context.Context, opts client.EventsListOptions) client.EventsResult {
				t.Fatal("Events should not be called after a snapshot error")
				return client.EventsResult{}
			},
		)
		w, msgs, _, _, _ := newTestWatcher(cli)

		done := make(chan struct{})
		go func() {
			defer close(done)
			w.RunLoop(context.Background())
		}()

		got := recvWatcherErr(t, msgs)
		assert.Equal(t, wantErr, got.err)

		select {
		case <-done:
		case <-time.After(testGuardTimeout):
			t.Fatal("timed out waiting for RunLoop to return")
		}
	})

	t.Run("dispatches snapshot then applies stream events until ctx is cancelled", func(t *testing.T) {
		t.Parallel()

		messages := make(chan events.Message, 1)
		errs := make(chan error, 1)
		cli := newTestWatcherClient(
			containerListOK(container.Summary{ID: "c1", Names: []string{"/web"}}),
			volumeListOK(volume.Volume{Name: "data"}),
			networkListOK(),
			func(ctx context.Context, opts client.EventsListOptions) client.EventsResult {
				return client.EventsResult{Messages: messages, Err: errs}
			},
		)
		w, msgs, store, _, _ := newTestWatcher(cli)
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			defer close(done)
			w.RunLoop(ctx)
		}()

		initial := recvContainers(t, msgs)
		assert.Equal(t, []Container{{ID: "c1", Name: "web", Volumes: []string{}}}, initial.containers)
		recvVolumes(t, msgs)
		recvNetworks(t, msgs)

		messages <- events.Message{Type: events.ContainerEventType, Action: events.ActionDestroy, Actor: events.Actor{ID: "c1"}}
		updated := recvContainers(t, msgs)
		assert.Empty(t, updated.containers)
		assert.Empty(t, store.List())

		cancel()
		select {
		case <-done:
		case <-time.After(testGuardTimeout):
			t.Fatal("timed out waiting for RunLoop to return after ctx cancellation")
		}
	})

	t.Run("stream error sends err and stops", func(t *testing.T) {
		t.Parallel()

		messages := make(chan events.Message, 1)
		errs := make(chan error, 1)
		wantErr := errors.New("stream broke")
		cli := newTestWatcherClient(
			containerListOK(),
			volumeListOK(),
			networkListOK(),
			func(ctx context.Context, opts client.EventsListOptions) client.EventsResult {
				return client.EventsResult{Messages: messages, Err: errs}
			},
		)
		w, msgs, _, _, _ := newTestWatcher(cli)

		done := make(chan struct{})
		go func() {
			defer close(done)
			w.RunLoop(context.Background())
		}()

		recvContainers(t, msgs)
		recvVolumes(t, msgs)
		recvNetworks(t, msgs)

		errs <- wantErr
		got := recvWatcherErr(t, msgs)
		assert.Equal(t, wantErr, got.err)

		select {
		case <-done:
		case <-time.After(testGuardTimeout):
			t.Fatal("timed out waiting for RunLoop to return after stream error")
		}
	})
}
