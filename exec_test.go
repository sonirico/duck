package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewExecArgv(t *testing.T) {
	const containerID = "abcdef012345678"

	type testCase struct {
		name       string
		dockerHost string
		tmux       TmuxInfo
		want       []string
	}

	tests := []testCase{
		{
			name: "no tmux",
			tmux: TmuxInfo{Present: false},
			want: nil,
		},
		{
			name: "tmux 2.9 below minimum",
			tmux: TmuxInfo{Present: true, Major: 2, Minor: 9},
			want: nil,
		},
		{
			name: "tmux 3.0 uses split-window",
			tmux: TmuxInfo{Present: true, Major: 3, Minor: 0, Pane: "%1"},
			want: []string{
				"tmux", "split-window", "-h", "-t", "%1", "-P", "-F", "#{pane_id}",
				"--",
				"docker", "exec", "-it", containerID, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh",
			},
		},
		{
			name: "tmux 3.2 uses display-popup",
			tmux: TmuxInfo{Present: true, Major: 3, Minor: 2, Pane: "%1"},
			want: []string{
				"tmux", "display-popup", "-E", "-w", "80%", "-h", "80%", "-T", " exec: " + shortID(containerID) + " ",
				"--",
				"docker", "exec", "-it", containerID, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh",
			},
		},
		{
			name:       "tmux 3.2 with docker host injects -e before --",
			dockerHost: "ssh://x",
			tmux:       TmuxInfo{Present: true, Major: 3, Minor: 2, Pane: "%1"},
			want: []string{
				"tmux", "display-popup", "-E", "-w", "80%", "-h", "80%", "-T", " exec: " + shortID(containerID) + " ",
				"-e", "DOCKER_HOST=ssh://x",
				"--",
				"docker", "exec", "-it", containerID, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := newExecArgv(containerID, tc.dockerHost, tc.tmux)

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewTmuxInfo(t *testing.T) {
	type testCase struct {
		name    string
		env     func(string) string
		look    func(string) (string, error)
		version func() (string, error)
		want    TmuxInfo
	}

	noVersion := func() (string, error) { return "", errors.New("not called") }

	tests := []testCase{
		{
			name:    "no $TMUX",
			env:     func(string) string { return "" },
			look:    func(string) (string, error) { return "/usr/bin/tmux", nil },
			version: noVersion,
			want:    TmuxInfo{},
		},
		{
			name: "$TMUX set but binary missing",
			env: func(k string) string {
				if k == "TMUX" {
					return "/tmp/tmux-1000/default,123,0"
				}
				return ""
			},
			look:    func(string) (string, error) { return "", errors.New("not found") },
			version: noVersion,
			want:    TmuxInfo{},
		},
		{
			name: "everything present parses version",
			env: func(k string) string {
				switch k {
				case "TMUX":
					return "/tmp/tmux-1000/default,123,0"
				case "TMUX_PANE":
					return "%1"
				}
				return ""
			},
			look:    func(string) (string, error) { return "/usr/bin/tmux", nil },
			version: func() (string, error) { return "tmux 3.7c", nil },
			want:    TmuxInfo{Present: true, Major: 3, Minor: 7, Pane: "%1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewTmuxInfo(tc.env, tc.look, tc.version)

			assert.Equal(t, tc.want, got)
		})
	}
}
