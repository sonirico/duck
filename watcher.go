package main

import (
	"context"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

type containersMsg struct {
	containers []Container
}

type watcherErrMsg struct {
	err error
}

// Watcher feeds the store from an initial snapshot plus the Docker events
// stream, so the UI never polls the full container list.
type Watcher struct {
	cli   client.APIClient
	store *Store
	send  func(msg any)
}

func NewWatcher(cli client.APIClient, store *Store, send func(msg any)) *Watcher {
	return &Watcher{cli: cli, store: store, send: send}
}

func (w *Watcher) RunLoop(ctx context.Context) {
	if err := w.snapshot(ctx); err != nil {
		w.send(watcherErrMsg{err: err})
		return
	}
	w.send(containersMsg{containers: w.store.List()})

	stream := w.cli.Events(ctx, client.EventsListOptions{
		Filters: make(client.Filters).Add("type", "container"),
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
			w.send(containersMsg{containers: w.store.List()})
		}
	}
}

func (w *Watcher) snapshot(ctx context.Context) error {
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

func (w *Watcher) apply(ctx context.Context, msg events.Message) {
	id := msg.Actor.ID
	if msg.Action == events.ActionDestroy {
		w.store.Delete(id)
		return
	}
	res, err := w.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("id", id),
	})
	if err != nil || len(res.Items) == 0 {
		w.store.Delete(id)
		return
	}
	w.store.Upsert(newContainerFromSummary(res.Items[0]))
}

func newContainerFromSummary(s container.Summary) Container {
	name := ""
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	}
	return Container{
		ID:      s.ID,
		Name:    name,
		Image:   s.Image,
		State:   string(s.State),
		Status:  s.Status,
		Project: s.Labels["com.docker.compose.project"],
		Service: s.Labels["com.docker.compose.service"],
	}
}
