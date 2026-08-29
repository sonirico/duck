package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testGuardTimeout = 2 * time.Second

type testLogsClient struct {
	inspect func(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	logs    func(ctx context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error)
}

func newTestLogsClient(
	inspect func(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error),
	logs func(ctx context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error),
) *testLogsClient {
	return &testLogsClient{inspect: inspect, logs: logs}
}

func (c *testLogsClient) ContainerInspect(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return c.inspect(ctx, id, opts)
}

func (c *testLogsClient) ContainerLogs(ctx context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	return c.logs(ctx, id, opts)
}

// testBlockingReadCloser blocks on Read until its context is cancelled, then
// reports the cancellation on cancelled and returns ctx.Err().
type testBlockingReadCloser struct {
	ctx       context.Context
	cancelled chan struct{}
}

func newTestBlockingReadCloser(ctx context.Context) *testBlockingReadCloser {
	return &testBlockingReadCloser{ctx: ctx, cancelled: make(chan struct{})}
}

func (r *testBlockingReadCloser) Read(_ []byte) (int, error) {
	<-r.ctx.Done()
	close(r.cancelled)
	return 0, r.ctx.Err()
}

func (r *testBlockingReadCloser) Close() error {
	return nil
}

// newTestMultiplexedFrame builds one StdCopy-framed chunk: an 8-byte header
// (stream type + big-endian uint32 payload length) followed by the payload.
func newTestMultiplexedFrame(st stdcopy.StdType, payload []byte) []byte {
	frame := make([]byte, 8+len(payload))
	frame[0] = byte(st)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	return frame
}

func inspectOK(tty bool) func(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return func(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		return client.ContainerInspectResult{
			Container: container.InspectResponse{
				Config: &container.Config{Tty: tty},
			},
		}, nil
	}
}

func recvLine(t *testing.T, msgs chan any) logLineMsg {
	t.Helper()
	select {
	case msg := <-msgs:
		line, ok := msg.(logLineMsg)
		require.True(t, ok, "expected logLineMsg, got %T", msg)
		return line
	case <-time.After(testGuardTimeout):
		t.Fatal("timed out waiting for logLineMsg")
		return logLineMsg{}
	}
}

func TestStreamer(t *testing.T) {
	t.Run("demux multiplexed stream", func(t *testing.T) {
		frames := append(
			newTestMultiplexedFrame(stdcopy.Stdout, []byte("hello stdout\n")),
			newTestMultiplexedFrame(stdcopy.Stderr, []byte("hello stderr\n"))...,
		)
		cli := newTestLogsClient(inspectOK(false), func(ctx context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			return io.NopCloser(bytes.NewReader(frames)), nil
		})
		msgs := make(chan any, 8)
		s := NewStreamer(cli, func(msg any) { msgs <- msg })
		target := LogTarget{ID: "c1", Name: "web"}

		s.stream(context.Background(), target)

		first := recvLine(t, msgs)
		second := recvLine(t, msgs)
		assert.Equal(t, logLineMsg{source: "web", line: "hello stdout"}, first)
		assert.Equal(t, logLineMsg{source: "web", line: "hello stderr"}, second)
	})

	t.Run("tty stream passthrough", func(t *testing.T) {
		raw := []byte("line one\nline two\n")
		cli := newTestLogsClient(inspectOK(true), func(ctx context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			return io.NopCloser(bytes.NewReader(raw)), nil
		})
		msgs := make(chan any, 8)
		s := NewStreamer(cli, func(msg any) { msgs <- msg })
		target := LogTarget{ID: "c1", Name: "web"}

		s.stream(context.Background(), target)

		first := recvLine(t, msgs)
		second := recvLine(t, msgs)
		assert.Equal(t, logLineMsg{source: "web", line: "line one"}, first)
		assert.Equal(t, logLineMsg{source: "web", line: "line two"}, second)
	})

	t.Run("error line when logs fails", func(t *testing.T) {
		cli := newTestLogsClient(inspectOK(false), func(ctx context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			return nil, errors.New("boom")
		})
		msgs := make(chan any, 8)
		s := NewStreamer(cli, func(msg any) { msgs <- msg })
		target := LogTarget{ID: "c1", Name: "web"}

		s.stream(context.Background(), target)

		line := recvLine(t, msgs)
		assert.Equal(t, "web", line.source)
		assert.Contains(t, line.line, "duck: logs:")
	})

	t.Run("retarget cancels previous stream", func(t *testing.T) {
		readers := make(chan *testBlockingReadCloser, 2)
		cli := newTestLogsClient(inspectOK(false), func(ctx context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
			r := newTestBlockingReadCloser(ctx)
			readers <- r
			return r, nil
		})
		msgs := make(chan any, 8)
		s := NewStreamer(cli, func(msg any) { msgs <- msg })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go s.RunLoop(ctx)

		first := LogTarget{ID: "c1", Name: "first"}
		s.SetTargets([]LogTarget{first})

		reset := recvReset(t, msgs)
		assert.Equal(t, []LogTarget{first}, reset.targets)

		var firstReader *testBlockingReadCloser
		select {
		case firstReader = <-readers:
		case <-time.After(testGuardTimeout):
			t.Fatal("timed out waiting for first stream's reader")
		}

		second := LogTarget{ID: "c2", Name: "second"}
		s.SetTargets([]LogTarget{second})

		reset = recvReset(t, msgs)
		assert.Equal(t, []LogTarget{second}, reset.targets)

		select {
		case <-firstReader.cancelled:
		case <-time.After(testGuardTimeout):
			t.Fatal("timed out waiting for first stream to be cancelled")
		}
	})
}

func recvReset(t *testing.T, msgs chan any) logResetMsg {
	t.Helper()
	select {
	case msg := <-msgs:
		reset, ok := msg.(logResetMsg)
		require.True(t, ok, "expected logResetMsg, got %T", msg)
		return reset
	case <-time.After(testGuardTimeout):
		t.Fatal("timed out waiting for logResetMsg")
		return logResetMsg{}
	}
}
