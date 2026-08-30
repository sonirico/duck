package main

import (
	"context"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

type containersMsg struct {
	containers []Container
}

type watcherErrMsg struct {
	err error
}

// watcherClient is the subset of the docker client the Watcher depends on.
type watcherClient interface {
	ContainerList(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error)
	VolumeList(ctx context.Context, opts client.VolumeListOptions) (client.VolumeListResult, error)
	NetworkList(ctx context.Context, opts client.NetworkListOptions) (client.NetworkListResult, error)
	Events(ctx context.Context, opts client.EventsListOptions) client.EventsResult
}

// Watcher feeds the store from an initial snapshot plus the Docker events
// stream, so the UI never polls the full container list.
type Watcher struct {
	cli      watcherClient
	store    *Store[Container]
	volumes  *Store[Volume]
	networks *Store[Network]
	send     func(msg any)
}

func NewWatcher(cli watcherClient, store *Store[Container], volumes *Store[Volume], networks *Store[Network], send func(msg any)) *Watcher {
	return &Watcher{cli: cli, store: store, volumes: volumes, networks: networks, send: send}
}

func (w *Watcher) RunLoop(ctx context.Context) {
	if err := w.snapshot(ctx); err != nil {
		w.send(watcherErrMsg{err: err})
		return
	}
	w.send(containersMsg{containers: w.store.List()})
	w.send(volumesMsg{volumes: w.volumes.List()})
	w.send(networksMsg{networks: w.networks.List()})

	stream := w.cli.Events(ctx, client.EventsListOptions{
		Filters: make(client.Filters).Add("type", "container", "volume", "network"),
	})
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-stream.Err:
			if ctx.Err() == nil {
				w.send(watcherErrMsg{err: err})
			}
			return
		case msg := <-stream.Messages:
			w.apply(ctx, msg)
		}
	}
}

func (w *Watcher) snapshot(ctx context.Context) error {
	if err := w.refreshContainers(ctx); err != nil {
		return err
	}
	if err := w.refreshVolumes(ctx); err != nil {
		return err
	}
	if err := w.refreshNetworks(ctx); err != nil {
		return err
	}
	return nil
}

func (w *Watcher) refreshContainers(ctx context.Context) error {
	res, err := w.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}
	cs := make([]Container, 0, len(res.Items))
	for _, s := range res.Items {
		cs = append(cs, newContainerFromSummary(s))
	}
	w.store.SetAll(cs)
	return nil
}

func (w *Watcher) refreshVolumes(ctx context.Context) error {
	res, err := w.cli.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return err
	}
	vs := make([]Volume, 0, len(res.Items))
	for _, v := range res.Items {
		vs = append(vs, newVolumeFromSummary(v))
	}
	w.volumes.SetAll(vs)
	return nil
}

func (w *Watcher) refreshNetworks(ctx context.Context) error {
	res, err := w.cli.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return err
	}
	ns := make([]Network, 0, len(res.Items))
	for _, n := range res.Items {
		ns = append(ns, newNetworkFromSummary(n))
	}
	w.networks.SetAll(ns)
	return nil
}

func (w *Watcher) apply(ctx context.Context, msg events.Message) {
	switch msg.Type {
	case events.ContainerEventType:
		id := msg.Actor.ID
		if msg.Action == events.ActionDestroy {
			w.store.Delete(id)
			w.send(containersMsg{containers: w.store.List()})
			return
		}
		res, err := w.cli.ContainerList(ctx, client.ContainerListOptions{
			All:     true,
			Filters: make(client.Filters).Add("id", id),
		})
		if err != nil || len(res.Items) == 0 {
			w.store.Delete(id)
			w.send(containersMsg{containers: w.store.List()})
			return
		}
		w.store.Upsert(newContainerFromSummary(res.Items[0]))
		w.send(containersMsg{containers: w.store.List()})
	case events.VolumeEventType:
		if err := w.refreshVolumes(ctx); err != nil {
			return
		}
		w.send(volumesMsg{volumes: w.volumes.List()})
	case events.NetworkEventType:
		if err := w.refreshNetworks(ctx); err != nil {
			return
		}
		w.send(networksMsg{networks: w.networks.List()})
	}
}
