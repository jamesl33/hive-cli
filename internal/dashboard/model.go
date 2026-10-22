package dashboard

import (
	"context"
	"fmt"
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
}

// defaultWindow is the position in windows shown at start.
const defaultWindow = 4

// helpText lists the keys.
const helpText = `w   next time window
W   previous time window
?   toggle this help
q   quit`

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
	width, height int
	help          bool
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
	connected bool
	help      bool
	err       error
	cache     *viewCache
}

// New returns a dashboard which displays the reports from the source.
func New(ctx context.Context, source Source, store *Store) Model {
	return Model{ctx: ctx, source: source, store: store, window: defaultWindow, connected: true, cache: &viewCache{}}
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
		case "?":
			m.help = !m.help
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
		m.connected = m.source.Connected()
		return m, m.tick()
	case errMsg:
		if m.ctx.Err() == nil {
			m.err = msg.err
		}

		return m, tea.Quit
	}

	return m, nil
}

// cell returns the time covered by one cell of a chart, which is two braille dots wide.
func (m Model) cell() time.Duration {
	return windows[m.window] / time.Duration(max(m.width-2, 1)*2)
}

// viewKey returns what View would draw at the time depends on.
func (m Model) viewKey(now time.Time) viewKey {
	return viewKey{
		version:   m.store.version,
		window:    m.window,
		width:     m.width,
		height:    m.height,
		help:      m.help,
		connected: m.connected,
		bucket:    now.UnixNano() / int64(m.cell()),
	}
}

// View draws the dashboard, reusing the last drawing if nothing which it shows has changed.
func (m Model) View() string {
	now := time.Now()
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
		return box("Help", helpText, m.width)
	}

	devices := "Devices"

	if !m.connected {
		devices += lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("  reconnecting to broker...")
	}

	if len(m.store.Order) == 0 {
		msg := "Waiting for devices to report; sensors only send when their reading changes."

		return box(devices, msg, m.width)
	}

	batteries := m.store.batteries()
	rows := []string{box(devices, m.store.legend(now), m.width)}

	// Besides the charts there are the legend row, the two boiler rows, the battery rows and the
	// borders of each box.
	hasHumidity := len(m.store.Series[Humidity]) > 0
	boxes := 3 + boolInt(hasHumidity) + boolInt(len(batteries) > 0)
	tempH, humH := m.height-1-2*boxes-2-len(batteries), 0

	if hasHumidity {
		tempH, humH = tempH*3/5, tempH-tempH*3/5
	}

	inner := m.width - 2
	span := "(" + formatWindow(windows[m.window]) + ")"

	temp := m.store.renderChart(Temperature, inner, tempH, from, now)
	rows = append(rows, box("Temperature °C "+span, temp.view, m.width))

	if humH > 0 {
		humidity := m.store.renderChart(Humidity, inner, humH, from, now)
		rows = append(rows, box("Humidity % "+span, humidity.view, m.width))
	}

	rows = append(rows, box("Boiler "+span, strings.Join(m.store.boilerRows(temp, from, now), "\n"), m.width))

	if len(batteries) > 0 {
		rows = append(rows, box("Battery", strings.Join(batteries, "\n"), m.width))
	}

	return strings.Join(rows, "\n")
}

// box draws the body inside a border of the width, with the title set into the top edge.
func box(title, body string, width int) string {
	inner := width - 2
	top := "┌─ " + title + " " + strings.Repeat("─", max(inner-lipgloss.Width(title)-3, 0)) + "┐"

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

	return fmt.Sprintf("%dh", int(d.Hours()))
}
