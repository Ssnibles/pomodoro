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

type preset struct {
	name          string
	workDuration  int
	shortBreak    int
	longBreak     int
}

var presets = []preset{
	{"Pomodoro (25/5)", 25, 5, 15},
	{"Focus (50/10)", 50, 10, 20},
	{"Deep Work (45/15)", 45, 15, 30},
	{"Short (15/3)", 15, 3, 10},
	{"Custom", 25, 5, 15},
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
	{"Tokyo Night", lipgloss.Color("#7AA2F7"), lipgloss.Color("#9ECE6A"), lipgloss.Color("#BB9AF7")},
	{"Catppuccin", lipgloss.Color("#F38BA8"), lipgloss.Color("#A6E3A1"), lipgloss.Color("#89B4FA")},
}

// --- Persistence ---

type settingsConfig struct {
	PresetIndex        int  `json:"preset_index"`
	WorkDuration       int  `json:"work_duration"`
	ShortBreakDuration int  `json:"short_break_duration"`
	LongBreakDuration  int  `json:"long_break_duration"`
	ThemeIndex         int  `json:"theme_index"`
	AutoStartOnSkip    bool `json:"auto_start_on_skip"`
	AutoStartNext      bool `json:"auto_start_next"`
	SoundAlert         bool `json:"sound_alert"`
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
	c.SoundAlert = true // default
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
		PresetIndex:        m.presetIndex,
		WorkDuration:       m.workDuration,
		ShortBreakDuration: m.shortBreakDuration,
		LongBreakDuration:  m.longBreakDuration,
		ThemeIndex:         m.themeIndex,
		AutoStartOnSkip:    m.autoStartOnSkip,
		AutoStartNext:      m.autoStartNext,
		SoundAlert:         m.soundAlert,
	})
}

// --- Big ASCII Digits ---

var bigDigits = map[rune][]string{
	'0': {"█▀▀█", "█  █", "█▄▄█"},
	'1': {" █  ", " █  ", " █  "},
	'2': {"█▀▀█", "  ▄▀", "█▄▄▄"},
	'3': {"█▀▀█", " ▀▀█", "█▄▄█"},
	'4': {"█  █", "█▄▄█", "   █"},
	'5': {"█▀▀▀", "▀▀▀█", "█▄▄█"},
	'6': {"█▀▀▀", "█▀▀█", "█▄▄█"},
	'7': {"▀▀▀█", "  █ ", " █  "},
	'8': {"█▀▀█", "█▀▀█", "█▄▄█"},
	'9': {"█▀▀█", "▀▀▀█", "█▄▄█"},
	':': {" ▄ ", "   ", " ▀ "},
}

func renderBigTimer(timerStr string, style lipgloss.Style) string {
	lines := []string{"", "", ""}
	for idx, ch := range timerStr {
		glyph, ok := bigDigits[ch]
		if !ok {
			glyph = []string{"   ", "   ", "   "}
		}
		for i := 0; i < 3; i++ {
			lines[i] += glyph[i]
			if idx < len(timerStr)-1 {
				lines[i] += " "
			}
		}
	}
	return style.Render(strings.Join(lines, "\n"))
}

// --- Model ---

type model struct {
	presetIndex        int
	workDuration       int
	shortBreakDuration int
	longBreakDuration  int

	mode   mode
	screen screen

	selectedSetting int // 0=preset, 1=work, 2=short break, 3=long break, 4=theme, 5=auto skip, 6=auto next, 7=sound alert
	themeIndex      int
	autoStartOnSkip bool
	autoStartNext   bool
	soundAlert      bool

	remaining       time.Duration
	sessionDuration time.Duration
	running         bool
	completedCycles int
	totalFocusTime  time.Duration

	width  int
	height int
}

func initialModel() model {
	m := model{
		presetIndex:        0,
		workDuration:       25,
		shortBreakDuration: 5,
		longBreakDuration:  15,
		mode:               workMode,
		screen:             timerScreen,
		remaining:          25 * time.Minute,
		sessionDuration:    25 * time.Minute,
		themeIndex:         0,
		autoStartOnSkip:    false,
		autoStartNext:      false,
		soundAlert:         true,
		width:              80,
		height:             24,
	}

	if c, err := loadConfig(); err == nil {
		m.presetIndex = max(0, min(len(presets)-1, c.PresetIndex))
		m.workDuration = max(1, min(120, c.WorkDuration))
		m.shortBreakDuration = max(1, min(60, c.ShortBreakDuration))
		m.longBreakDuration = max(1, min(60, c.LongBreakDuration))
		m.themeIndex = max(0, min(len(themes)-1, c.ThemeIndex))
		m.autoStartOnSkip = c.AutoStartOnSkip
		m.autoStartNext = c.AutoStartNext
		m.soundAlert = c.SoundAlert
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

// --- Helper Functions ---

func notify(title, body string, sound bool) {
	if sound {
		fmt.Print("\a")
	}
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
			if m.mode == workMode {
				m.totalFocusTime += time.Second
			}
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
				notify("Pomodoro Timer", msgText, m.soundAlert)

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

func (m *model) applyPreset(idx int) {
	m.presetIndex = idx
	p := presets[idx]
	if p.name != "Custom" {
		m.workDuration = p.workDuration
		m.shortBreakDuration = p.shortBreak
		m.longBreakDuration = p.longBreak
		if !m.running {
			m.remaining = m.currentModeDuration()
			m.sessionDuration = m.remaining
		}
	}
}

func (m *model) matchPreset() {
	for i, p := range presets {
		if p.name != "Custom" && p.workDuration == m.workDuration && p.shortBreak == m.shortBreakDuration && p.longBreak == m.longBreakDuration {
			m.presetIndex = i
			return
		}
	}
	m.presetIndex = len(presets) - 1 // Custom
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
		notify("Pomodoro Timer", fmt.Sprintf("Skipped to %s.", m.modeLabel()), false)
		if m.running && !wasRunning {
			return m, tickCmd()
		}

	case "p":
		m.presetIndex = (m.presetIndex + 1) % len(presets)
		m.applyPreset(m.presetIndex)
		persistFromModel(m)

	case "+", "=":
		m.remaining += time.Minute
		m.sessionDuration += time.Minute

	case "-", "_":
		if m.remaining >= time.Minute {
			m.remaining -= time.Minute
			m.sessionDuration -= time.Minute
		}

	case "tab", "esc", ",":
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

	case "tab", "esc", ",":
		m.screen = timerScreen

	case "up", "k":
		if m.selectedSetting > 0 {
			m.selectedSetting--
		}

	case "down", "j":
		if m.selectedSetting < 7 {
			m.selectedSetting++
		}

	case "right", "l":
		save = true
		switch m.selectedSetting {
		case 0:
			m.presetIndex = (m.presetIndex + 1) % len(presets)
			m.applyPreset(m.presetIndex)
		case 1:
			if m.workDuration < 120 {
				m.workDuration++
				m.matchPreset()
			}
			if m.mode == workMode && !m.running {
				m.remaining = time.Duration(m.workDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 2:
			if m.shortBreakDuration < 60 {
				m.shortBreakDuration++
				m.matchPreset()
			}
			if m.mode == shortBreakMode && !m.running {
				m.remaining = time.Duration(m.shortBreakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 3:
			if m.longBreakDuration < 60 {
				m.longBreakDuration++
				m.matchPreset()
			}
			if m.mode == longBreakMode && !m.running {
				m.remaining = time.Duration(m.longBreakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 4:
			if m.themeIndex < len(themes)-1 {
				m.themeIndex++
			}
		case 5:
			m.autoStartOnSkip = !m.autoStartOnSkip
		case 6:
			m.autoStartNext = !m.autoStartNext
		case 7:
			m.soundAlert = !m.soundAlert
		}

	case "left", "h":
		save = true
		switch m.selectedSetting {
		case 0:
			if m.presetIndex > 0 {
				m.presetIndex--
			} else {
				m.presetIndex = len(presets) - 1
			}
			m.applyPreset(m.presetIndex)
		case 1:
			if m.workDuration > 1 {
				m.workDuration--
				m.matchPreset()
			}
			if m.mode == workMode && !m.running {
				m.remaining = time.Duration(m.workDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 2:
			if m.shortBreakDuration > 1 {
				m.shortBreakDuration--
				m.matchPreset()
			}
			if m.mode == shortBreakMode && !m.running {
				m.remaining = time.Duration(m.shortBreakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 3:
			if m.longBreakDuration > 1 {
				m.longBreakDuration--
				m.matchPreset()
			}
			if m.mode == longBreakMode && !m.running {
				m.remaining = time.Duration(m.longBreakDuration) * time.Minute
				m.sessionDuration = m.remaining
			}
		case 4:
			if m.themeIndex > 0 {
				m.themeIndex--
			}
		case 5:
			m.autoStartOnSkip = !m.autoStartOnSkip
		case 6:
			m.autoStartNext = !m.autoStartNext
		case 7:
			m.soundAlert = !m.soundAlert
		}

	case " ":
		save = true
		switch m.selectedSetting {
		case 0:
			m.presetIndex = (m.presetIndex + 1) % len(presets)
			m.applyPreset(m.presetIndex)
		case 5:
			m.autoStartOnSkip = !m.autoStartOnSkip
		case 6:
			m.autoStartNext = !m.autoStartNext
		case 7:
			m.soundAlert = !m.soundAlert
		}

	case "r":
		save = true
		resetDefaults = true
	}

	if resetDefaults {
		m.presetIndex = 0
		m.workDuration = 25
		m.shortBreakDuration = 5
		m.longBreakDuration = 15
		m.themeIndex = 0
		m.autoStartOnSkip = false
		m.autoStartNext = false
		m.soundAlert = true
		m.running = false
		m.mode = workMode
		m.completedCycles = 0
		m.totalFocusTime = 0
		m.remaining = m.currentModeDuration()
		m.sessionDuration = m.remaining
	}

	if save {
		persistFromModel(m)
	}

	return m, nil
}

func (m model) nextMode() model {
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
	switch m.mode {
	case workMode:
		return "FOCUS"
	case shortBreakMode:
		return "SHORT BREAK"
	case longBreakMode:
		return "LONG BREAK"
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

	// Title & Mode Banner
	modeBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#1A1B26")).
		Background(color).
		Padding(0, 3).
		MarginBottom(1).
		Render(m.modeLabel())

	presetStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginBottom(1)

	// Timer text
	minutes := int(m.remaining.Minutes())
	seconds := int(m.remaining.Seconds()) % 60
	timerText := fmt.Sprintf("%02d:%02d", minutes, seconds)

	var timerView string
	if m.height >= 18 {
		timerView = renderBigTimer(timerText, lipgloss.NewStyle().Bold(true).Foreground(color).MarginBottom(1))
	} else {
		timerBoxStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(color).
			Padding(1, 6).
			MarginBottom(1).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(color)
		timerView = timerBoxStyle.Render(timerText)
	}

	// Progress calculation
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
	pct := int(progress * 100)

	// Status badge with percentage
	var statusBadge string
	if m.running {
		statusBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#50FA7B")).
			MarginBottom(1).
			Render(fmt.Sprintf("▶ RUNNING   •   %d%%", pct))
	} else {
		statusBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFB86C")).
			MarginBottom(1).
			Render(fmt.Sprintf("⏸ PAUSED   •   %d%%", pct))
	}

	// Centered Progress bar
	barWidth := 40
	if m.width > 0 && m.width-20 < barWidth {
		barWidth = max(16, m.width-20)
	}

	filledLen := int(progress * float64(barWidth))
	if filledLen > barWidth {
		filledLen = barWidth
	}
	emptyLen := barWidth - filledLen
	bar := strings.Repeat("█", filledLen) + strings.Repeat("░", emptyLen)

	barLine := lipgloss.NewStyle().
		Foreground(color).
		MarginBottom(1).
		Render(bar)

	// Cycle indicators
	totalSets := (m.completedCycles / 4) + 1
	currentInSet := m.completedCycles % 4
	var cycleB strings.Builder
	for i := 0; i < 4; i++ {
		if i < currentInSet {
			cycleB.WriteString(lipgloss.NewStyle().Foreground(color).Render("●"))
		} else {
			cycleB.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#444444")).Render("○"))
		}
		if i < 3 {
			cycleB.WriteString(" ")
		}
	}

	totalMinutes := int(m.totalFocusTime.Minutes())
	hours := totalMinutes / 60
	mins := totalMinutes % 60
	var focusStr string
	if hours > 0 {
		focusStr = fmt.Sprintf("%dh %dm", hours, mins)
	} else {
		focusStr = fmt.Sprintf("%dm", mins)
	}

	cycleLine := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AAAAAA")).
		MarginBottom(0).
		Render(fmt.Sprintf("Set %d   %s", totalSets, cycleB.String()))

	statsLine := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#777777")).
		MarginBottom(1).
		Render(fmt.Sprintf("Focus Today: %s  •  Completed: %d", focusStr, m.completedCycles))

	// Help Bar
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555")).
		MarginTop(1).
		Align(lipgloss.Center)
	help := helpStyle.Render("space toggle   s skip   r reset   p preset   +/- adjust   tab settings   q quit")

	presetLine := presetStyle.Render(fmt.Sprintf("Preset: %s", presets[m.presetIndex].name))

	content := lipgloss.JoinVertical(lipgloss.Center, modeBadge, presetLine, timerView, statusBadge, barLine, cycleLine, statsLine, help)
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
		Width(20).
		Foreground(lipgloss.Color("#FAFAFA"))

	selValueStyle := lipgloss.NewStyle().
		Bold(true).
		Width(20).
		Foreground(lipgloss.Color("#4ECDC4"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		MarginTop(2)

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
	soundStr := "Off"
	if m.soundAlert {
		soundStr = "On"
	}

	rows := []string{
		titleStyle.Render("Settings"),
		row(0, "Preset", presets[m.presetIndex].name),
		row(1, "Work Duration", fmt.Sprintf("%d min", m.workDuration)),
		row(2, "Short Break", fmt.Sprintf("%d min", m.shortBreakDuration)),
		row(3, "Long Break", fmt.Sprintf("%d min", m.longBreakDuration)),
		row(4, "Theme", themes[m.themeIndex].name),
		row(5, "Auto Start Break", autoStartStr),
		row(6, "Auto Start Work", autoNextStr),
		row(7, "Terminal Sound", soundStr),
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
