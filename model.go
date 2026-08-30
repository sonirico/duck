package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os/exec"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const maxLogLines = 2000

var (
	styleHeader     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleBorder     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8"))
	styleBorderOn   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("6"))
	styleTitle      = lipgloss.NewStyle().Bold(true)
	styleStack      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	styleSelected   = lipgloss.NewStyle().Reverse(true)
	styleDotRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleDotStopped = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleDim        = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleErr        = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	styleSection    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))

	sourcePalette = []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
	}
)

type rowKind int

const (
	rowStack rowKind = iota
	rowContainer
)

type row struct {
	kind      rowKind
	key       string
	project   string
	container Container
}

type subtabID int

const (
	subLogs subtabID = iota
	subInfo
	subEnv
	subTop
	subStats
	subInspect
)

func subtabsFor(kind rowKind) []subtabID {
	switch kind {
	case rowContainer:
		return []subtabID{subLogs, subInfo, subEnv, subTop, subStats, subInspect}
	case rowStack:
		return []subtabID{subLogs, subInfo}
	}
	return nil
}

func renderSubtabBar(sel subtabID, kinds []subtabID) string {
	labels := map[subtabID]string{
		subLogs:    "logs",
		subInfo:    "info",
		subEnv:     "env",
		subTop:     "top",
		subStats:   "stats",
		subInspect: "inspect",
	}
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		label := labels[k]
		if k == sel {
			parts[i] = styleSelected.Render(" " + label + " ")
		} else {
			parts[i] = styleDim.Render(label)
		}
	}
	return strings.Join(parts, "  ")
}

type focusArea int

const (
	focusList focusArea = iota
	focusLogs
)

type tabID int

const (
	tabContainers tabID = iota
	tabVolumes
	tabNetworks
	tabImages
)

type deleteKind int

const (
	deleteVolume deleteKind = iota
	deleteNetwork
	deleteContainer
	deleteStack
	pruneContainers
	pruneImages
	pruneVolumes
	pruneNetworks
	deleteImage
)

type pendingDelete struct {
	kind  deleteKind
	id    string
	ids   []string
	label string
}

type containerOp int

const (
	opStart containerOp = iota
	opStop
	opRestart
	opKill
	opPause
	opUnpause
)

type logLine struct {
	source string
	line   string
}

type composeMsg struct {
	yaml string
}

type detailExtra struct {
	env     []string
	titles  []string
	procs   [][]string
	mounts  []string
	inspect string
}

type detailExtraMsg struct {
	id    string
	extra detailExtra
}

type logRetargeter interface {
	SetTargets(ts []LogTarget)
}

// resourceClient is the subset of the docker client the Model depends on to
// mutate resources (e.g. deleting a volume).
type resourceClient interface {
	VolumeRemove(ctx context.Context, volumeID string, options client.VolumeRemoveOptions) (client.VolumeRemoveResult, error)
	NetworkRemove(ctx context.Context, networkID string, options client.NetworkRemoveOptions) (client.NetworkRemoveResult, error)
	ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error)
	ContainerKill(ctx context.Context, containerID string, options client.ContainerKillOptions) (client.ContainerKillResult, error)
	ContainerPause(ctx context.Context, containerID string, options client.ContainerPauseOptions) (client.ContainerPauseResult, error)
	ContainerUnpause(ctx context.Context, containerID string, options client.ContainerUnpauseOptions) (client.ContainerUnpauseResult, error)
	ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	ContainerTop(ctx context.Context, containerID string, options client.ContainerTopOptions) (client.ContainerTopResult, error)
	ImageInspect(ctx context.Context, imageID string, inspectOpts ...client.ImageInspectOption) (client.ImageInspectResult, error)
	ImageRemove(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error)
	ContainerPrune(ctx context.Context, opts client.ContainerPruneOptions) (client.ContainerPruneResult, error)
	ImagePrune(ctx context.Context, opts client.ImagePruneOptions) (client.ImagePruneResult, error)
	VolumePrune(ctx context.Context, options client.VolumePruneOptions) (client.VolumePruneResult, error)
	NetworkPrune(ctx context.Context, opts client.NetworkPruneOptions) (client.NetworkPruneResult, error)
}

type Model struct {
	streamer  logRetargeter
	tmux      TmuxInfo
	resources resourceClient

	tab tabID

	containers []Container
	rows       []row
	cursor     int
	focus      focusArea

	filtering bool
	filter    string

	volumes   []Volume
	volCursor int

	networks  []Network
	netCursor int

	images    []Image
	imgCursor int

	confirm *pendingDelete

	logs     []logLine
	logTitle string
	viewport viewport.Model
	follow   bool

	compose   string
	composeVP viewport.Model

	subtab       subtabID
	subVP        viewport.Model
	stats        map[string]ContainerStat
	extras       map[string]detailExtra
	statsTicking bool

	width  int
	height int
	err    error
}

func NewModel(streamer logRetargeter, tmux TmuxInfo, resources resourceClient) Model {
	return Model{streamer: streamer, tmux: tmux, resources: resources, follow: true}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = m.logsWidth()
		m.viewport.Height = m.panesHeight()
		m.viewport.SetContent(m.renderLogs())
		m.composeVP.Width = m.logsWidth()
		m.composeVP.Height = m.panesHeight()
		m.subVP.Width = m.logsWidth()
		m.subVP.Height = m.panesHeight()
		m.syncSubVP()
		return m, nil

	case containersMsg:
		prevKey := m.selectedKey()
		m.containers = msg.containers
		m.rows = newRows(m.filteredContainers())
		m.cursor = indexOfKey(m.rows, prevKey)
		m.syncSubVP()
		if prevKey == "" || m.selectedKey() != prevKey {
			return m, m.retarget()
		}
		return m, nil

	case volumesMsg:
		m.volumes = msg.volumes
		if m.volCursor >= len(m.volumes) {
			m.volCursor = len(m.volumes) - 1
		}
		if m.volCursor < 0 {
			m.volCursor = 0
		}
		return m, nil

	case networksMsg:
		m.networks = msg.networks
		if m.netCursor >= len(m.networks) {
			m.netCursor = len(m.networks) - 1
		}
		if m.netCursor < 0 {
			m.netCursor = 0
		}
		return m, nil

	case imagesMsg:
		m.images = msg.images
		if m.imgCursor >= len(m.images) {
			m.imgCursor = len(m.images) - 1
		}
		if m.imgCursor < 0 {
			m.imgCursor = 0
		}
		return m, nil

	case watcherErrMsg:
		m.err = msg.err
		return m, nil

	case statsMsg:
		if m.stats == nil {
			m.stats = make(map[string]ContainerStat)
		}
		for _, s := range msg.stats {
			m.stats[s.ID] = s
		}
		m.syncSubVP()
		return m, nil

	case statsTickMsg:
		if m.cursor >= 0 && m.cursor < len(m.rows) && m.tab == tabContainers && m.subtab == subStats {
			r := m.rows[m.cursor]
			if r.kind == rowContainer && r.container.State == "running" {
				return m, tea.Batch(m.statsCmd([]string{r.container.ID}), statsTickCmd())
			}
		}
		m.statsTicking = false
		return m, nil

	case detailExtraMsg:
		if m.extras == nil {
			m.extras = make(map[string]detailExtra)
		}
		m.extras[msg.id] = msg.extra
		m.syncSubVP()
		return m, nil

	case composeMsg:
		m.compose = msg.yaml
		m.composeVP.SetContent(msg.yaml)
		return m, nil

	case logResetMsg:
		m.logs = nil
		names := make([]string, 0, len(msg.targets))
		for _, t := range msg.targets {
			names = append(names, t.Name)
		}
		m.logTitle = strings.Join(names, ", ")
		m.viewport.SetContent("")
		return m, nil

	case logLineMsg:
		m.logs = append(m.logs, logLine{source: msg.source, line: msg.line})
		if len(m.logs) > maxLogLines {
			m.logs = m.logs[len(m.logs)-maxLogLines:]
		}
		m.viewport.SetContent(m.renderLogs())
		if m.follow {
			m.viewport.GotoBottom()
		}
		return m, nil

	case tea.MouseMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.follow = m.viewport.AtBottom()
		return m, cmd

	case tea.KeyMsg:
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m Model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		if m.focus == focusList {
			m.focus = focusLogs
		} else {
			m.focus = focusList
		}
		return m, nil
	}

	if m.compose != "" {
		if msg.String() == "esc" {
			m.compose = ""
			return m, nil
		}
		var cmd tea.Cmd
		m.composeVP, cmd = m.composeVP.Update(msg)
		return m, cmd
	}

	if m.filtering {
		var cmd tea.Cmd
		switch msg.String() {
		case "esc":
			m.filter = ""
			m.filtering = false
			m.cursor = 0
			m.volCursor = 0
			m.netCursor = 0
			m.imgCursor = 0
			m.rows = newRows(m.filteredContainers())
			if m.tab == tabContainers {
				cmd = m.retarget()
			}
		case "enter":
			m.filtering = false
		case "backspace":
			if m.filter != "" {
				r := []rune(m.filter)
				m.filter = string(r[:len(r)-1])
				m.cursor = 0
				m.volCursor = 0
				m.netCursor = 0
				m.imgCursor = 0
				m.rows = newRows(m.filteredContainers())
				if m.tab == tabContainers {
					cmd = m.retarget()
				}
			}
		default:
			if len(msg.Runes) == 1 {
				m.filter += string(msg.Runes)
				m.cursor = 0
				m.volCursor = 0
				m.netCursor = 0
				m.imgCursor = 0
				m.rows = newRows(m.filteredContainers())
				if m.tab == tabContainers {
					cmd = m.retarget()
				}
			}
		}
		return m, cmd
	}

	if (msg.String() == "]" || msg.String() == "[") && m.tab == tabContainers && m.confirm == nil {
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			kinds := subtabsFor(m.rows[m.cursor].kind)
			idx := 0
			for i, k := range kinds {
				if k == m.subtab {
					idx = i
					break
				}
			}
			if msg.String() == "]" {
				idx = (idx + 1) % len(kinds)
			} else {
				idx = (idx - 1 + len(kinds)) % len(kinds)
			}
			m.subtab = kinds[idx]
			m.syncSubVP()
			return m, tea.Batch(m.lazyExtraCmd(), m.statsTickStartCmd())
		}
		return m, nil
	}

	if m.focus == focusList {
		switch msg.String() {
		case "1":
			m.tab = tabContainers
			m.confirm = nil
			return m, m.retarget()
		case "2":
			m.tab = tabVolumes
			m.confirm = nil
			return m, nil
		case "3":
			m.tab = tabNetworks
			m.confirm = nil
			return m, nil
		case "4":
			m.tab = tabImages
			m.confirm = nil
			return m, nil
		case "right":
			m.tab = (m.tab + 1) % 4
			m.confirm = nil
			if m.tab == tabContainers {
				return m, m.retarget()
			}
			return m, nil
		case "left":
			m.tab = (m.tab + 3) % 4
			m.confirm = nil
			if m.tab == tabContainers {
				return m, m.retarget()
			}
			return m, nil
		}
	}

	if msg.String() == "/" && m.focus == focusList && m.confirm == nil {
		m.filtering = true
		return m, nil
	}

	if m.tab == tabVolumes {
		return m.updateVolumeKeys(msg)
	}

	if m.tab == tabNetworks {
		return m.updateNetworkKeys(msg)
	}

	if m.tab == tabImages {
		return m.updateImageKeys(msg)
	}

	if m.focus == focusLogs {
		if m.tab == tabContainers && m.subtab != subLogs {
			switch msg.String() {
			case "G":
				m.subVP.GotoBottom()
				return m, nil
			case "g":
				m.subVP.GotoTop()
				return m, nil
			}
			var cmd tea.Cmd
			m.subVP, cmd = m.subVP.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "G":
			m.follow = true
			m.viewport.GotoBottom()
			return m, nil
		case "g":
			m.follow = false
			m.viewport.GotoTop()
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.follow = m.viewport.AtBottom()
		return m, cmd
	}

	switch msg.String() {
	case "j", "down":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.clampSubtab()
			m.syncSubVP()
			return m, tea.Batch(m.retarget(), m.lazyExtraCmd(), m.statsTickStartCmd())
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.clampSubtab()
			m.syncSubVP()
			return m, tea.Batch(m.retarget(), m.lazyExtraCmd(), m.statsTickStartCmd())
		}
	case "g":
		if len(m.rows) > 0 && m.cursor != 0 {
			m.cursor = 0
			m.clampSubtab()
			m.syncSubVP()
			return m, tea.Batch(m.retarget(), m.lazyExtraCmd(), m.statsTickStartCmd())
		}
	case "G":
		if len(m.rows) > 0 && m.cursor != len(m.rows)-1 {
			m.cursor = len(m.rows) - 1
			m.clampSubtab()
			m.syncSubVP()
			return m, tea.Batch(m.retarget(), m.lazyExtraCmd(), m.statsTickStartCmd())
		}
	case "enter":
		if m.confirm == nil && m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			m.subtab = subInfo
			m.syncSubVP()
			m.subVP.GotoTop()
			var ids []string
			switch r.kind {
			case rowContainer:
				if r.container.State == "running" {
					ids = []string{r.container.ID}
					return m, tea.Batch(m.statsCmd(ids), m.detailExtraCmd(r.container.ID, true))
				}
			case rowStack:
				for _, c := range m.containers {
					if c.Project == r.project && c.State == "running" {
						ids = append(ids, c.ID)
					}
				}
			}
			return m, m.statsCmd(ids)
		}
	case "e":
		if m.confirm == nil && m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowContainer {
			return m, m.execCmd(m.rows[m.cursor].container.ID)
		}
	case "s":
		if m.confirm == nil && m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			switch r.kind {
			case rowContainer:
				return m, m.containerOpCmd(opStop, []string{r.container.ID})
			case rowStack:
				return m, m.containerOpCmd(opStop, m.stackContainerIDs(r.project))
			}
		}
	case "S":
		if m.confirm == nil && m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			switch r.kind {
			case rowContainer:
				return m, m.containerOpCmd(opStart, []string{r.container.ID})
			case rowStack:
				return m, m.containerOpCmd(opStart, m.stackContainerIDs(r.project))
			}
		}
	case "r":
		if m.confirm == nil && m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			switch r.kind {
			case rowContainer:
				return m, m.containerOpCmd(opRestart, []string{r.container.ID})
			case rowStack:
				return m, m.containerOpCmd(opRestart, m.stackContainerIDs(r.project))
			}
		}
	case "K":
		if m.confirm == nil && m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			switch r.kind {
			case rowContainer:
				return m, m.containerOpCmd(opKill, []string{r.container.ID})
			case rowStack:
				return m, m.containerOpCmd(opKill, m.stackContainerIDs(r.project))
			}
		}
	case "p":
		if m.confirm == nil && m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowContainer {
			container := m.rows[m.cursor].container
			op := opPause
			if container.State == "paused" {
				op = opUnpause
			}
			return m, m.containerOpCmd(op, []string{container.ID})
		}
	case "d":
		if m.confirm == nil && m.cursor >= 0 && m.cursor < len(m.rows) {
			r := m.rows[m.cursor]
			switch r.kind {
			case rowContainer:
				m.confirm = &pendingDelete{kind: deleteContainer, id: r.container.ID, label: r.container.Name}
			case rowStack:
				name := r.project
				if name == "" {
					name = "standalone"
				}
				m.confirm = &pendingDelete{kind: deleteStack, ids: m.stackContainerIDs(r.project), label: name}
			}
		}
	case "P":
		if m.confirm == nil {
			m.confirm = &pendingDelete{kind: pruneContainers, label: "stopped containers"}
		}
	case "y":
		if m.confirm != nil {
			return m.applyConfirm()
		}
		return m, m.composeCmd()
	case "n", "esc":
		if m.confirm != nil {
			m.confirm = nil
		} else if msg.String() == "esc" && m.subtab != subLogs {
			m.subtab = subLogs
			m.syncSubVP()
		} else if m.filter != "" {
			m.filter = ""
			m.cursor = 0
			m.volCursor = 0
			m.netCursor = 0
			m.imgCursor = 0
			m.rows = newRows(m.filteredContainers())
			m.clampSubtab()
			m.syncSubVP()
			return m, tea.Batch(m.retarget(), m.lazyExtraCmd())
		}
	}
	return m, nil
}

func (m Model) updateVolumeKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down", "k", "up", "g", "G":
		m.volCursor = moveListCursor(m.volCursor, len(m.filteredVolumes()), msg.String())
		return m, nil
	case "d":
		vols := m.filteredVolumes()
		if m.confirm != nil || m.volCursor < 0 || m.volCursor >= len(vols) {
			return m, nil
		}
		v := vols[m.volCursor]
		if volumeUsedBy(m.volumes, m.containers)[v.Name] == 0 {
			m.confirm = &pendingDelete{kind: deleteVolume, id: v.Name, label: v.Name}
		}
		return m, nil
	case "P":
		if m.confirm == nil {
			m.confirm = &pendingDelete{kind: pruneVolumes, label: "unused volumes"}
		}
		return m, nil
	case "y":
		return m.applyConfirm()
	case "n", "esc":
		if m.confirm != nil {
			m.confirm = nil
		} else if m.filter != "" {
			m.filter = ""
			m.volCursor = 0
			m.netCursor = 0
			m.imgCursor = 0
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateNetworkKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down", "k", "up", "g", "G":
		m.netCursor = moveListCursor(m.netCursor, len(m.filteredNetworks()), msg.String())
		return m, nil
	case "d":
		nets := m.filteredNetworks()
		if m.confirm != nil || m.netCursor < 0 || m.netCursor >= len(nets) {
			return m, nil
		}
		n := nets[m.netCursor]
		if networkUsedBy(m.networks, m.containers)[n.Name] == 0 && !isBuiltinNetwork(n.Name) {
			m.confirm = &pendingDelete{kind: deleteNetwork, id: n.ID, label: n.Name}
		}
		return m, nil
	case "P":
		if m.confirm == nil {
			m.confirm = &pendingDelete{kind: pruneNetworks, label: "unused networks"}
		}
		return m, nil
	case "y":
		return m.applyConfirm()
	case "n", "esc":
		if m.confirm != nil {
			m.confirm = nil
		} else if m.filter != "" {
			m.filter = ""
			m.volCursor = 0
			m.netCursor = 0
			m.imgCursor = 0
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateImageKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down", "k", "up", "g", "G":
		m.imgCursor = moveListCursor(m.imgCursor, len(m.filteredImages()), msg.String())
		return m, nil
	case "d":
		imgs := m.filteredImages()
		if m.confirm != nil || m.imgCursor < 0 || m.imgCursor >= len(imgs) {
			return m, nil
		}
		img := imgs[m.imgCursor]
		if imageUsedBy(m.images, m.containers)[img.ID] == 0 {
			m.confirm = &pendingDelete{kind: deleteImage, id: img.ID, label: img.RepoTag}
		}
		return m, nil
	case "P":
		if m.confirm == nil {
			m.confirm = &pendingDelete{kind: pruneImages, label: "dangling images"}
		}
		return m, nil
	case "y":
		return m.applyConfirm()
	case "n", "esc":
		if m.confirm != nil {
			m.confirm = nil
		} else if m.filter != "" {
			m.filter = ""
			m.volCursor = 0
			m.netCursor = 0
			m.imgCursor = 0
		}
		return m, nil
	}
	return m, nil
}

func moveListCursor(cursor, length int, key string) int {
	switch key {
	case "j", "down":
		if cursor < length-1 {
			return cursor + 1
		}
	case "k", "up":
		if cursor > 0 {
			return cursor - 1
		}
	case "g":
		if length > 0 {
			return 0
		}
	case "G":
		if length > 0 {
			return length - 1
		}
	}
	return cursor
}

func (m Model) applyConfirm() (Model, tea.Cmd) {
	if m.confirm == nil {
		return m, nil
	}
	kind := m.confirm.kind
	id := m.confirm.id
	ids := m.confirm.ids
	resources := m.resources
	m.confirm = nil
	return m, func() tea.Msg {
		var err error
		switch kind {
		case deleteVolume:
			_, err = resources.VolumeRemove(context.Background(), id, client.VolumeRemoveOptions{})
		case deleteNetwork:
			_, err = resources.NetworkRemove(context.Background(), id, client.NetworkRemoveOptions{})
		case deleteContainer:
			_, err = resources.ContainerRemove(context.Background(), id, client.ContainerRemoveOptions{})
		case deleteImage:
			_, err = resources.ImageRemove(context.Background(), id, client.ImageRemoveOptions{})
		case deleteStack:
			for _, sid := range ids {
				if _, rmErr := resources.ContainerRemove(context.Background(), sid, client.ContainerRemoveOptions{}); rmErr != nil {
					err = rmErr
				}
			}
		case pruneContainers:
			_, err = resources.ContainerPrune(context.Background(), client.ContainerPruneOptions{})
		case pruneImages:
			_, err = resources.ImagePrune(context.Background(), client.ImagePruneOptions{})
		case pruneVolumes:
			_, err = resources.VolumePrune(context.Background(), client.VolumePruneOptions{})
		case pruneNetworks:
			_, err = resources.NetworkPrune(context.Background(), client.NetworkPruneOptions{})
		}
		if err != nil {
			return watcherErrMsg{err: err}
		}
		return nil
	}
}

func (m Model) containerOpCmd(op containerOp, ids []string) tea.Cmd {
	resources := m.resources
	return func() tea.Msg {
		var lastErr error
		for _, id := range ids {
			var err error
			switch op {
			case opStart:
				_, err = resources.ContainerStart(context.Background(), id, client.ContainerStartOptions{})
			case opStop:
				_, err = resources.ContainerStop(context.Background(), id, client.ContainerStopOptions{})
			case opRestart:
				_, err = resources.ContainerRestart(context.Background(), id, client.ContainerRestartOptions{})
			case opKill:
				_, err = resources.ContainerKill(context.Background(), id, client.ContainerKillOptions{})
			case opPause:
				_, err = resources.ContainerPause(context.Background(), id, client.ContainerPauseOptions{})
			case opUnpause:
				_, err = resources.ContainerUnpause(context.Background(), id, client.ContainerUnpauseOptions{})
			}
			if err != nil {
				lastErr = err
			}
		}
		if lastErr != nil {
			return watcherErrMsg{err: lastErr}
		}
		return nil
	}
}

func (m Model) stackContainerIDs(project string) []string {
	var ids []string
	for _, c := range m.containers {
		if c.Project == project {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

func (m Model) execCmd(id string) tea.Cmd {
	const dockerHost = ""
	if argv := newExecArgv(id, dockerHost, m.tmux); argv != nil {
		cmd := exec.Command(argv[0], argv[1:]...)
		return func() tea.Msg {
			if err := cmd.Run(); err != nil {
				return watcherErrMsg{err: err}
			}
			return nil
		}
	}
	return tea.ExecProcess(exec.Command("docker", "exec", "-it", id, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"), func(err error) tea.Msg {
		if err != nil {
			return watcherErrMsg{err: err}
		}
		return nil
	})
}

func (m Model) retarget() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	r := m.rows[m.cursor]
	var ts []LogTarget
	switch r.kind {
	case rowContainer:
		ts = []LogTarget{{ID: r.container.ID, Name: r.container.Name}}
	case rowStack:
		for _, c := range m.containers {
			if c.Project == r.project {
				ts = append(ts, LogTarget{ID: c.ID, Name: c.Name})
			}
		}
	}
	streamer := m.streamer
	return func() tea.Msg {
		streamer.SetTargets(ts)
		return nil
	}
}

func (m Model) composeCmd() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	r := m.rows[m.cursor]
	var ids []string
	switch r.kind {
	case rowContainer:
		ids = []string{r.container.ID}
	case rowStack:
		for _, c := range m.containers {
			if c.Project == r.project {
				ids = append(ids, c.ID)
			}
		}
	}
	project := r.project
	resources := m.resources
	return func() tea.Msg {
		containers := make([]container.InspectResponse, 0, len(ids))
		for _, id := range ids {
			res, err := resources.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
			if err != nil {
				return watcherErrMsg{err: err}
			}
			containers = append(containers, res.Container)
		}

		images := make(map[string]image.InspectResponse)
		for _, c := range containers {
			if _, ok := images[c.Config.Image]; ok {
				continue
			}
			res, err := resources.ImageInspect(context.Background(), c.Config.Image)
			if err != nil {
				return watcherErrMsg{err: err}
			}
			images[c.Config.Image] = res.InspectResponse
		}

		f := newComposeFile(containers, images, project)
		return composeMsg{yaml: f.render()}
	}
}

func (m Model) statsCmd(ids []string) tea.Cmd {
	if len(ids) == 0 {
		return nil
	}
	resources := m.resources
	return func() tea.Msg {
		stats := make([]ContainerStat, 0, len(ids))
		for _, id := range ids {
			res, err := resources.ContainerStats(context.Background(), id, client.ContainerStatsOptions{IncludePreviousSample: true})
			if err != nil {
				return watcherErrMsg{err: err}
			}
			var sr container.StatsResponse
			decErr := json.NewDecoder(res.Body).Decode(&sr)
			closeErr := res.Body.Close()
			if decErr != nil {
				return watcherErrMsg{err: decErr}
			}
			if closeErr != nil {
				return watcherErrMsg{err: closeErr}
			}
			stats = append(stats, newContainerStatFromResponse(id, sr))
		}
		return statsMsg{stats: stats}
	}
}

func (m Model) detailExtraCmd(id string, running bool) tea.Cmd {
	resources := m.resources
	return func() tea.Msg {
		res, err := resources.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
		if err != nil {
			return watcherErrMsg{err: err}
		}
		var titles []string
		var procs [][]string
		if running {
			top, err := resources.ContainerTop(context.Background(), id, client.ContainerTopOptions{})
			if err != nil {
				return watcherErrMsg{err: err}
			}
			titles = top.Titles
			procs = top.Processes
		}
		var mounts []string
		for _, mp := range res.Container.Mounts {
			entry := mp.Source + " -> " + mp.Destination
			if mp.Type == mount.TypeVolume {
				entry = mp.Name + " -> " + mp.Destination
			}
			mounts = append(mounts, entry)
		}
		sort.Strings(mounts)
		inspect, err := json.MarshalIndent(res.Container, "", "  ")
		if err != nil {
			return watcherErrMsg{err: err}
		}
		return detailExtraMsg{id: id, extra: detailExtra{env: res.Container.Config.Env, titles: titles, procs: procs, mounts: mounts, inspect: string(inspect)}}
	}
}

func (m *Model) syncSubVP() {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		m.subVP.SetContent("")
		return
	}
	r := m.rows[m.cursor]
	switch m.subtab {
	case subInfo:
		switch r.kind {
		case rowStack:
			m.subVP.SetContent(m.renderStackDetail(r.project))
		case rowContainer:
			m.subVP.SetContent(m.renderContainerDetail(r.container))
		}
	case subEnv:
		m.subVP.SetContent(m.renderEnvDetail(r.container))
	case subTop:
		m.subVP.SetContent(m.renderTopDetail(r.container))
	case subStats:
		m.subVP.SetContent(m.renderStatsDetail(r.container))
	case subInspect:
		m.subVP.SetContent(m.renderInspectDetail(r.container))
	case subLogs:
	}
}

func (m *Model) statsTickStartCmd() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	r := m.rows[m.cursor]
	if m.subtab != subStats || r.kind != rowContainer || r.container.State != "running" {
		return nil
	}
	cmd := m.statsCmd([]string{r.container.ID})
	if m.statsTicking {
		return cmd
	}
	m.statsTicking = true
	return tea.Batch(cmd, statsTickCmd())
}

func (m Model) lazyExtraCmd() tea.Cmd {
	if m.subtab != subEnv && m.subtab != subTop && m.subtab != subInspect {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	r := m.rows[m.cursor]
	if r.kind != rowContainer {
		return nil
	}
	if _, ok := m.extras[r.container.ID]; ok {
		return nil
	}
	return m.detailExtraCmd(r.container.ID, r.container.State == "running")
}

func (m *Model) clampSubtab() {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return
	}
	for _, k := range subtabsFor(m.rows[m.cursor].kind) {
		if k == m.subtab {
			return
		}
	}
	m.subtab = subInfo
}

func (m Model) selectedKey() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	return m.rows[m.cursor].key
}

func matchesFilter(filter string, fields ...string) bool {
	if filter == "" {
		return true
	}
	needle := strings.ToLower(filter)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), needle) {
			return true
		}
	}
	return false
}

func (m Model) filteredContainers() []Container {
	var out []Container
	for _, c := range m.containers {
		if matchesFilter(m.filter, c.Name, c.Service, c.Project, c.Image) {
			out = append(out, c)
		}
	}
	return out
}

func (m Model) filteredVolumes() []Volume {
	var out []Volume
	for _, v := range m.volumes {
		if matchesFilter(m.filter, v.Name) {
			out = append(out, v)
		}
	}
	return out
}

func (m Model) filteredNetworks() []Network {
	var out []Network
	for _, n := range m.networks {
		if matchesFilter(m.filter, n.Name) {
			out = append(out, n)
		}
	}
	return out
}

func (m Model) filteredImages() []Image {
	var out []Image
	for _, i := range m.images {
		if matchesFilter(m.filter, i.RepoTag) {
			out = append(out, i)
		}
	}
	return out
}

func newRows(containers []Container) []row {
	byProject := make(map[string][]Container)
	for _, c := range containers {
		byProject[c.Project] = append(byProject[c.Project], c)
	}
	projects := make([]string, 0, len(byProject))
	for p := range byProject {
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool {
		if (projects[i] == "") != (projects[j] == "") {
			return projects[j] == ""
		}
		return projects[i] < projects[j]
	})

	var rows []row
	for _, p := range projects {
		rows = append(rows, row{kind: rowStack, key: "stack:" + p, project: p})
		for _, c := range byProject[p] {
			rows = append(rows, row{kind: rowContainer, key: "id:" + c.ID, project: p, container: c})
		}
	}
	return rows
}

func indexOfKey(rows []row, key string) int {
	for i, r := range rows {
		if r.key == key {
			return i
		}
	}
	return 0
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading..."
	}

	list := m.renderList()
	rightContent := m.viewport.View()
	if m.tab == tabVolumes {
		list = m.renderVolumeList()
		rightContent = m.renderVolumeDetail()
	} else if m.tab == tabNetworks {
		list = m.renderNetworkList()
		rightContent = m.renderNetworkDetail()
	} else if m.tab == tabImages {
		list = m.renderImageList()
		rightContent = m.renderImageDetail()
	} else if m.compose != "" {
		rightContent = m.composeVP.View()
	} else if m.subtab != subLogs {
		rightContent = m.subVP.View()
	}
	left := m.paneStyle(focusList).Width(m.listWidth()).Height(m.panesHeight()).Render(list)
	right := m.paneStyle(focusLogs).Width(m.logsWidth()).Height(m.panesHeight()).Render(rightContent)

	header := styleHeader.Render(" 🦆 duck ") + " " + renderTabBar(m.tab) + "  " + styleDim.Render(fmt.Sprintf("%d containers", len(m.containers)))
	if m.err != nil {
		header += "  " + styleErr.Render("error: "+m.err.Error())
	}
	kinds := subtabsFor(rowContainer)
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		kinds = subtabsFor(m.rows[m.cursor].kind)
	}
	title := " " + renderSubtabBar(m.subtab, kinds)
	if m.tab == tabContainers && m.compose != "" {
		title = " compose"
	}
	leftTitle := " containers"
	if m.tab == tabVolumes {
		leftTitle = " volumes"
		title = " detail"
	} else if m.tab == tabNetworks {
		leftTitle = " networks"
		title = " detail"
	} else if m.tab == tabImages {
		leftTitle = " images"
		title = " detail"
	}
	if !m.filtering && m.filter != "" {
		leftTitle += " /" + m.filter
	}
	titles := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.listWidth()+2).Render(styleTitle.Render(leftTitle)),
		styleTitle.Render(truncate(title, m.logsWidth())),
	)

	var footer string
	if m.tab == tabVolumes {
		hint := ""
		vols := m.filteredVolumes()
		if m.volCursor >= 0 && m.volCursor < len(vols) {
			if volumeUsedBy(m.volumes, m.containers)[vols[m.volCursor].Name] > 0 {
				hint = "d: volume in use"
			}
		}
		footer = resourceFooter(m.confirm, hint)
	} else if m.tab == tabNetworks {
		hint := ""
		nets := m.filteredNetworks()
		if m.netCursor >= 0 && m.netCursor < len(nets) {
			n := nets[m.netCursor]
			used := networkUsedBy(m.networks, m.containers)[n.Name]
			if used > 0 {
				hint = "d: network in use"
			} else if isBuiltinNetwork(n.Name) {
				hint = "d: builtin network"
			}
		}
		footer = resourceFooter(m.confirm, hint)
	} else if m.tab == tabImages {
		hint := ""
		imgs := m.filteredImages()
		if m.imgCursor >= 0 && m.imgCursor < len(imgs) {
			if imageUsedBy(m.images, m.containers)[imgs[m.imgCursor].ID] > 0 {
				hint = "d: image in use"
			}
		}
		footer = resourceFooter(m.confirm, hint)
	} else if m.tab == tabContainers && m.compose != "" {
		footer = " j/k scroll  g/G top/bottom  esc back  q quit"
	} else if m.tab == tabContainers && m.subtab != subLogs {
		footer = " [/] view  j/k scroll  esc logs  q quit"
	} else if m.confirm != nil {
		footer = resourceFooter(m.confirm, "")
	} else {
		footer = " j/k move  tab focus  enter detail  y compose  e exec  s/S stop/start  r restart  p pause  K kill  d delete  P prune  left/right tab  q quit"
	}
	if m.filtering {
		footer = " filter: /" + m.filter
	}

	return header + "\n" + titles + "\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n" + styleDim.Render(footer)
}

func renderTabBar(tab tabID) string {
	labels := []string{"1:containers", "2:volumes", "3:networks", "4:images"}
	parts := make([]string, len(labels))
	for i, label := range labels {
		if tabID(i) == tab {
			parts[i] = styleSelected.Render(" " + label + " ")
		} else {
			parts[i] = styleDim.Render(label)
		}
	}
	return strings.Join(parts, "  ")
}

func (m Model) renderList() string {
	var b strings.Builder
	w := m.listWidth()
	for i, r := range m.rows {
		var line string
		switch r.kind {
		case rowStack:
			name := r.project
			if name == "" {
				name = "standalone"
			}
			line = styleStack.Render(truncate("▾ "+name, w))
		case rowContainer:
			dot := styleDotStopped.Render("●")
			if r.container.State == "running" {
				dot = styleDotRunning.Render("●")
			}
			label := r.container.Name
			if r.container.Service != "" {
				label = r.container.Service + " " + styleDim.Render(r.container.Name)
			}
			line = "  " + dot + " " + truncate(label, w-4)
		}
		if i == m.cursor {
			line = styleSelected.Render(fmt.Sprintf("%-*s", w, line))
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(m.rows) == 0 {
		b.WriteString(styleDim.Render("no containers 🦆"))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderKV(label, value string) string {
	return styleDim.Render(fmt.Sprintf("%-9s", label)) + " " + value
}

func (m Model) renderContainerDetail(c Container) string {
	dot := styleDotStopped.Render("●")
	if c.State == "running" {
		dot = styleDotRunning.Render("●")
	}

	var b strings.Builder
	b.WriteString(renderKV("name:", c.Name) + "\n")
	b.WriteString(renderKV("image:", c.Image) + "\n")
	b.WriteString(renderKV("state:", dot+" "+c.State) + "\n")
	b.WriteString(renderKV("status:", c.Status) + "\n")
	if c.Project != "" {
		b.WriteString(renderKV("project:", c.Project) + "\n")
	}
	if c.Service != "" {
		b.WriteString(renderKV("service:", c.Service) + "\n")
	}
	if s, ok := m.stats[c.ID]; ok {
		b.WriteString(renderKV("cpu:", fmt.Sprintf("%.1f%%", s.CPUPercent)) + "\n")
		b.WriteString(renderKV("mem:", formatMemBytes(s.MemUsage)+" / "+formatMemBytes(s.MemLimit)) + "\n")
	}

	mounts := c.Volumes
	if extra, ok := m.extras[c.ID]; ok && len(extra.mounts) > 0 {
		mounts = extra.mounts
	}

	writeSection := func(title string, entries []string) {
		if len(entries) == 0 {
			return
		}
		b.WriteString("\n" + styleSection.Render(title) + "\n")
		for _, e := range entries {
			b.WriteString("  " + e + "\n")
		}
	}
	writeSection("PORTS", c.Ports)
	writeSection("MOUNTS", mounts)
	writeSection("NETWORKS", c.Networks)

	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderEnvDetail(c Container) string {
	var b strings.Builder
	b.WriteString(styleSection.Render("ENV") + "\n")

	extra, ok := m.extras[c.ID]
	if !ok {
		b.WriteString(styleDim.Render("loading..."))
		return b.String()
	}
	if len(extra.env) == 0 {
		b.WriteString(styleDim.Render("no env"))
		return b.String()
	}
	for _, e := range extra.env {
		name := e
		rest := ""
		if idx := strings.Index(e, "="); idx >= 0 {
			name = e[:idx]
			rest = e[idx:]
		}
		b.WriteString(styleDim.Render(name) + rest + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderTopDetail(c Container) string {
	var b strings.Builder
	b.WriteString(styleSection.Render("PROCESSES") + "\n")

	if c.State != "running" {
		b.WriteString(styleDim.Render("container not running"))
		return b.String()
	}
	extra, ok := m.extras[c.ID]
	if !ok {
		b.WriteString(styleDim.Render("loading..."))
		return b.String()
	}

	widths := make([]int, len(extra.titles))
	for i, title := range extra.titles {
		widths[i] = len(title)
	}
	for _, proc := range extra.procs {
		for i, cell := range proc {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	writeRow := func(cells []string) string {
		var row strings.Builder
		for i, cell := range cells {
			if i > 0 {
				row.WriteString("  ")
			}
			row.WriteString(fmt.Sprintf("%-*s", widths[i], cell))
		}
		return row.String()
	}

	b.WriteString(styleDim.Render(writeRow(extra.titles)) + "\n")
	for _, proc := range extra.procs {
		b.WriteString(writeRow(proc) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderStatsDetail(c Container) string {
	var b strings.Builder
	b.WriteString(styleSection.Render("STATS") + "\n")

	if c.State != "running" {
		b.WriteString(styleDim.Render("container not running"))
		return b.String()
	}
	s, ok := m.stats[c.ID]
	if !ok {
		b.WriteString(styleDim.Render("sampling..."))
		return b.String()
	}
	b.WriteString(renderKV("cpu:", fmt.Sprintf("%.1f%%", s.CPUPercent)) + "\n")
	b.WriteString(renderKV("mem:", formatMemBytes(s.MemUsage)+" / "+formatMemBytes(s.MemLimit)))
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderInspectDetail(c Container) string {
	extra, ok := m.extras[c.ID]
	if !ok {
		return styleDim.Render("loading...")
	}
	return extra.inspect
}

func (m Model) renderStackDetail(project string) string {
	name := project
	if name == "" {
		name = "standalone"
	}

	var b strings.Builder
	b.WriteString(renderKV("project:", name) + "\n")

	var stackContainers []Container
	for _, c := range m.containers {
		if c.Project == project {
			stackContainers = append(stackContainers, c)
		}
	}

	b.WriteString("\n" + styleSection.Render("SERVICES") + "\n")
	for _, c := range stackContainers {
		dot := styleDotStopped.Render("●")
		if c.State == "running" {
			dot = styleDotRunning.Render("●")
		}
		label := c.Service
		if label == "" {
			label = c.Name
		}
		line := "  " + dot + " " + label + "  " + styleDim.Render(c.Image)
		if s, ok := m.stats[c.ID]; ok {
			line += "  " + styleDim.Render(fmt.Sprintf("cpu %.1f%%  mem %s", s.CPUPercent, formatMemBytes(s.MemUsage)))
		}
		b.WriteString(line + "\n")
	}

	ports := make(map[string]struct{})
	volumes := make(map[string]struct{})
	networks := make(map[string]struct{})
	for _, c := range stackContainers {
		for _, p := range c.Ports {
			ports[p] = struct{}{}
		}
		for _, v := range c.Volumes {
			volumes[v] = struct{}{}
		}
		for _, n := range c.Networks {
			networks[n] = struct{}{}
		}
	}

	writeSet := func(title string, set map[string]struct{}) {
		if len(set) == 0 {
			return
		}
		entries := make([]string, 0, len(set))
		for e := range set {
			entries = append(entries, e)
		}
		sort.Strings(entries)
		b.WriteString("\n" + styleSection.Render(title) + "\n")
		for _, e := range entries {
			b.WriteString("  " + e + "\n")
		}
	}
	writeSet("PORTS", ports)
	writeSet("VOLUMES", volumes)
	writeSet("NETWORKS", networks)

	return strings.TrimRight(b.String(), "\n")
}

func resourceFooter(confirm *pendingDelete, hint string) string {
	footer := " j/k move  left/right tab  d delete  P prune  q quit"
	if confirm != nil {
		footer += "  delete " + confirm.label + "? y/n"
	} else if hint != "" {
		footer += "  " + hint
	}
	return footer
}

func (m Model) renderResourceRows(rows []string, cursor int, empty string) string {
	var b strings.Builder
	w := m.listWidth()
	for i, row := range rows {
		line := truncate(row, w)
		if i == cursor {
			line = styleSelected.Render(fmt.Sprintf("%-*s", w, line))
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(rows) == 0 {
		b.WriteString(styleDim.Render(empty))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderVolumeList() string {
	used := volumeUsedBy(m.volumes, m.containers)
	vols := m.filteredVolumes()
	rows := make([]string, 0, len(vols))
	for _, v := range vols {
		rows = append(rows, formatVolumeRow(v, used[v.Name]))
	}
	return m.renderResourceRows(rows, m.volCursor, "no volumes 🦆")
}

func formatVolumeRow(v Volume, used int) string {
	return fmt.Sprintf("%s  %s  used-by:%d", v.Name, v.Driver, used)
}

func (m Model) renderVolumeDetail() string {
	vols := m.filteredVolumes()
	if m.volCursor < 0 || m.volCursor >= len(vols) {
		return styleDim.Render("no volumes 🦆")
	}
	v := vols[m.volCursor]

	var b strings.Builder
	b.WriteString("name: " + v.Name + "\n")
	b.WriteString("driver: " + v.Driver + "\n")
	b.WriteString("mountpoint: " + v.Mountpoint + "\n")
	b.WriteString("created: " + v.Created + "\n")

	if len(v.Labels) > 0 {
		keys := make([]string, 0, len(v.Labels))
		for k := range v.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("labels:\n")
		for _, k := range keys {
			b.WriteString("  " + k + "=" + v.Labels[k] + "\n")
		}
	}

	var users []string
	for _, c := range m.containers {
		for _, name := range c.Volumes {
			if name == v.Name {
				users = append(users, c.Name)
				break
			}
		}
	}
	if len(users) > 0 {
		b.WriteString("used by:\n")
		for _, name := range users {
			b.WriteString("  " + name + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderNetworkList() string {
	used := networkUsedBy(m.networks, m.containers)
	nets := m.filteredNetworks()
	rows := make([]string, 0, len(nets))
	for _, n := range nets {
		rows = append(rows, formatNetworkRow(n, used[n.Name]))
	}
	return m.renderResourceRows(rows, m.netCursor, "no networks 🦆")
}

func formatNetworkRow(n Network, used int) string {
	parts := []string{n.Name, n.Driver}
	if n.Subnet != "" {
		parts = append(parts, n.Subnet)
	}
	parts = append(parts, fmt.Sprintf("used-by:%d", used))
	return strings.Join(parts, "  ")
}

func isBuiltinNetwork(name string) bool {
	switch name {
	case "bridge", "host", "none":
		return true
	}
	return false
}

func (m Model) renderNetworkDetail() string {
	nets := m.filteredNetworks()
	if m.netCursor < 0 || m.netCursor >= len(nets) {
		return styleDim.Render("no networks 🦆")
	}
	n := nets[m.netCursor]

	var b strings.Builder
	b.WriteString("id: " + n.ID + "\n")
	b.WriteString("driver: " + n.Driver + "\n")
	b.WriteString("subnet: " + n.Subnet + "\n")

	var users []string
	for _, c := range m.containers {
		for _, name := range c.Networks {
			if name == n.Name {
				users = append(users, c.Name)
				break
			}
		}
	}
	if len(users) > 0 {
		b.WriteString("used by:\n")
		for _, name := range users {
			b.WriteString("  " + name + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderImageList() string {
	used := imageUsedBy(m.images, m.containers)
	imgs := m.filteredImages()
	rows := make([]string, 0, len(imgs))
	for _, i := range imgs {
		rows = append(rows, formatImageRow(i, used[i.ID]))
	}
	return m.renderResourceRows(rows, m.imgCursor, "no images 🦆")
}

func formatImageRow(i Image, used int) string {
	return fmt.Sprintf("%s  %s  used-by:%d", i.RepoTag, formatImageSize(i.Size), used)
}

func (m Model) renderImageDetail() string {
	imgs := m.filteredImages()
	if m.imgCursor < 0 || m.imgCursor >= len(imgs) {
		return styleDim.Render("no images 🦆")
	}
	img := imgs[m.imgCursor]

	var b strings.Builder
	b.WriteString("id: " + img.ID + "\n")
	b.WriteString("repo:tag: " + img.RepoTag + "\n")
	b.WriteString("size: " + formatImageSize(img.Size) + "\n")

	var users []string
	for _, c := range m.containers {
		if c.ImageID == img.ID {
			users = append(users, c.Name)
		}
	}
	if len(users) > 0 {
		b.WriteString("used by:\n")
		for _, name := range users {
			b.WriteString("  " + name + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderLogs() string {
	var b strings.Builder
	for _, l := range m.logs {
		b.WriteString(sourceStyle(l.source).Render(l.source))
		b.WriteString(styleDim.Render(" │ "))
		b.WriteString(l.line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) paneStyle(area focusArea) lipgloss.Style {
	if m.focus == area {
		return styleBorderOn
	}
	return styleBorder
}

func (m Model) listWidth() int {
	w := m.width / 3
	if w < 24 {
		w = 24
	}
	if w > 44 {
		w = 44
	}
	return w
}

func (m Model) logsWidth() int {
	return m.width - m.listWidth() - 4
}

func (m Model) panesHeight() int {
	h := m.height - 5
	if h < 3 {
		h = 3
	}
	return h
}

func sourceStyle(source string) lipgloss.Style {
	h := fnv.New32a()
	if _, err := h.Write([]byte(source)); err != nil {
		return sourcePalette[0]
	}
	return sourcePalette[int(h.Sum32())%len(sourcePalette)]
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}
