package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gen2brain/beeep"
)

// --- Types ---

type mode int

const (
	workMode mode = iota
	shortBreakMode
	longBreakMode
)

type screen int

const (
	timerScreen screen = iota
	settingsScreen
)

type techniqueType int

const (
	pomodoroTechnique techniqueType = iota
	focusTechnique
)

func (t techniqueType) String() string {
	switch t {
	case pomodoroTechnique:
		return "Pomodoro"
	case focusTechnique:
		return "50/10"
	}
	return ""
}

type theme struct {
	name       string
	workColor  lipgloss.Color
	shortColor lipgloss.Color
	longColor  lipgloss.Color
}

var themes = []theme{
	{"Standard", lipgloss.Color("#FF6B6B"), lipgloss.Color("#4ECDC4"), lipgloss.Color("#45B7D1")},
	{"Nord", lipgloss.Color("#BF616A"), lipgloss.Color("#A3BE8C"), lipgloss.Color("#81A1C1")},
	{"Dracula", lipgloss.Color("#FF79C6"), lipgloss.Color("#50FA7B"), lipgloss.Color("#8BE9FD")},
	{"Gruvbox", lipgloss.Color("#FB4934"), lipgloss.Color("#B8BB26"), lipgloss.Color("#83A598")},
}

// --- Persistence ---

type settingsConfig struct {
	Technique          int  `json:"technique"`
	WorkDuration       int  `json:"work_duration"`
	ShortBreakDuration int  `json:"short_break_duration"`
	LongBreakDuration  int  `json:"long_break_duration"`
	FocusDuration      int  `json:"focus_duration"`
	BreakDuration      int  `json:"break_duration"`
	ThemeIndex         int  `json:"theme_index"`
	AutoStartOnSkip    bool `json:"auto_start_on_skip"`
	AutoStartNext      bool `json:"auto_start_next"`
}

func configPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "pomodoro", "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "pomodoro", "settings.json")
}

func loadConfig() (settingsConfig, error) {
	var c settingsConfig
	data, err := os.ReadFile(configPath())
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(data, &c)
	return c, err
}

func saveConfig(c settingsConfig) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func persistFromModel(m model) {
	_ = saveConfig(settingsConfig{
		Technique:          int(m.technique),
		WorkDuration:       m.workDuration,
		ShortBreakDuration: m.shortBreakDuration,
		LongBreakDuration:  m.longBreakDuration,
		FocusDuration:      m.focusDuration,
		BreakDuration:      m.breakDuration,
		ThemeIndex:         m.themeIndex,
		AutoStartOnSkip:    m.autoStartOnSkip,
		AutoStartNext:      m.autoStartNext,
	})
}

// --- Model ---

type model struct {
	technique techniqueType

	workDuration       int
	shortBreakDuration int
	longBreakDuration  int
	focusDuration      int
	breakDuration      int

	mode   mode
	screen screen

	selectedSetting int // 0=technique, 1=work, 2=short break, 3=long break, 4=focus, 5=break, 6=theme, 7=auto start on skip, 8=auto start next
	themeIndex      int
	autoStartOnSkip bool
	autoStartNext   bool

	remaining       time.Duration
	sessionDuration time.Duration
	running         bool
	completedCycles int

	width  int
	height int
}

func initialModel() model {
	m := model{
		technique:          pomodoroTechnique,
		workDuration:       25,
		shortBreakDuration: 5,
		longBreakDuration:  15,
		focusDuration:      50,
		breakDuration:      10,
		mode:               workMode,
		screen:             timerScreen,
		remaining:          25 * time.Minute,
		sessionDuration:    25 * time.Minute,
		themeIndex:         0,
		autoStartOnSkip:    false,
		autoStartNext:      false,
		width:              80,
		height:             24,
	}

	if c, err := loadConfig(); err == nil {
		m.technique = techniqueType(max(0, min(1, c.Technique)))
		m.workDuration = max(1, min(120, c.WorkDuration))
		m.shortBreakDuration = max(1, min(60, c.ShortBreakDuration))
		m.longBreakDuration = max(1, min(60, c.LongBreakDuration))
		m.focusDuration = max(1, min(120, c.FocusDuration))
		m.breakDuration = max(1, min(60, c.BreakDuration))
		m.themeIndex = max(0, min(len(themes)-1, c.ThemeIndex))
		m.autoStartOnSkip = c.AutoStartOnSkip
		m.autoStartNext = c.AutoStartNext
		m.remaining = m.currentModeDuration()
		m.sessionDuration = m.remaining
	}

	return m
}

// --- Messages ---

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// --- Bubble Tea Functions ---

func notify(title, body string) {
	go beeep.Notify(title, body, "")
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.screen == settingsScreen {
			return m.updateSettings(msg)
		}
		return m.updateTimer(msg)

	case tickMsg:
		if m.running && m.remaining > 0 {
			m.remaining -= time.Second
			if m.remaining < 0 {
				m.remaining = 0
			}
			if m.remaining == 0 {
				oldLabel := m.modeLabel()
				m.running = m.autoStartNext
				m = m.nextMode()

				msgText := fmt.Sprintf("%s finished.", oldLabel)
				if m.running {
					msgText += fmt.Sprintf(" %s started.", m.modeLabel())
				} else {
					msgText += fmt.Sprintf(" %s ready.", m.modeLabel())
				}
				notify("Pomodoro Timer", msgText)

				if m.running {
					return m, tickCmd()
				}
			}
			return m, tickCmd()
		}
		return m, nil
	}

	return m, nil
}

func (m model) updateTimer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case " ":
		m.running = !m.running
		if m.running {
			return m, tickCmd()
		}

	case "r":
		m.running = false
		m.remaining = m.currentModeDuration()
		m.sessionDuration = m.remaining

	case "s":
		wasRunning := m.running
		m.running = m.autoStartOnSkip
		m = m.nextMode()
		m.sessionDuration = m.remaining
		notify("Pomodoro Timer", fmt.Sprintf("Skipped to %s.", m.modeLabel()))
		if m.running && !wasRunning {
			return m, tickCmd()
		}

	case "+", "=":
		m.remaining += time.Minute
		m.sessionDuration += time.Minute

	case "-", "_":
		if m.remaining >= time.Minute {
			m.remaining -= time.Minute
			m.sessionDuration -= time.Minute
		}

	case "tab", "esc":
		m.screen = settingsScreen
	}

	return m, nil
}

func (m model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	save := false
	resetDefaults := false

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab", "esc":
		m.screen = timerScreen

	case "up", "k":
		if m.selectedSetting > 0 {
			m.selectedSetting--
		}

	case "down", "j":
		if m.selectedSetting < 8 {
			m.selectedSetting++
		}

	case "right", "l":
		save = true
		switch m.selectedSetting {
		case 0:
			m.technique = focusTechnique
			m.mode = workMode
			m.remaining = m.currentModeDuration()
			m.sessionDuration = m.remaining
			m.running = false
			m.completedCycles = 0
		case 1:
			if m.workDuration < 120 {
				m.workDuration++
			}
			if m.mode == workMode && !m.running {
				m.remaining = time.Duration(m.workDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 2:
			if m.shortBreakDuration < 60 {
				m.shortBreakDuration++
			}
			if m.mode == shortBreakMode && !m.running {
				m.remaining = time.Duration(m.shortBreakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 3:
			if m.longBreakDuration < 60 {
				m.longBreakDuration++
			}
			if m.mode == longBreakMode && !m.running {
				m.remaining = time.Duration(m.longBreakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 4:
			if m.focusDuration < 120 {
				m.focusDuration++
			}
			if m.technique == focusTechnique && m.mode == workMode && !m.running {
				m.remaining = time.Duration(m.focusDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 5:
			if m.breakDuration < 60 {
				m.breakDuration++
			}
			if m.technique == focusTechnique && m.mode == shortBreakMode && !m.running {
				m.remaining = time.Duration(m.breakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 6:
			if m.themeIndex < len(themes)-1 {
				m.themeIndex++
			}
		case 7:
			m.autoStartOnSkip = !m.autoStartOnSkip
		case 8:
			m.autoStartNext = !m.autoStartNext
		}

	case "left", "h":
		save = true
		switch m.selectedSetting {
		case 0:
			m.technique = pomodoroTechnique
			m.mode = workMode
			m.remaining = m.currentModeDuration()
			m.sessionDuration = m.remaining
			m.running = false
			m.completedCycles = 0
		case 1:
			if m.workDuration > 1 {
				m.workDuration--
			}
			if m.mode == workMode && !m.running {
				m.remaining = time.Duration(m.workDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 2:
			if m.shortBreakDuration > 1 {
				m.shortBreakDuration--
			}
			if m.mode == shortBreakMode && !m.running {
				m.remaining = time.Duration(m.shortBreakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 3:
			if m.longBreakDuration > 1 {
				m.longBreakDuration--
			}
			if m.mode == longBreakMode && !m.running {
				m.remaining = time.Duration(m.longBreakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 4:
			if m.focusDuration > 1 {
				m.focusDuration--
			}
			if m.technique == focusTechnique && m.mode == workMode && !m.running {
				m.remaining = time.Duration(m.focusDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 5:
			if m.breakDuration > 1 {
				m.breakDuration--
			}
			if m.technique == focusTechnique && m.mode == shortBreakMode && !m.running {
				m.remaining = time.Duration(m.breakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 6:
			if m.themeIndex > 0 {
				m.themeIndex--
			}
		case 7:
			m.autoStartOnSkip = !m.autoStartOnSkip
		case 8:
			m.autoStartNext = !m.autoStartNext
		}

	case " ":
		save = true
		switch m.selectedSetting {
		case 0:
			if m.technique == pomodoroTechnique {
				m.technique = focusTechnique
			} else {
				m.technique = pomodoroTechnique
			}
			m.mode = workMode
			m.remaining = m.currentModeDuration()
			m.sessionDuration = m.remaining
			m.running = false
			m.completedCycles = 0
		case 7:
			m.autoStartOnSkip = !m.autoStartOnSkip
		case 8:
			m.autoStartNext = !m.autoStartNext
		}

	case "r":
		save = true
		resetDefaults = true
	}

	if resetDefaults {
		m.technique = pomodoroTechnique
		m.workDuration = 25
		m.shortBreakDuration = 5
		m.longBreakDuration = 15
		m.focusDuration = 50
		m.breakDuration = 10
		m.themeIndex = 0
		m.autoStartOnSkip = false
		m.autoStartNext = false
		m.running = false
		m.mode = workMode
		m.completedCycles = 0
		m.remaining = m.currentModeDuration()
		m.sessionDuration = m.remaining
	}

	if save {
		persistFromModel(m)
	}

	return m, nil
}

func (m model) nextMode() model {
	if m.technique == focusTechnique {
		switch m.mode {
		case workMode:
			m.completedCycles++
			m.mode = shortBreakMode
			m.remaining = time.Duration(m.breakDuration) * time.Minute
		case shortBreakMode:
			m.mode = workMode
			m.remaining = time.Duration(m.focusDuration) * time.Minute
		}
		m.sessionDuration = m.remaining
		return m
	}

	switch m.mode {
	case workMode:
		m.completedCycles++
		if m.completedCycles%4 == 0 {
			m.mode = longBreakMode
			m.remaining = time.Duration(m.longBreakDuration) * time.Minute
		} else {
			m.mode = shortBreakMode
			m.remaining = time.Duration(m.shortBreakDuration) * time.Minute
		}
	case shortBreakMode, longBreakMode:
		m.mode = workMode
		m.remaining = time.Duration(m.workDuration) * time.Minute
	}
	m.sessionDuration = m.remaining
	return m
}

func (m model) currentModeDuration() time.Duration {
	if m.technique == focusTechnique {
		switch m.mode {
		case workMode:
			return time.Duration(m.focusDuration) * time.Minute
		case shortBreakMode:
			return time.Duration(m.breakDuration) * time.Minute
		}
		return 0
	}

	switch m.mode {
	case workMode:
		return time.Duration(m.workDuration) * time.Minute
	case shortBreakMode:
		return time.Duration(m.shortBreakDuration) * time.Minute
	case longBreakMode:
		return time.Duration(m.longBreakDuration) * time.Minute
	}
	return 0
}

func (m model) modeLabel() string {
	if m.technique == focusTechnique {
		switch m.mode {
		case workMode:
			return "Focus"
		case shortBreakMode:
			return "Break"
		}
		return ""
	}

	switch m.mode {
	case workMode:
		return "Focus"
	case shortBreakMode:
		return "Short Break"
	case longBreakMode:
		return "Long Break"
	}
	return ""
}

func (m model) modeColor() lipgloss.Color {
	t := themes[m.themeIndex]
	switch m.mode {
	case workMode:
		return t.workColor
	case shortBreakMode:
		return t.shortColor
	case longBreakMode:
		return t.longColor
	}
	return lipgloss.Color("#FFFFFF")
}

// --- View ---

func (m model) View() string {
	if m.screen == settingsScreen {
		return m.settingsView()
	}
	return m.timerView()
}

func (m model) timerView() string {
	color := m.modeColor()

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(color).
		MarginTop(1).
		MarginBottom(1).
		Align(lipgloss.Center)

	techniqueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginBottom(0).
		Align(lipgloss.Center)

	timerBoxStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(color).
		Padding(2, 10).
		MarginBottom(2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(color)

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginBottom(1).
		Align(lipgloss.Center)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555")).
		MarginTop(2).
		Align(lipgloss.Center)

	// Timer text
	minutes := int(m.remaining.Minutes())
	seconds := int(m.remaining.Seconds()) % 60
	timerText := fmt.Sprintf("%02d:%02d", minutes, seconds)

	// Status
	status := "Paused"
	if m.running {
		status = "Running"
	}

	// Progress bar
	progress := 0.0
	if m.sessionDuration > 0 {
		elapsed := m.sessionDuration - m.remaining
		progress = float64(elapsed) / float64(m.sessionDuration)
		if progress < 0 {
			progress = 0
		}
		if progress > 1 {
			progress = 1
		}
	}

	barWidth := m.width - 24
	if barWidth < 20 {
		barWidth = 20
	}
	if barWidth > 50 {
		barWidth = 50
	}
	filled := int(progress * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	pct := int(progress * 100)
	barLine := lipgloss.NewStyle().
		MarginBottom(1).
		Render(lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Foreground(color).Render(bar),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render(fmt.Sprintf(" %3d%%", pct)),
		))

	// Cycle indicators
	var cycleLine string
	if m.technique == pomodoroTechnique {
		totalSets := (m.completedCycles / 4) + 1
		currentInSet := m.completedCycles % 4
		var cycleB strings.Builder
		for i := 0; i < 4; i++ {
			if i < currentInSet {
				cycleB.WriteString(lipgloss.NewStyle().Foreground(color).Render("●"))
			} else {
				cycleB.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#444444")).Render("●"))
			}
			if i < 3 {
				cycleB.WriteString(" ")
			}
		}
		cycleLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginBottom(1).
			Render(fmt.Sprintf("Set %d   %s", totalSets, cycleB.String()))
	} else {
		var cycleB strings.Builder
		for i := 0; i < 4; i++ {
			if i < m.completedCycles%4 {
				cycleB.WriteString(lipgloss.NewStyle().Foreground(color).Render("●"))
			} else {
				cycleB.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#444444")).Render("●"))
			}
			if i < 3 {
				cycleB.WriteString(" ")
			}
		}
		cycleLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginBottom(1).
			Render(fmt.Sprintf("Cycle %d   %s", m.completedCycles+1, cycleB.String()))
	}

	techniqueLine := techniqueStyle.Render(m.technique.String())
	title := titleStyle.Render(m.modeLabel())
	timer := timerBoxStyle.Render(timerText)
	statusLine := statusStyle.Render(fmt.Sprintf("%s  •  Cycle %d", status, m.completedCycles+1))
	help := helpStyle.Render("space start/pause   +/- 1min   r reset   s skip   tab settings   q quit")

	content := lipgloss.JoinVertical(lipgloss.Center, techniqueLine, title, timer, barLine, statusLine, cycleLine, help)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m model) settingsView() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		MarginBottom(2).
		Align(lipgloss.Center)

	labelStyle := lipgloss.NewStyle().
		Width(24).
		Foreground(lipgloss.Color("#CCCCCC"))

	selLabelStyle := lipgloss.NewStyle().
		Width(24).
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA"))

	valueStyle := lipgloss.NewStyle().
		Bold(true).
		Width(12).
		Foreground(lipgloss.Color("#FAFAFA"))

	selValueStyle := lipgloss.NewStyle().
		Bold(true).
		Width(12).
		Foreground(lipgloss.Color("#4ECDC4"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		MarginTop(2)

	headingStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#888888")).
		MarginTop(1).
		MarginBottom(1).
		Align(lipgloss.Center)

	row := func(idx int, label string, val string) string {
		var l, v string
		if m.selectedSetting == idx {
			l = selLabelStyle.Render("> " + label)
			v = selValueStyle.Render(val)
		} else {
			l = labelStyle.Render("  " + label)
			v = valueStyle.Render(val)
		}
		return lipgloss.JoinHorizontal(lipgloss.Left, l, "    ", v)
	}

	autoStartStr := "Off"
	if m.autoStartOnSkip {
		autoStartStr = "On"
	}
	autoNextStr := "Off"
	if m.autoStartNext {
		autoNextStr = "On"
	}

	rows := []string{
		titleStyle.Render("Settings"),
		row(0, "Technique", m.technique.String()),
		headingStyle.Render("─ Pomodoro ─"),
		row(1, "Work Duration", fmt.Sprintf("%d min", m.workDuration)),
		row(2, "Short Break", fmt.Sprintf("%d min", m.shortBreakDuration)),
		row(3, "Long Break", fmt.Sprintf("%d min", m.longBreakDuration)),
		headingStyle.Render("─ 50/10 ─"),
		row(4, "Focus Duration", fmt.Sprintf("%d min", m.focusDuration)),
		row(5, "Break Duration", fmt.Sprintf("%d min", m.breakDuration)),
		headingStyle.Render("─ Other ─"),
		row(6, "Theme", themes[m.themeIndex].name),
		row(7, "Auto Start on Skip", autoStartStr),
		row(8, "Auto Start Next", autoNextStr),
		helpStyle.Render("↑↓/jk navigate • ←→/hl adjust • space toggle • r reset defaults • tab/esc back • q quit"),
	}

	content := lipgloss.JoinVertical(lipgloss.Center, rows...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// --- Main ---

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
