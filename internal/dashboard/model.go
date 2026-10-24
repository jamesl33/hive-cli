package dashboard

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jamesl33/hive-cli/internal/z2m"
)

const (
	// minRefresh is the shortest interval between redraw checks when nothing has been reported. The chart
	// only moves once per cell, so shorter windows need checks this often.
	minRefresh = time.Second

	// maxRefresh is the longest interval between redraw checks when nothing has been reported. It keeps
	// the connection status current.
	maxRefresh = 10 * time.Second

	// minWidth is the narrowest terminal the dashboard can draw in.
	minWidth = 50

	// minChartHeight is the shortest temperature or humidity chart which is drawn.
	minChartHeight = 3

	// minHeight is the shortest terminal the dashboard can draw in.
	minHeight = 18
)

// windows are the time spans the charts can show, cycled with the w and W keys.
var windows = []time.Duration{
	time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	45 * time.Minute,
	time.Hour,
	3 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
	7 * 24 * time.Hour,
	14 * 24 * time.Hour,
	30 * 24 * time.Hour,
	6 * 30 * 24 * time.Hour,
	12 * 30 * 24 * time.Hour,
}

// page is a screen of the dashboard, cycled with the tab and shift+tab keys.
type page int

const (
	pageClimate page = iota
	pageFuels
)

// pageNames are the names of the pages, in order.
var pageNames = []string{"Climate", "Fuels"}

// defaultWindow is the position in windows shown at start.
const defaultWindow = 4

// helpText lists the keys.
const helpText = `tab    next page
shift+tab previous page
w      next time window
W      previous time window
1-8    show or hide a device on the charts
o      show or hide the outdoor sensor on the charts
g, e   show or hide gas or electricity on the usage chart
space pause or resume the display
?      toggle this help
q      quit`

// Source is a live stream of device reports.
type Source interface {
	// Next blocks until a device reports its state.
	Next(ctx context.Context) (z2m.Update, error)

	// Connected returns whether the connection to the broker is up.
	Connected() bool
}

// updateMsg carries a device report.
type updateMsg z2m.Update

// errMsg carries the failure which ended the stream.
type errMsg struct{ err error }

// tickMsg triggers a redraw.
type tickMsg time.Time

// viewKey is everything which changes what View draws. The bucket is the time in units of one chart
// cell, as the chart looks the same until the time moves on by that much.
type viewKey struct {
	version       uint64
	window        int
	page          page
	width, height int
	help          bool
	paused        bool
	hidden        string
	connected     bool
	bucket        int64
}

// viewCache is the last drawing, which is reused for as long as its key matches.
type viewCache struct {
	key  viewKey
	view string
	ok   bool
}

// Model is the dashboard.
type Model struct {
	ctx       context.Context
	source    Source
	store     *Store
	width     int
	height    int
	window    int // position in windows
	page      page
	connected bool
	help      bool
	err       error
	cache     *viewCache

	// hidden is the devices left off the charts, so they scale to the others. It is replaced, not
	// changed, by toggle, as models share it.
	hidden map[string]bool

	// paused freezes the drawing at pausedAt, and at the reports which had arrived by pausedVersion.
	paused        bool
	pausedAt      time.Time
	pausedVersion uint64
}

// New returns a dashboard which displays the reports from the source, with the outdoor sensor hidden
// from the charts.
func New(ctx context.Context, source Source, store *Store) Model {
	m := Model{ctx: ctx, source: source, store: store, window: defaultWindow, connected: true, cache: &viewCache{}}

	// The outdoor sensor starts hidden, as it stretches the charts well past the rooms.
	return m.toggle(store.Outdoors)
}

// Err returns the failure which ended the dashboard, if any.
func (m Model) Err() error {
	return m.err
}

// WindowStart returns the start of the window shown at the time.
func (m Model) WindowStart(now time.Time) time.Time {
	return now.Add(-windows[m.window])
}

// Init starts reading reports and redrawing.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.wait(), m.tick())
}

// wait returns a command which delivers the next report.
func (m Model) wait() tea.Cmd {
	return func() tea.Msg {
		u, err := m.source.Next(m.ctx)
		if err != nil {
			return errMsg{err}
		}

		return updateMsg(u)
	}
}

// tick returns a command which triggers a redraw after the time it takes the chart to move by a cell.
func (m Model) tick() tea.Cmd {
	return tea.Tick(min(max(m.cell(), minRefresh), maxRefresh), func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles input and new reports.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.help {
				m.help = false

				return m, nil
			}

			return m, tea.Quit
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.page = (m.page + 1) % page(len(pageNames))
		case "shift+tab":
			m.page = (m.page + page(len(pageNames)) - 1) % page(len(pageNames))
		case "?":
			m.help = !m.help
		case "o":
			m = m.toggle(m.store.Outdoors)
		case "g":
			m = m.toggle(string(Gas))
		case "e":
			m = m.toggle(string(Electricity))
		case "1", "2", "3", "4", "5", "6", "7", "8":
			if i := int(msg.String()[0] - '1'); i < len(m.store.Order) {
				m = m.toggle(m.store.Order[i])
			}
		case " ":
			m.paused = !m.paused
			m.pausedAt, m.pausedVersion = time.Now(), m.store.version
		case "w", "W":
			step := 1
			if msg.String() == "W" {
				step = len(windows) - 1
			}

			m.window = (m.window + step) % len(windows)

			err := m.store.Reload(m.WindowStart(time.Now()))
			if err != nil {
				m.err = fmt.Errorf("failed to load history: %w", err)

				return m, tea.Quit
			}
		}
	case updateMsg:
		m.store.Apply(z2m.Update(msg))
		return m, m.wait()
	case tickMsg:
		if !m.paused {
			m.connected = m.source.Connected()
		}

		return m, m.tick()
	case errMsg:
		if m.ctx.Err() == nil {
			m.err = msg.err
		}

		return m, tea.Quit
	}

	return m, nil
}

// toggle shows the device on the charts if it is hidden, and hides it if it is shown.
func (m Model) toggle(device string) Model {
	if device == "" {
		return m
	}

	hidden := maps.Clone(m.hidden)
	if hidden == nil {
		hidden = map[string]bool{}
	}

	hidden[device] = !hidden[device]
	m.hidden = hidden

	return m
}

// hiddenKey returns the hidden devices as a string, for the key of a drawing.
func (m Model) hiddenKey() string {
	var names []string

	for device, hide := range m.hidden {
		if hide {
			names = append(names, device)
		}
	}

	slices.Sort(names)

	return strings.Join(names, "\x00")
}

// cell returns the time covered by one cell of a chart, which is two braille dots wide.
func (m Model) cell() time.Duration {
	return windows[m.window] / time.Duration(max(m.width-2, 1)*2)
}

// now returns the time the drawing shows, which stands still while paused.
func (m Model) now() time.Time {
	if m.paused {
		return m.pausedAt
	}

	return time.Now()
}

// viewKey returns what View would draw at the time depends on.
func (m Model) viewKey(now time.Time) viewKey {
	version := m.store.version
	if m.paused {
		version = m.pausedVersion
	}

	return viewKey{
		version:   version,
		window:    m.window,
		page:      m.page,
		width:     m.width,
		height:    m.height,
		help:      m.help,
		paused:    m.paused,
		hidden:    m.hiddenKey(),
		connected: m.connected,
		bucket:    now.UnixNano() / int64(m.cell()),
	}
}

// View draws the dashboard, reusing the last drawing if nothing which it shows has changed.
func (m Model) View() string {
	now := m.now()
	key := m.viewKey(now)

	if m.cache.ok && m.cache.key == key {
		return m.cache.view
	}

	view := m.draw(now)
	*m.cache = viewCache{key: key, view: view, ok: true}

	return view
}

// draw draws the dashboard as it is at the time.
func (m Model) draw(now time.Time) string {
	if m.width < minWidth || m.height < minHeight {
		return "Terminal too small"
	}

	from := now.Add(-windows[m.window])

	if m.help {
		return box("Help", "", helpText, m.width)
	}

	if len(m.store.Order) == 0 {
		msg := "Waiting for devices to report; sensors only send when their reading changes."

		return box(m.title("Devices"), "", msg, m.width)
	}

	if m.page == pageFuels {
		return strings.Join(m.drawFuels(now, from), "\n")
	}

	return strings.Join(m.drawClimate(now, from), "\n")
}

// title returns the title of the box at the top of a page: the name, the state of the display, and the
// pages with the current one highlighted.
func (m Model) title(name string) string {
	if m.paused {
		name += lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("  paused")
	}

	if !m.connected {
		name += lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("  reconnecting to broker...")
	}

	return name + m.tabs()
}

// tabs lists the pages, with the current one highlighted.
func (m Model) tabs() string {
	var b strings.Builder

	for i, name := range pageNames {
		style := lipgloss.NewStyle().Foreground(staleColor)
		if page(i) == m.page {
			style = lipgloss.NewStyle().Bold(true).Reverse(true)
		}

		b.WriteString("  " + style.Render(" "+name+" "))
	}

	return b.String()
}

// drawClimate draws the rows of the climate page: the temperature chart above the humidity chart, with
// the boiler and batteries below.
func (m Model) drawClimate(now, from time.Time) []string {
	batteries := m.store.batteries()
	rows := []string{box(m.title("Devices"), formatWindow(windows[m.window]), m.store.legend(now, m.hidden), m.width)}

	// Besides the charts there are the legend row, the two boiler rows, the battery rows and the
	// borders of each box. When the charts would get too short, the humidity and then the batteries
	// are left off.
	hasHumidity := len(m.store.Series[Humidity]) > 0
	h := m.climateHeight(hasHumidity, len(batteries))

	if h < minChartHeight*(1+boolInt(hasHumidity)) {
		hasHumidity = false
		h = m.climateHeight(false, len(batteries))
	}

	if h < minChartHeight {
		batteries = nil
		h = m.climateHeight(false, 0)
	}

	// Humidity shares the height with the temperature, half each
	tempH, humH := h, 0
	if hasHumidity {
		humH = h / 2
		tempH = h - humH
	}

	temp := m.store.renderChart(Temperature, m.hidden, m.width-2, tempH, from, now)
	rows = append(rows, box("Temperature °C", "", temp.view, m.width))

	if hasHumidity {
		humidity := m.store.renderChart(Humidity, m.hidden, m.width-2, humH, from, now)
		rows = append(rows, box("Humidity %", "", humidity.view, m.width))
	}

	boiler := formatOn(boilerOnTime(m.store.Series[Boiler][m.store.Receiver.Name], from, now), windows[m.window])
	rows = append(rows, box("Boiler "+boiler, "", strings.Join(m.store.boilerRows(m.width-2, from, now), "\n"), m.width))

	if len(batteries) > 0 {
		rows = append(rows, box("Battery", "", strings.Join(batteries, "\n"), m.width))
	}

	return rows
}

// climateHeight returns the height left for the charts of the climate page, given the humidity chart and
// the rows of batteries which are drawn.
func (m Model) climateHeight(humidity bool, batteries int) int {
	boxes := 3 + boolInt(humidity) + boolInt(batteries > 0)

	return m.height - 2*boxes - 3 - batteries
}

// drawFuels draws the rows of the fuels page: the energy used by each fuel, and the usage chart.
func (m Model) drawFuels(now, from time.Time) []string {
	if !m.store.hasUsage() {
		return []string{box(m.title("Fuels"), "", "No energy use recorded.", m.width)}
	}

	// Besides the chart there are the fuels row and the borders of each box.
	h := m.height - 2*2 - 1

	rows := []string{box(m.title("Fuels"), formatWindow(windows[m.window]), m.store.fuels(from, m.hidden), m.width)}

	if h < minUsageHeight {
		return rows
	}

	usage := m.store.renderUsage(m.width-2, h, from, now, m.hidden)

	return append(rows, box("Usage kWh/30m", m.store.usageCorner(now), usage, m.width))
}

// box draws the body inside a border of the width, with the title set into the top edge, and the
// corner at its right end if there is room.
func box(title, corner, body string, width int) string {
	inner := width - 2
	title = lipgloss.NewStyle().MaxWidth(max(inner-3, 0)).Render(title)
	fill := inner - lipgloss.Width(title) - 3

	if corner != "" && fill >= lipgloss.Width(corner)+3 {
		corner = " " + corner + " ─"
	} else {
		corner = ""
	}

	top := "┌─ " + title + " " + strings.Repeat("─", max(fill-lipgloss.Width(corner), 0)) + corner + "┐"

	lines := []string{top}

	clip := lipgloss.NewStyle().MaxWidth(inner)

	for line := range strings.SplitSeq(body, "\n") {
		w := lipgloss.Width(line)
		if w > inner {
			line = clip.Render(line)
			w = lipgloss.Width(line)
		}

		lines = append(lines, "│"+line+strings.Repeat(" ", max(inner-w, 0))+"│")
	}

	lines = append(lines, "└"+strings.Repeat("─", inner)+"┘")

	return strings.Join(lines, "\n")
}

func boolInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// formatWindow formats a window as minutes or hours, e.g. 15m or 3h.
func formatWindow(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}

	if d >= 180*24*time.Hour {
		return fmt.Sprintf("%dmon", int(d.Hours())/(30*24))
	}

	if d >= 48*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}

	return fmt.Sprintf("%dh", int(d.Hours()))
}
