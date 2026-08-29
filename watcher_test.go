package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testWatcherClient struct {
	containerList func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error)
	volumeList    func(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error)
	events        func(ctx context.Context, opts client.EventsListOptions) client.EventsResult
}

func newTestWatcherClient(
	containerList func(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error),
	volumeList func(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error),
	events func(ctx context.Context, opts client.EventsListOptions) client.EventsResult,
) *testWatcherClient {
	return &testWatcherClient{containerList: containerList, volumeList: volumeList, events: events}
}

func (c *testWatcherClient) ContainerList(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
	return c.containerList(ctx, opts)
}

func (c *testWatcherClient) VolumeList(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error) {
	return c.volumeList(ctx, opts)
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

func newTestWatcher(cli watcherClient) (w *Watcher, msgs chan any, store *Store[Container], volumes *Store[Volume]) {
	msgs = make(chan any, testMsgBufSize)
	store = NewStore[Container](func(c Container) string { return c.ID }, func(a, b Container) bool { return a.Name < b.Name })
	volumes = NewStore[Volume](func(v Volume) string { return v.Name }, func(a, b Volume) bool { return a.Name < b.Name })
	w = NewWatcher(cli, store, volumes, func(msg any) { msgs <- msg })
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

func recvWatcherErr(t *testing.T, msgs chan any) watcherErrMsg {
	t.Helper()
	return recvMsg[watcherErrMsg](t, msgs)
}

func TestNewContainerFromSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   container.Summary
		want Container
	}{
		{
			name: "with name and volume mount",
			in: container.Summary{
				ID:     "c1",
				Names:  []string{"/web"},
				Image:  "nginx",
				State:  "running",
				Status: "Up 2 minutes",
				Labels: map[string]string{
					"com.docker.compose.project": "proj",
					"com.docker.compose.service": "svc",
				},
				Mounts: []container.MountPoint{
					{Type: mount.TypeVolume, Name: "data"},
					{Type: mount.TypeBind, Name: "ignored"},
				},
			},
			want: Container{
				ID:      "c1",
				Name:    "web",
				Image:   "nginx",
				State:   "running",
				Status:  "Up 2 minutes",
				Project: "proj",
				Service: "svc",
				Volumes: []string{"data"},
			},
		},
		{
			name: "without name",
			in:   container.Summary{ID: "c2"},
			want: Container{ID: "c2", Name: "", Volumes: []string{}},
		},
		{
			name: "without mounts",
			in:   container.Summary{ID: "c3", Names: []string{"/db"}},
			want: Container{ID: "c3", Name: "db", Volumes: []string{}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newContainerFromSummary(tc.in)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestWatcherSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("success populates both stores", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(
			containerListOK(container.Summary{ID: "c1", Names: []string{"/web"}}),
			volumeListOK(volume.Volume{Name: "data"}),
			nil,
		)
		w, _, store, volumes := newTestWatcher(cli)

		err := w.snapshot(context.Background())

		require.NoError(t, err)
		assert.Equal(t, []Container{{ID: "c1", Name: "web", Volumes: []string{}}}, store.List())
		assert.Equal(t, []Volume{{Name: "data"}}, volumes.List())
	})

	t.Run("container list error", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		cli := newTestWatcherClient(containerListErr(wantErr), nil, nil)
		w, _, store, volumes := newTestWatcher(cli)

		err := w.snapshot(context.Background())

		assert.Equal(t, wantErr, err)
		assert.Empty(t, store.List())
		assert.Empty(t, volumes.List())
	})

	t.Run("volume list error", func(t *testing.T) {
		t.Parallel()

		wantErr := errors.New("boom")
		cli := newTestWatcherClient(containerListOK(container.Summary{ID: "c1"}), volumeListErr(wantErr), nil)
		w, _, store, volumes := newTestWatcher(cli)

		err := w.snapshot(context.Background())

		assert.Equal(t, wantErr, err)
		assert.Len(t, store.List(), 1)
		assert.Empty(t, volumes.List())
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
		)
		w, msgs, store, _ := newTestWatcher(cli)
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
		)
		w, msgs, store, _ := newTestWatcher(cli)
		msg := events.Message{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "c1"}}

		w.apply(context.Background(), msg)

		assert.Equal(t, []Container{{ID: "c1", Name: "web", Volumes: []string{}}}, store.List())
		got := recvContainers(t, msgs)
		assert.Equal(t, store.List(), got.containers)
	})

	t.Run("container update lookup error deletes from store", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(containerListErr(errors.New("boom")), nil, nil)
		w, msgs, store, _ := newTestWatcher(cli)
		store.SetAll([]Container{{ID: "c1", Name: "web"}})
		msg := events.Message{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "c1"}}

		w.apply(context.Background(), msg)

		assert.Empty(t, store.List())
		recvContainers(t, msgs)
	})

	t.Run("container update lookup empty deletes from store", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(containerListOK(), nil, nil)
		w, msgs, store, _ := newTestWatcher(cli)
		store.SetAll([]Container{{ID: "c1", Name: "web"}})
		msg := events.Message{Type: events.ContainerEventType, Action: events.ActionStart, Actor: events.Actor{ID: "c1"}}

		w.apply(context.Background(), msg)

		assert.Empty(t, store.List())
		recvContainers(t, msgs)
	})

	t.Run("volume event success replaces volumes", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(nil, volumeListOK(volume.Volume{Name: "data"}), nil)
		w, msgs, _, volumes := newTestWatcher(cli)
		msg := events.Message{Type: events.VolumeEventType}

		w.apply(context.Background(), msg)

		assert.Equal(t, []Volume{{Name: "data"}}, volumes.List())
		got := recvVolumes(t, msgs)
		assert.Equal(t, volumes.List(), got.volumes)
	})

	t.Run("volume event error sends no message", func(t *testing.T) {
		t.Parallel()

		cli := newTestWatcherClient(nil, volumeListErr(errors.New("boom")), nil)
		w, msgs, _, volumes := newTestWatcher(cli)
		msg := events.Message{Type: events.VolumeEventType}

		w.apply(context.Background(), msg)

		assert.Empty(t, volumes.List())
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
			func(ctx context.Context, opts client.EventsListOptions) client.EventsResult {
				t.Fatal("Events should not be called after a snapshot error")
				return client.EventsResult{}
			},
		)
		w, msgs, _, _ := newTestWatcher(cli)

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
			func(ctx context.Context, opts client.EventsListOptions) client.EventsResult {
				return client.EventsResult{Messages: messages, Err: errs}
			},
		)
		w, msgs, store, _ := newTestWatcher(cli)
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			defer close(done)
			w.RunLoop(ctx)
		}()

		initial := recvContainers(t, msgs)
		assert.Equal(t, []Container{{ID: "c1", Name: "web", Volumes: []string{}}}, initial.containers)
		recvVolumes(t, msgs)

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
			func(ctx context.Context, opts client.EventsListOptions) client.EventsResult {
				return client.EventsResult{Messages: messages, Err: errs}
			},
		)
		w, msgs, _, _ := newTestWatcher(cli)

		done := make(chan struct{})
		go func() {
			defer close(done)
			w.RunLoop(context.Background())
		}()

		recvContainers(t, msgs)
		recvVolumes(t, msgs)

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
