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
const testMsgBufSize = 8

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

func newTestStreamer(cli logsClient) (*Streamer, chan any) {
	msgs := make(chan any, testMsgBufSize)
	return NewStreamer(cli, func(msg any) { msgs <- msg }), msgs
}

func newTestRetargetHarness(t *testing.T) (*Streamer, chan any, chan *testBlockingReadCloser) {
	t.Helper()
	readers := make(chan *testBlockingReadCloser, 2)
	cli := newTestLogsClient(inspectOK(false), func(ctx context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
		r := newTestBlockingReadCloser(ctx)
		readers <- r
		return r, nil
	})
	s, msgs := newTestStreamer(cli)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.RunLoop(ctx)
	return s, msgs, readers
}

func recvMsg[T any](t *testing.T, msgs chan any) T {
	t.Helper()
	select {
	case msg := <-msgs:
		v, ok := msg.(T)
		require.True(t, ok, "expected %T, got %T", v, msg)
		return v
	case <-time.After(testGuardTimeout):
		var zero T
		t.Fatalf("timed out waiting for %T", zero)
		return zero
	}
}

func recvReader(t *testing.T, readers chan *testBlockingReadCloser) *testBlockingReadCloser {
	t.Helper()
	select {
	case r := <-readers:
		return r
	case <-time.After(testGuardTimeout):
		t.Fatal("timed out waiting for stream's reader")
		return nil
	}
}

func recvLine(t *testing.T, msgs chan any) logLineMsg {
	t.Helper()
	return recvMsg[logLineMsg](t, msgs)
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
		s, msgs := newTestStreamer(cli)
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
		s, msgs := newTestStreamer(cli)
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
		s, msgs := newTestStreamer(cli)
		target := LogTarget{ID: "c1", Name: "web"}

		s.stream(context.Background(), target)

		line := recvLine(t, msgs)
		assert.Equal(t, "web", line.source)
		assert.Contains(t, line.line, "duck: logs:")
	})

	t.Run("retarget emits reset message per selection", func(t *testing.T) {
		s, msgs, readers := newTestRetargetHarness(t)

		first := LogTarget{ID: "c1", Name: "first"}
		s.SetTargets([]LogTarget{first})

		reset := recvReset(t, msgs)
		assert.Equal(t, []LogTarget{first}, reset.targets)

		recvReader(t, readers)

		second := LogTarget{ID: "c2", Name: "second"}
		s.SetTargets([]LogTarget{second})

		reset = recvReset(t, msgs)
		assert.Equal(t, []LogTarget{second}, reset.targets)
	})

	t.Run("retarget cancels previous stream", func(t *testing.T) {
		s, msgs, readers := newTestRetargetHarness(t)

		first := LogTarget{ID: "c1", Name: "first"}
		s.SetTargets([]LogTarget{first})
		recvReset(t, msgs)

		firstReader := recvReader(t, readers)

		second := LogTarget{ID: "c2", Name: "second"}
		s.SetTargets([]LogTarget{second})
		recvReset(t, msgs)

		select {
		case <-firstReader.cancelled:
		case <-time.After(testGuardTimeout):
			t.Fatal("timed out waiting for first stream to be cancelled")
		}
	})
}

func recvReset(t *testing.T, msgs chan any) logResetMsg {
	t.Helper()
	return recvMsg[logResetMsg](t, msgs)
}
