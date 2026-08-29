package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/moby/moby/client"
)

func main() {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "duck: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := cli.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "duck: close client: %v\n", err)
		}
	}()

	store := NewStore()
	var p *tea.Program
	send := func(msg any) { p.Send(msg) }
	streamer := NewStreamer(cli, send)
	tmux := NewTmuxInfo(os.Getenv, exec.LookPath, func() (string, error) {
		out, err := exec.Command("tmux", "-V").Output()
		return string(out), err
	})
	p = tea.NewProgram(NewModel(streamer, tmux), tea.WithAltScreen(), tea.WithMouseCellMotion())
	watcher := NewWatcher(cli, store, send)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watcher.RunLoop(ctx)
	go streamer.RunLoop(ctx)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "duck: %v\n", err)
		os.Exit(1)
	}
}
