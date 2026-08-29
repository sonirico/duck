package main

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type focusArea int

const (
	focusList focusArea = iota
	focusLogs
)

type logLine struct {
	source string
	line   string
}

type logRetargeter interface {
	SetTargets(ts []LogTarget)
}

type Model struct {
	streamer logRetargeter

	containers []Container
	rows       []row
	cursor     int
	focus      focusArea

	logs     []logLine
	logTitle string
	viewport viewport.Model
	follow   bool

	width  int
	height int
	err    error
}

func NewModel(streamer logRetargeter) Model {
	return Model{streamer: streamer, follow: true}
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
		return m, nil

	case containersMsg:
		prevKey := m.selectedKey()
		m.containers = msg.containers
		m.rows = newRows(msg.containers)
		m.cursor = indexOfKey(m.rows, prevKey)
		if prevKey == "" || m.selectedKey() != prevKey {
			return m, m.retarget()
		}
		return m, nil

	case watcherErrMsg:
		m.err = msg.err
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

	if m.focus == focusLogs {
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
			return m, m.retarget()
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			return m, m.retarget()
		}
	case "g":
		if len(m.rows) > 0 && m.cursor != 0 {
			m.cursor = 0
			return m, m.retarget()
		}
	case "G":
		if len(m.rows) > 0 && m.cursor != len(m.rows)-1 {
			m.cursor = len(m.rows) - 1
			return m, m.retarget()
		}
	}
	return m, nil
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

func (m Model) selectedKey() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	return m.rows[m.cursor].key
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
	left := m.paneStyle(focusList).Width(m.listWidth()).Height(m.panesHeight()).Render(list)
	right := m.paneStyle(focusLogs).Width(m.logsWidth()).Height(m.panesHeight()).Render(m.viewport.View())

	header := styleHeader.Render(fmt.Sprintf(" duck  %d containers", len(m.containers)))
	if m.err != nil {
		header += "  " + styleErr.Render("error: "+m.err.Error())
	}
	title := " logs"
	if m.logTitle != "" {
		title = " logs: " + m.logTitle
	}
	titles := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.listWidth()+2).Render(styleTitle.Render(" containers")),
		styleTitle.Render(truncate(title, m.logsWidth())),
	)
	footer := styleDim.Render(" j/k move  g/G top/bottom  tab focus  q quit")

	return header + "\n" + titles + "\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n" + footer
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
		b.WriteString(styleDim.Render("no containers"))
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
	h := m.height - 4
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
