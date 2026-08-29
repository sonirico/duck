package main

import (
	"bufio"
	"context"
	"io"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

type LogTarget struct {
	ID   string
	Name string
}

type logResetMsg struct {
	targets []LogTarget
}

type logLineMsg struct {
	source string
	line   string
}

// Streamer follows the logs of the selected containers and pumps each line
// into the UI. Retargeting cancels the streams of the previous selection.
type Streamer struct {
	cli     client.APIClient
	send    func(msg any)
	targets chan []LogTarget
}

func NewStreamer(cli client.APIClient, send func(msg any)) *Streamer {
	return &Streamer{cli: cli, send: send, targets: make(chan []LogTarget, 1)}
}

// SetTargets replaces the followed set. Non-blocking: a pending, not yet
// picked up selection is discarded in favor of the new one.
func (s *Streamer) SetTargets(ts []LogTarget) {
	for {
		select {
		case s.targets <- ts:
			return
		case <-s.targets:
		}
	}
}

func (s *Streamer) RunLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case ts := <-s.targets:
		for {
			next, ok := s.streamUntilRetarget(ctx, ts)
			if !ok {
				return
			}
			ts = next
		}
	}
}

// streamUntilRetarget follows ts until a new selection arrives (returned with
// ok=true) or the context is cancelled (ok=false).
func (s *Streamer) streamUntilRetarget(ctx context.Context, ts []LogTarget) (next []LogTarget, ok bool) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.send(logResetMsg{targets: ts})
	for _, t := range ts {
		go s.stream(streamCtx, t)
	}
	select {
	case <-ctx.Done():
		return nil, false
	case next := <-s.targets:
		return next, true
	}
}

func (s *Streamer) stream(ctx context.Context, t LogTarget) {
	insp, err := s.cli.ContainerInspect(ctx, t.ID, client.ContainerInspectOptions{})
	if err != nil {
		s.sendLine(ctx, t, "duck: inspect: "+err.Error())
		return
	}
	rc, err := s.cli.ContainerLogs(ctx, t.ID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "100",
	})
	if err != nil {
		s.sendLine(ctx, t, "duck: logs: "+err.Error())
		return
	}
	defer func() {
		if err := rc.Close(); err != nil && ctx.Err() == nil {
			s.sendLine(ctx, t, "duck: close log stream: "+err.Error())
		}
	}()

	var r io.Reader = rc
	tty := insp.Container.Config != nil && insp.Container.Config.Tty
	if !tty {
		pr, pw := io.Pipe()
		go func() {
			_, copyErr := stdcopy.StdCopy(pw, pw, rc)
			if err := pw.CloseWithError(copyErr); err != nil {
				return
			}
		}()
		r = pr
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		s.sendLine(ctx, t, sc.Text())
	}
	if err := sc.Err(); err != nil {
		s.sendLine(ctx, t, "duck: read logs: "+err.Error())
	}
}

func (s *Streamer) sendLine(ctx context.Context, t LogTarget, line string) {
	if ctx.Err() != nil {
		return
	}
	s.send(logLineMsg{source: t.Name, line: line})
}
