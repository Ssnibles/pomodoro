// Package main provides the entry point, user interface architecture, timer state machine,
// settings persistence, automation hooks, live state export, and main event loop for the Pomodoro CLI application.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gen2brain/beeep"
)

// --- Types ---

// mode represents the operational phase of the Pomodoro timer (Work, Short Break, or Long Break).
type mode int

const (
	workMode mode = iota
	shortBreakMode
	longBreakMode
)

// screen represents the currently active view screen in the terminal application.
type screen int

const (
	timerScreen screen = iota
	tasksScreen
	statsScreen
	settingsScreen
)

// preset defines a pre-configured timer duration template for focus and break sessions.
type preset struct {
	name          string
	workDuration  int
	shortBreak    int
	longBreak     int
}

// presets contains the standard predefined duration configurations available to the user.
var presets = []preset{
	{"Pomodoro (25/5)", 25, 5, 15},
	{"Focus (50/10)", 50, 10, 20},
	{"Deep Work (45/15)", 45, 15, 30},
	{"Short (15/3)", 15, 3, 10},
	{"Custom", 25, 5, 15},
}

// theme defines the visual colour palette for timer states across the application.
type theme struct {
	name       string
	workColor  lipgloss.Color
	shortColor lipgloss.Color
	longColor  lipgloss.Color
}

// themes contains the curated colour themes available for customising the interface aesthetics.
var themes = []theme{
	{"Standard", lipgloss.Color("#FF6B6B"), lipgloss.Color("#4ECDC4"), lipgloss.Color("#45B7D1")},
	{"Nord", lipgloss.Color("#BF616A"), lipgloss.Color("#A3BE8C"), lipgloss.Color("#81A1C1")},
	{"Dracula", lipgloss.Color("#FF79C6"), lipgloss.Color("#50FA7B"), lipgloss.Color("#8BE9FD")},
	{"Gruvbox", lipgloss.Color("#FB4934"), lipgloss.Color("#B8BB26"), lipgloss.Color("#83A598")},
	{"Tokyo Night", lipgloss.Color("#7AA2F7"), lipgloss.Color("#9ECE6A"), lipgloss.Color("#BB9AF7")},
	{"Catppuccin", lipgloss.Color("#F38BA8"), lipgloss.Color("#A6E3A1"), lipgloss.Color("#89B4FA")},
}

// bigDigits contains 3-line high ASCII representations for numbers 0-9 and colon.
var bigDigits = map[rune][]string{
	'0': {"█▀█", "█ █", "▀▀▀"},
	'1': {" █ ", " █ ", " ▀ "},
	'2': {"▀▀█", "█▀▀", "▀▀▀"},
	'3': {"▀▀█", " ▀█", "▀▀▀"},
	'4': {"█ █", "▀▀█", "  ▀"},
	'5': {"█▀▀", "▀▀█", "▀▀▀"},
	'6': {"█▀▀", "█▀█", "▀▀▀"},
	'7': {"▀▀█", "  █", "  ▀"},
	'8': {"█▀█", "█▀█", "▀▀▀"},
	'9': {"█▀█", "▀▀█", "▀▀▀"},
	':': {"   ", " ▀ ", " ▀ "},
}

// renderBigTimer constructs a 3-line ASCII block digital clock display.
func renderBigTimer(timerText string, style lipgloss.Style) string {
	line1 := ""
	line2 := ""
	line3 := ""

	for _, ch := range timerText {
		if art, ok := bigDigits[ch]; ok {
			line1 += art[0] + " "
			line2 += art[1] + " "
			line3 += art[2] + " "
		} else {
			line1 += "   "
			line2 += "   "
			line3 += "   "
		}
	}

	block := strings.TrimRight(line1, " ") + "\n" +
		strings.TrimRight(line2, " ") + "\n" +
		strings.TrimRight(line3, " ")

	return style.Render(block)
}

// --- Keybind Hints Renderer ---

// keyHint represents a keyboard shortcut hint entry displayed in the application footer.
type keyHint struct {
	key   string
	label string
}

// renderKeyHints formats and renders keybinding hints for display in the footer bar.
func renderKeyHints(hints []keyHint) string {
	var items []string
	for _, h := range hints {
		k := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#DDDDDD")).
			Background(lipgloss.Color("#252636")).
			Padding(0, 1).
			Render(h.key)
		l := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666677")).
			Render(" " + h.label)
		items = append(items, lipgloss.JoinHorizontal(lipgloss.Top, k, l))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(items, "   "))
}

// --- Persistence & State Export ---

// writeJSONAtomic serialises the given data structure to JSON and writes it
// atomically to the target file path using a temporary file to prevent corruption.
func writeJSONAtomic(path string, v interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON data: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-save-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary save file in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()

	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := tmpFile.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("failed to write data to temp file %s: %w", tmpPath, err)
	}

	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("failed to sync temp file %s: %w", tmpPath, err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to atomically rename temp file %s to %s: %w", tmpPath, path, err)
	}

	return nil
}

// configDir determines the configuration directory path based on environment variables
// or defaults to the standard XDG configuration directory.
func configDir() string {
	if dir := os.Getenv("POMODORO_CONFIG_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "pomodoro")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".config", "pomodoro")
	}
	return filepath.Join(home, ".config", "pomodoro")
}

// configPath returns the absolute file path to the user settings JSON file.
func configPath() string {
	return filepath.Join(configDir(), "settings.json")
}

// stateExportPath returns the file path to the live status JSON export file for Neovim/tmux statuslines.
func stateExportPath() string {
	return filepath.Join(configDir(), "state.json")
}

// ExportState represents the serialised live state exported to state.json for statuslines.
type ExportState struct {
	Mode         string `json:"mode"`
	ModeColor    string `json:"mode_color"`
	Remaining    string `json:"remaining"`
	SecondsLeft  int    `json:"seconds_left"`
	Running      bool   `json:"running"`
	ActiveTask   string `json:"active_task"`
	TaskCategory string `json:"task_category"`
	FocusToday   string `json:"focus_today"`
	GoalMins     int    `json:"goal_mins"`
	TodayMins    int    `json:"today_mins"`
}

// loadExportState reads the current state export from disk.
func loadExportState() (ExportState, error) {
	var s ExportState
	data, err := os.ReadFile(stateExportPath())
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}

// exportStateToFile writes the live model status to state.json for statusline integrations.
func exportStateToFile(m model) {
	minutes := int(m.remaining.Minutes())
	seconds := int(m.remaining.Seconds()) % 60
	timerText := fmt.Sprintf("%02d:%02d", minutes, seconds)

	activeTaskTitle := ""
	activeTaskCat := ""
	if activeTask := m.taskModel.store.ActiveTask(); activeTask != nil {
		activeTaskTitle = activeTask.Title
		activeTaskCat = activeTask.Category
	}

	store, _ := loadHistory()
	today := time.Now().Format("2006-01-02")
	todayMins := 0
	if rec, ok := store.DailyRecords[today]; ok && rec != nil {
		todayMins = rec.Minutes
	}

	state := ExportState{
		Mode:         m.modeLabel(),
		ModeColor:    fmt.Sprintf("%v", m.modeColor()),
		Remaining:    timerText,
		SecondsLeft:  int(m.remaining.Seconds()),
		Running:      m.running,
		ActiveTask:   activeTaskTitle,
		TaskCategory: activeTaskCat,
		FocusToday:   fmt.Sprintf("%dh %dm", todayMins/60, todayMins%60),
		GoalMins:     m.dailyGoalMinutes,
		TodayMins:    todayMins,
	}

	_ = writeJSONAtomic(stateExportPath(), state)
}

// settingsConfig represents the serialisable user preferences stored on disk.
type settingsConfig struct {
	PresetIndex        int    `json:"preset_index"`
	WorkDuration       int    `json:"work_duration"`
	ShortBreakDuration int    `json:"short_break_duration"`
	LongBreakDuration  int    `json:"long_break_duration"`
	ThemeIndex         int    `json:"theme_index"`
	AutoStartOnSkip    bool   `json:"auto_start_on_skip"`
	AutoStartNext      bool   `json:"auto_start_next"`
	SoundAlert         bool   `json:"sound_alert"`
	DailyGoalMinutes   int    `json:"daily_goal_minutes"`
	OnWorkStartCmd     string `json:"on_work_start_cmd"`
	OnBreakStartCmd    string `json:"on_break_start_cmd"`
	OnCompleteCmd      string `json:"on_complete_cmd"`
}

// loadConfig reads and deserialises settings from the disk configuration file.
func loadConfig() (settingsConfig, error) {
	var c settingsConfig
	c.SoundAlert = true      // Default setting
	c.DailyGoalMinutes = 240 // Default 4 hours goal

	data, err := os.ReadFile(configPath())
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(data, &c)
	if c.DailyGoalMinutes <= 0 {
		c.DailyGoalMinutes = 240
	}
	return c, err
}

// saveConfig serialises and atomically writes user settings configuration to disk.
func saveConfig(c settingsConfig) error {
	return writeJSONAtomic(configPath(), c)
}

// persistFromModel extracts settings state from the active model and saves it to disk.
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
		DailyGoalMinutes:   m.dailyGoalMinutes,
		OnWorkStartCmd:     m.onWorkStartCmd,
		OnBreakStartCmd:    m.onBreakStartCmd,
		OnCompleteCmd:      m.onCompleteCmd,
	})
}

// runShellHook executes an external shell command asynchronously in the background.
func runShellHook(cmdStr string) {
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
			}
		}()
		cmd := exec.Command("sh", "-c", cmdStr)
		_ = cmd.Run()
	}()
}

// --- Application Model ---

// model represents the root application state model for Bubble Tea.
type model struct {
	width                int
	height               int
	mode                 mode
	screen               screen
	presetIndex          int
	workDuration         int // In minutes
	shortBreakDuration   int // In minutes
	longBreakDuration    int // In minutes
	themeIndex           int
	autoStartOnSkip      bool
	autoStartNext        bool
	soundAlert           bool
	dailyGoalMinutes     int
	onWorkStartCmd       string
	onBreakStartCmd      string
	onCompleteCmd        string
	remaining            time.Duration
	sessionDuration      time.Duration
	running              bool
	completedCycles      int
	currentInterruptions int
	selectedSetting      int
	taskModel            taskModel
	presetToast          string
	presetToastID        int
}

// initialModel constructs and initialises the root application model with user preferences and defaults.
func initialModel() model {
	tm := initialTaskModel()

	m := model{
		mode:                 workMode,
		screen:               timerScreen,
		presetIndex:          0,
		workDuration:         25,
		shortBreakDuration:   5,
		longBreakDuration:    15,
		themeIndex:           0,
		autoStartOnSkip:      false,
		autoStartNext:        false,
		soundAlert:           true,
		dailyGoalMinutes:     240,
		remaining:            25 * time.Minute,
		sessionDuration:      25 * time.Minute,
		running:              false,
		completedCycles:      0,
		currentInterruptions: 0,
		selectedSetting:      0,
		taskModel:            tm,
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
		m.dailyGoalMinutes = max(30, min(1440, c.DailyGoalMinutes))
		m.onWorkStartCmd = c.OnWorkStartCmd
		m.onBreakStartCmd = c.OnBreakStartCmd
		m.onCompleteCmd = c.OnCompleteCmd
		m.remaining = m.currentModeDuration()
		m.sessionDuration = m.remaining
	}

	exportStateToFile(m)
	return m
}

// --- Messages ---

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type clearPresetToastMsg struct{ id int }

func clearPresetToastCmd(id int) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return clearPresetToastMsg{id: id}
	})
}

func (m *model) triggerPresetToast() tea.Cmd {
	m.presetToastID++
	p := presets[m.presetIndex]
	if p.name == "Custom" {
		m.presetToast = fmt.Sprintf("Preset: Custom (%dm/%dm)", m.workDuration, m.shortBreakDuration)
	} else {
		m.presetToast = fmt.Sprintf("Preset: %s (%dm/%dm)", p.name, p.workDuration, p.shortBreak)
	}
	return clearPresetToastCmd(m.presetToastID)
}

// --- Helper Functions ---

func notify(title, body string, sound bool) {
	if sound {
		fmt.Print("\a")
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
			}
		}()
		_ = beeep.Notify(title, body, "")
	}()
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
		if !m.taskModel.addingTask {
			switch msg.String() {
			case "1":
				m.screen = timerScreen
				return m, nil
			case "2":
				m.screen = tasksScreen
				return m, nil
			case "3":
				m.screen = statsScreen
				return m, nil
			case "4":
				m.screen = settingsScreen
				return m, nil
			case "tab":
				m.screen = (m.screen + 1) % 4
				return m, nil
			case "shift+tab":
				m.screen = (m.screen - 1 + 4) % 4
				return m, nil
			}
		}

		switch m.screen {
		case timerScreen:
			return m.updateTimer(msg)
		case tasksScreen:
			var cmd tea.Cmd
			m.taskModel, cmd = m.taskModel.update(msg)
			exportStateToFile(m)
			return m, cmd
		case statsScreen:
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		case settingsScreen:
			return m.updateSettings(msg)
		}

	case clearPresetToastMsg:
		if msg.id == m.presetToastID {
			m.presetToast = ""
		}
		return m, nil

	case tickMsg:
		if m.running && m.remaining > 0 {
			m.remaining -= time.Second
			if m.remaining < 0 {
				m.remaining = 0
			}

			exportStateToFile(m)

			if m.remaining == 0 {
				oldLabel := m.modeLabel()

				if m.mode == workMode {
					cat := "general"
					taskTitle := ""
					if activeTask := m.taskModel.store.ActiveTask(); activeTask != nil {
						taskTitle = activeTask.Title
						if activeTask.Category != "" {
							cat = activeTask.Category
						}
					}
					recordFocusSession(m.workDuration, cat, taskTitle, m.currentInterruptions, "")
					m.taskModel.store.IncrementActivePomodoro()
					m.currentInterruptions = 0
					runShellHook(m.onCompleteCmd)
				}

				m.running = m.autoStartNext
				m = m.nextMode()

				if m.mode == workMode {
					runShellHook(m.onWorkStartCmd)
				} else {
					runShellHook(m.onBreakStartCmd)
				}

				msgText := fmt.Sprintf("%s finished.", oldLabel)
				if m.running {
					msgText += fmt.Sprintf(" %s started.", m.modeLabel())
				} else {
					msgText += fmt.Sprintf(" %s ready.", m.modeLabel())
				}
				notify("Pomodoro Timer", msgText, m.soundAlert)

				exportStateToFile(m)

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
	idx = max(0, min(len(presets)-1, idx))
	m.presetIndex = idx
	p := presets[idx]
	if p.name != "Custom" {
		m.workDuration = p.workDuration
		m.shortBreakDuration = p.shortBreak
		m.longBreakDuration = p.longBreak
	}
	if !m.running {
		m.remaining = m.currentModeDuration()
		m.sessionDuration = m.remaining
	}
	exportStateToFile(*m)
}

func (m *model) matchPreset() {
	for i, p := range presets {
		if p.name != "Custom" && p.workDuration == m.workDuration && p.shortBreak == m.shortBreakDuration && p.longBreak == m.longBreakDuration {
			m.presetIndex = i
			return
		}
	}
	m.presetIndex = len(presets) - 1
}

func (m model) updateTimer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case " ":
		m.running = !m.running
		exportStateToFile(m)
		if m.running {
			if m.mode == workMode {
				runShellHook(m.onWorkStartCmd)
			} else {
				runShellHook(m.onBreakStartCmd)
			}
			return m, tickCmd()
		}

	case "i":
		if m.running {
			m.currentInterruptions++
			exportStateToFile(m)
		}

	case "s":
		if m.running {
			m.running = m.autoStartOnSkip
		}
		m = m.nextMode()
		exportStateToFile(m)
		if m.running {
			return m, tickCmd()
		}

	case "r":
		m.running = false
		m.remaining = m.currentModeDuration()
		m.sessionDuration = m.remaining
		m.currentInterruptions = 0
		exportStateToFile(m)

	case "p":
		m.presetIndex = (m.presetIndex + 1) % len(presets)
		m.applyPreset(m.presetIndex)
		cmd := m.triggerPresetToast()
		return m, cmd

	case "+", "=":
		if m.mode == workMode && m.workDuration < 120 {
			m.workDuration++
		} else if m.mode == shortBreakMode && m.shortBreakDuration < 60 {
			m.shortBreakDuration++
		} else if m.mode == longBreakMode && m.longBreakDuration < 60 {
			m.longBreakDuration++
		}
		m.matchPreset()
		if !m.running {
			m.remaining = m.currentModeDuration()
			m.sessionDuration = m.remaining
		}
		exportStateToFile(m)
		cmd := m.triggerPresetToast()
		return m, cmd

	case "-", "_":
		if m.mode == workMode && m.workDuration > 1 {
			m.workDuration--
		} else if m.mode == shortBreakMode && m.shortBreakDuration > 1 {
			m.shortBreakDuration--
		} else if m.mode == longBreakMode && m.longBreakDuration > 1 {
			m.longBreakDuration--
		}
		m.matchPreset()
		if !m.running {
			m.remaining = m.currentModeDuration()
			m.sessionDuration = m.remaining
		}
		exportStateToFile(m)
		cmd := m.triggerPresetToast()
		return m, cmd
	}

	return m, nil
}

func (m model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	save := false
	resetDefaults := false

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc", ",":
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
		case 8:
			if m.dailyGoalMinutes < 1440 {
				m.dailyGoalMinutes += 30
			}
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
		case 8:
			if m.dailyGoalMinutes > 30 {
				m.dailyGoalMinutes -= 30
			}
		}

	case "space":
		save = true
		switch m.selectedSetting {
		case 5:
			m.autoStartOnSkip = !m.autoStartOnSkip
		case 6:
			m.autoStartNext = !m.autoStartNext
		case 7:
			m.soundAlert = !m.soundAlert
		}

	case "r":
		resetDefaults = true
	}

	if resetDefaults {
		save = true
		m.presetIndex = 0
		m.workDuration = 25
		m.shortBreakDuration = 5
		m.longBreakDuration = 15
		m.themeIndex = 0
		m.autoStartOnSkip = false
		m.autoStartNext = false
		m.soundAlert = true
		m.dailyGoalMinutes = 240
		m.running = false
		m.mode = workMode
		m.completedCycles = 0
		m.currentInterruptions = 0
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
	if m.themeIndex < 0 || m.themeIndex >= len(themes) {
		m.themeIndex = 0
	}
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

// --- Header, Footer & Router ---

func (m model) headerView() string {
	color := m.modeColor()
	tabs := []string{" 1 Timer ", " 2 Tasks ", " 3 Stats ", " 4 Settings "}
	var renderedTabs []string

	for i, t := range tabs {
		if int(m.screen) == i {
			renderedTabs = append(renderedTabs, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#1A1B26")).
				Background(color).
				Render(t))
		} else {
			renderedTabs = append(renderedTabs, lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9999AA")).
				Background(lipgloss.Color("#242533")).
				Render(t))
		}
	}

	tabBar := lipgloss.JoinHorizontal(lipgloss.Top,
		renderedTabs[0], " ", renderedTabs[1], " ", renderedTabs[2], " ", renderedTabs[3],
	)

	if m.screen == timerScreen {
		store, _ := loadHistory()
		today := time.Now().Format("2006-01-02")
		todayMins := 0
		if rec, ok := store.DailyRecords[today]; ok && rec != nil {
			todayMins = rec.Minutes
		}

		goalPct := 0
		if m.dailyGoalMinutes > 0 {
			goalPct = (todayMins * 100) / m.dailyGoalMinutes
			if goalPct > 100 {
				goalPct = 100
			}
		}

		goalMinsLeft := m.dailyGoalMinutes - todayMins
		var goalStatusStr string
		if goalMinsLeft <= 0 {
			goalStatusStr = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#50FA7B")).
				Bold(true).
				MarginTop(1).
				Render(fmt.Sprintf("🎯 Daily Goal: %dh %dm / %dh %dm (Goal Reached! 🎉)",
					todayMins/60, todayMins%60, m.dailyGoalMinutes/60, m.dailyGoalMinutes%60))
		} else {
			goalStatusStr = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888899")).
				MarginTop(1).
				Render(fmt.Sprintf("🎯 Daily Goal: %dh %dm / %dh %dm (%dh %dm left • %d%%)",
					todayMins/60, todayMins%60, m.dailyGoalMinutes/60, m.dailyGoalMinutes%60,
					goalMinsLeft/60, goalMinsLeft%60, goalPct))
		}

		headerContent := lipgloss.JoinVertical(lipgloss.Center, tabBar, goalStatusStr)
		return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, lipgloss.NewStyle().MarginBottom(1).Render(headerContent))
	}

	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, lipgloss.NewStyle().MarginBottom(1).Render(tabBar))
}

func (m model) footerView() string {
	var hints []keyHint

	switch m.screen {
	case timerScreen:
		hints = []keyHint{
			{"space", "toggle"},
			{"i", "distraction"},
			{"s", "skip"},
			{"r", "reset"},
			{"p", "preset"},
			{"+/-", "adjust"},
			{"tab", "switch"},
			{"q", "quit"},
		}
	case tasksScreen:
		if m.taskModel.addingTask {
			hints = []keyHint{
				{"enter", "save"},
				{"esc", "cancel"},
			}
		} else {
			hints = []keyHint{
				{"↑↓/jk", "navigate"},
				{"Shift+↑↓", "reorder"},
				{"space", "active"},
				{"p", "priority"},
				{"x", "done"},
				{"a", "add"},
				{"d", "delete"},
				{"tab", "switch"},
				{"q", "quit"},
			}
		}
	case statsScreen:
		hints = []keyHint{
			{"1-4", "jump tab"},
			{"tab/shift+tab", "switch tab"},
			{"q", "quit"},
		}
	case settingsScreen:
		hints = []keyHint{
			{"↑↓/jk", "navigate"},
			{"←→/hl", "adjust"},
			{"space", "toggle"},
			{"r", "defaults"},
			{"tab", "switch"},
			{"q", "quit"},
		}
	}

	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, renderKeyHints(hints))
}

func (m model) View() string {
	header := m.headerView()
	footer := m.footerView()

	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	bodyHeight := m.height - headerH - footerH
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	switch m.screen {
	case timerScreen:
		body = m.timerView(bodyHeight)
	case tasksScreen:
		body = m.taskModel.view(m.width, bodyHeight, m.modeColor())
	case statsScreen:
		body = renderStatsView(m.width, bodyHeight, m.modeColor(), m.dailyGoalMinutes)
	case settingsScreen:
		body = m.settingsView(bodyHeight)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m model) timerView(bodyHeight int) string {
	color := m.modeColor()

	modeBadge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#1A1B26")).
		Background(color).
		Padding(0, 3).
		MarginBottom(1).
		Render(m.modeLabel())

	var activeTaskLine string
	if activeTask := m.taskModel.store.ActiveTask(); activeTask != nil {
		catStr := ""
		if activeTask.Category != "" {
			catStr = fmt.Sprintf(" [+%s]", activeTask.Category)
		}
		activeTaskLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFB86C")).
			Bold(true).
			MarginBottom(1).
			Render(fmt.Sprintf("⚡ Working on: %s%s (🍅 %d/%d)", activeTask.Title, catStr, activeTask.Pomodoros, activeTask.Target))
	}

	minutes := int(m.remaining.Minutes())
	seconds := int(m.remaining.Seconds()) % 60
	timerText := fmt.Sprintf("%02d:%02d", minutes, seconds)

	var timerView string
	if m.height >= 16 {
		timerView = renderBigTimer(timerText, lipgloss.NewStyle().Bold(true).Foreground(color).MarginBottom(1))
	} else {
		timerBoxStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(color).
			Padding(1, 6).
			MarginBottom(1)
		timerView = timerBoxStyle.Render(timerText)
	}

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

	barWidth := 36
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
		MarginBottom(1).
		Render(lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Foreground(lipgloss.Color("#555566")).Render("▐"),
			lipgloss.NewStyle().Foreground(color).Render(bar),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#555566")).Render("▌"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#888899")).Render(fmt.Sprintf(" %d%%", pct)),
		))

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

	var stateStr string
	if m.running {
		stateStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Bold(true).Render("▶ RUNNING")
	} else {
		stateStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Bold(true).Render("⏸ PAUSED")
	}

	if m.currentInterruptions > 0 {
		stateStr += lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B")).Render(fmt.Sprintf("  •  ⚡ %d", m.currentInterruptions))
	}

	statusLine := lipgloss.NewStyle().
		MarginBottom(0).
		Render(fmt.Sprintf("Set %d   %s     %s", totalSets, cycleB.String(), stateStr))

	var toastLine string
	if m.presetToast != "" {
		toastLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ECDC4")).
			Bold(true).
			MarginBottom(1).
			Render(fmt.Sprintf("⚡ %s", m.presetToast))
	}

	var elements []string
	elements = append(elements, modeBadge)
	if toastLine != "" {
		elements = append(elements, toastLine)
	}
	if activeTaskLine != "" {
		elements = append(elements, activeTaskLine)
	}
	elements = append(elements, timerView, barLine, statusLine)

	content := lipgloss.JoinVertical(lipgloss.Center, elements...)
	return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, content)
}

func (m model) settingsView(bodyHeight int) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		MarginBottom(1).
		Align(lipgloss.Center)

	maxRowWidth := 56

	row := func(idx int, label string, val string) string {
		isSelected := (m.selectedSetting == idx)
		var bg lipgloss.Color
		if isSelected {
			bg = lipgloss.Color("#252638")
		}

		var v string
		var rawValLen int

		if val == "[ON]" {
			if isSelected {
				v = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Background(bg).Bold(true).Render("[ON]")
			} else {
				v = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Bold(true).Render("[ON]")
			}
			rawValLen = 4
		} else if val == "[OFF]" {
			if isSelected {
				v = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")).Background(bg).Render("[OFF]")
			} else {
				v = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")).Render("[OFF]")
			}
			rawValLen = 5
		} else {
			if isSelected {
				v = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ECDC4")).Background(bg).Bold(true).Render(val)
			} else {
				v = lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA")).Bold(true).Render(val)
			}
			rawValLen = len([]rune(val))
		}

		if isSelected {
			prefix := lipgloss.NewStyle().Foreground(lipgloss.Color("#4ECDC4")).Background(bg).Bold(true).Render("❯ ")
			lblStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA")).Background(bg).Bold(true).Render(fmt.Sprintf("%-22s", label))
			gap := lipgloss.NewStyle().Background(bg).Render("    ")

			rawLen := 2 + 22 + 4 + rawValLen
			padCount := max(0, maxRowWidth-rawLen)
			pad := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", padCount))
			return prefix + lblStr + gap + v + pad
		}

		lblStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC")).Render(fmt.Sprintf("  %-22s", label))
		gap := "    "
		rawLen := 2 + 22 + 4 + rawValLen
		padCount := max(0, maxRowWidth-rawLen)
		pad := strings.Repeat(" ", padCount)
		return lblStr + gap + v + pad
	}

	onBadge := "[ON]"
	offBadge := "[OFF]"

	autoStartStr := offBadge
	if m.autoStartOnSkip {
		autoStartStr = onBadge
	}
	autoNextStr := offBadge
	if m.autoStartNext {
		autoNextStr = onBadge
	}
	soundStr := offBadge
	if m.soundAlert {
		soundStr = onBadge
	}

	activePresetName := "Custom"
	if m.presetIndex >= 0 && m.presetIndex < len(presets) {
		activePresetName = presets[m.presetIndex].name
	}

	activeThemeName := "Standard"
	if m.themeIndex >= 0 && m.themeIndex < len(themes) {
		activeThemeName = themes[m.themeIndex].name
	}

	goalStr := fmt.Sprintf("%dh %dm (%dm)", m.dailyGoalMinutes/60, m.dailyGoalMinutes%60, m.dailyGoalMinutes)

	rows := []string{
		row(0, "Preset", activePresetName),
		row(1, "Work Duration", fmt.Sprintf("%d min", m.workDuration)),
		row(2, "Short Break", fmt.Sprintf("%d min", m.shortBreakDuration)),
		row(3, "Long Break", fmt.Sprintf("%d min", m.longBreakDuration)),
		row(4, "Theme", activeThemeName),
		row(5, "Auto Start Break", autoStartStr),
		row(6, "Auto Start Work", autoNextStr),
		row(7, "Terminal Sound", soundStr),
		row(8, "Daily Goal Target", goalStr),
	}

	settingsBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#333344")).
		Padding(1, 3).
		Width(64).
		MarginBottom(1)

	footerHint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666677")).
		MarginTop(1).
		Align(lipgloss.Center).
		Render("Press 'r' to reset all settings to defaults")

	content := lipgloss.JoinVertical(lipgloss.Center,
		titleStyle.Render("Settings & Preferences"),
		settingsBox.Render(lipgloss.JoinVertical(lipgloss.Left, rows...)),
		footerHint,
	)
	return lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, content)
}

func main() {
	if handleCLISubcommands() {
		os.Exit(0)
	}

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
