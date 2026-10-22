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
	// refresh is how often the dashboard redraws when nothing has been reported.
	refresh = time.Second

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
const defaultWindow = 1

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

// Model is the dashboard.
type Model struct {
	ctx       context.Context
	source    Source
	store     *Store
	width     int
	height    int
	window    int // position in windows
	connected bool
	err       error
}

// New returns a dashboard which displays the reports from the source.
func New(ctx context.Context, source Source, store *Store) Model {
	return Model{ctx: ctx, source: source, store: store, window: defaultWindow, connected: true}
}

// Err returns the failure which ended the dashboard, if any.
func (m Model) Err() error {
	return m.err
}

// Init starts reading reports and redrawing.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.wait(), tick())
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

// tick returns a command which triggers a redraw after the refresh interval.
func tick() tea.Cmd {
	return tea.Tick(refresh, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles input and new reports.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "w":
			m.window = (m.window + 1) % len(windows)
		case "W":
			m.window = (m.window + len(windows) - 1) % len(windows)
		}
	case updateMsg:
		m.store.Apply(z2m.Update(msg))
		return m, m.wait()
	case tickMsg:
		m.connected = m.source.Connected()
		return m, tick()
	case errMsg:
		if m.ctx.Err() == nil {
			m.err = msg.err
		}

		return m, tea.Quit
	}

	return m, nil
}

// View draws the dashboard.
func (m Model) View() string {
	if m.width < minWidth || m.height < minHeight {
		return "Terminal too small"
	}

	now := time.Now()
	from := now.Add(-windows[m.window])

	title := lipgloss.NewStyle().Bold(true).Render("Dashboard")
	title += fmt.Sprintf("  last %s  (w/W: change window, q: quit)", formatWindow(windows[m.window]))

	if !m.connected {
		title += lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("  reconnecting to broker...")
	}

	batteries := m.store.batteries()
	rows := []string{title, m.store.legend()}

	// Besides the charts there are the title and legend, a heading for each chart, two boiler rows
	// and the battery heading and rows.
	tempH, humH := m.height-len(rows)-1-2-1-len(batteries), 0

	if len(m.store.Series[Humidity]) > 0 {
		free := tempH - 1
		tempH = free * 3 / 5
		humH = free - tempH
	}

	temp := m.store.renderChart(Temperature, m.width, tempH, from, now)
	rows = append(rows, "Temperature °C", temp.view)

	if humH > 0 {
		humidity := m.store.renderChart(Humidity, m.width, humH, from, now)
		rows = append(rows, "Humidity %", humidity.view)
	}

	rows = append(rows, m.store.boilerRows(temp, from, now)...)
	rows = append(rows, "Battery")
	rows = append(rows, batteries...)

	return lipgloss.NewStyle().MaxWidth(m.width).Render(strings.Join(rows, "\n"))
}

// formatWindow formats a window as minutes or hours, e.g. 15m or 3h.
func formatWindow(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}

	return fmt.Sprintf("%dh", int(d.Hours()))
}
