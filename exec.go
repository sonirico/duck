package main

import (
	"fmt"
	"strings"
)

// TmuxInfo describes the tmux environment duck is running inside, if any.
type TmuxInfo struct {
	Present bool
	Major   int
	Minor   int
	Pane    string
}

// NewTmuxInfo detects tmux by checking for the $TMUX env var, the tmux
// binary on PATH, and parsing its version output (e.g. "tmux 3.7c").
func NewTmuxInfo(env func(string) string, look func(string) (string, error), version func() (string, error)) TmuxInfo {
	if env("TMUX") == "" {
		return TmuxInfo{}
	}
	if _, err := look("tmux"); err != nil {
		return TmuxInfo{}
	}
	out, err := version()
	if err != nil {
		return TmuxInfo{}
	}

	fields := strings.Fields(out)
	if len(fields) < 2 {
		return TmuxInfo{}
	}
	var major, minor int
	fmt.Sscanf(fields[1], "%d.%d", &major, &minor)

	return TmuxInfo{Present: true, Major: major, Minor: minor, Pane: env("TMUX_PANE")}
}

// newExecArgv builds the argv for launching an interactive shell in
// containerID, using a tmux popup (>=3.2) or split (>=3.0) when available,
// or nil to signal the caller should fall back to tea.ExecProcess.
func newExecArgv(containerID, dockerHost string, t TmuxInfo) []string {
	inner := []string{"docker", "exec", "-it", containerID, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"}

	if !t.Present {
		return nil
	}

	if t.Major > 3 || (t.Major == 3 && t.Minor >= 2) {
		argv := []string{"tmux", "display-popup", "-E", "-w", "80%", "-h", "80%", "-T", " exec: " + shortID(containerID) + " "}
		if dockerHost != "" {
			argv = append(argv, "-e", "DOCKER_HOST="+dockerHost)
		}
		argv = append(argv, "--")
		return append(argv, inner...)
	}

	if t.Major > 3 || (t.Major == 3 && t.Minor >= 0) {
		argv := []string{"tmux", "split-window", "-h", "-t", t.Pane, "-P", "-F", "#{pane_id}"}
		if dockerHost != "" {
			argv = append(argv, "-e", "DOCKER_HOST="+dockerHost)
		}
		argv = append(argv, "--")
		return append(argv, inner...)
	}

	return nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
