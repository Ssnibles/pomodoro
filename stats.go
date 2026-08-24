// Package main provides statistics collection, daily focus history tracking, streak calculation, and analytics views.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// SessionLogEntry records a completed pomodoro focus session event.
type SessionLogEntry struct {
	ID            string    `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	DurationMins  int       `json:"duration_mins"`
	Category      string    `json:"category"`
	TaskTitle     string    `json:"task_title"`
	Note          string    `json:"note"`
	Interruptions int       `json:"interruptions"`
}

// SessionRecord holds the accumulated focus duration and completed session counts for a single date.
type SessionRecord struct {
	Date          string            `json:"date"`          // Date string formatted as YYYY-MM-DD
	Minutes       int               `json:"minutes"`       // Accumulated focus minutes completed
	Sessions      int               `json:"sessions"`      // Count of completed pomodoro sessions
	Interruptions int               `json:"interruptions"` // Total recorded interruptions today
	CategoryMins  map[string]int    `json:"category_mins"` // Focus duration per category map
	Logs          []SessionLogEntry `json:"logs"`          // Session log entries list
}

// HistoryStore encapsulates the collection of historical session records mapped by date string.
type HistoryStore struct {
	DailyRecords map[string]*SessionRecord `json:"daily_records"`
}

// historyPath returns the absolute file path to the session history JSON file.
func historyPath() string {
	return filepath.Join(configDir(), "history.json")
}

// loadHistory reads and deserialises historical session records from disk.
func loadHistory() (HistoryStore, error) {
	store := HistoryStore{
		DailyRecords: make(map[string]*SessionRecord),
	}
	data, err := os.ReadFile(historyPath())
	if err != nil {
		return store, err
	}
	err = json.Unmarshal(data, &store)
	if store.DailyRecords == nil {
		store.DailyRecords = make(map[string]*SessionRecord)
	}
	return store, err
}

// saveHistory serialises and atomically writes session history data to disk.
func saveHistory(store HistoryStore) error {
	return writeJSONAtomic(historyPath(), store)
}

// recordFocusSession records completed focus duration minutes, category breakdown, interruptions, and session log.
func recordFocusSession(minutes int, category string, taskTitle string, interruptions int, note string) {
	if minutes <= 0 {
		return
	}
	store, _ := loadHistory()
	if store.DailyRecords == nil {
		store.DailyRecords = make(map[string]*SessionRecord)
	}
	today := time.Now().Format("2006-01-02")
	rec, ok := store.DailyRecords[today]
	if !ok || rec == nil {
		rec = &SessionRecord{
			Date:          today,
			Minutes:       0,
			Sessions:      0,
			Interruptions: 0,
			CategoryMins:  make(map[string]int),
			Logs:          []SessionLogEntry{},
		}
		store.DailyRecords[today] = rec
	}
	if rec.CategoryMins == nil {
		rec.CategoryMins = make(map[string]int)
	}

	catKey := strings.TrimSpace(category)
	if catKey == "" {
		catKey = "general"
	}

	rec.Minutes += minutes
	rec.Sessions++
	rec.Interruptions += interruptions
	rec.CategoryMins[catKey] += minutes

	entry := SessionLogEntry{
		ID:            fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp:     time.Now(),
		DurationMins:  minutes,
		Category:      catKey,
		TaskTitle:     taskTitle,
		Note:          note,
		Interruptions: interruptions,
	}
	rec.Logs = append(rec.Logs, entry)

	_ = saveHistory(store)
}

// calculateStreak computes the current consecutive day focus streak count.
func calculateStreak(store HistoryStore) int {
	if store.DailyRecords == nil {
		return 0
	}
	streak := 0
	curr := time.Now()
	for {
		dateStr := curr.Format("2006-01-02")
		rec, ok := store.DailyRecords[dateStr]
		if ok && rec != nil && rec.Sessions > 0 {
			streak++
			curr = curr.AddDate(0, 0, -1)
		} else {
			if streak == 0 {
				yesterday := curr.AddDate(0, 0, -1).Format("2006-01-02")
				if recY, okY := store.DailyRecords[yesterday]; okY && recY != nil && recY.Sessions > 0 {
					curr = curr.AddDate(0, 0, -1)
					continue
				}
			}
			break
		}
	}
	return streak
}

// renderStatsView constructs the rendered view layout for productivity analytics, charts, category breakdown, and focus matrix.
func renderStatsView(width, height int, themeColor lipgloss.Color, goalMins int) string {
	store, _ := loadHistory()
	today := time.Now().Format("2006-01-02")
	todayRec := store.DailyRecords[today]
	todayMins := 0
	todaySessions := 0
	todayInterrupts := 0
	var catMins map[string]int

	if todayRec != nil {
		todayMins = todayRec.Minutes
		todaySessions = todayRec.Sessions
		todayInterrupts = todayRec.Interruptions
		catMins = todayRec.CategoryMins
	}
	if catMins == nil {
		catMins = make(map[string]int)
	}

	streak := calculateStreak(store)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		MarginBottom(1).
		Align(lipgloss.Center)

	// Focus Score calculation (% sessions without interruptions)
	focusScorePct := 100
	if todaySessions > 0 {
		cleanSessions := todaySessions - todayInterrupts
		if cleanSessions < 0 {
			cleanSessions = 0
		}
		focusScorePct = (cleanSessions * 100) / todaySessions
	}

	// 1. Clean Borderless Summary Stats Row
	lblStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888899")).Bold(true)
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA")).Bold(true)
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#444455"))

	col1 := lipgloss.JoinVertical(lipgloss.Center, lblStyle.Render("FOCUS TODAY"), valStyle.Render(fmt.Sprintf("%dh %dm", todayMins/60, todayMins%60)))
	col2 := lipgloss.JoinVertical(lipgloss.Center, lblStyle.Render("POMODOROS"), valStyle.Render(fmt.Sprintf("%d 🍅 (%d⚡)", todaySessions, todayInterrupts)))
	col3 := lipgloss.JoinVertical(lipgloss.Center, lblStyle.Render("STREAK / SCORE"), valStyle.Render(fmt.Sprintf("🔥 %dD • %d%%", streak, focusScorePct)))

	cardsRow := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.PlaceHorizontal(18, lipgloss.Center, col1),
		sepStyle.Render(" │ "),
		lipgloss.PlaceHorizontal(22, lipgloss.Center, col2),
		sepStyle.Render(" │ "),
		lipgloss.PlaceHorizontal(18, lipgloss.Center, col3),
	)

	// 2. Daily Goal Progress (Clean Line)
	goalPct := 0
	if goalMins > 0 {
		goalPct = (todayMins * 100) / goalMins
		if goalPct > 100 {
			goalPct = 100
		}
	}
	goalBarLen := 26
	goalFilled := (goalPct * goalBarLen) / 100
	goalBarStr := strings.Repeat("█", goalFilled) + strings.Repeat("░", goalBarLen-goalFilled)

	goalText := fmt.Sprintf("🎯 Daily Goal: %dh %dm / %dh %dm   [%s] %d%%",
		todayMins/60, todayMins%60, goalMins/60, goalMins%60, goalBarStr, goalPct)

	goalBlock := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#50FA7B")).
		Bold(true).
		MarginTop(1).
		MarginBottom(1).
		Render(goalText)

	// 3. 7-Day Focus Activity Chart
	now := time.Now()
	type dayStat struct {
		dayLabel string
		minutes  int
	}
	days := make([]dayStat, 7)
	maxMins := 1

	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dateStr := d.Format("2006-01-02")
		label := d.Format("Mon")
		mins := 0
		if rec, ok := store.DailyRecords[dateStr]; ok && rec != nil {
			mins = rec.Minutes
		}
		if mins > maxMins {
			maxMins = mins
		}
		days[6-i] = dayStat{dayLabel: label, minutes: mins}
	}

	chartHeight := 4
	colWidth := 8
	var chartLines []string

	for level := chartHeight; level >= 1; level-- {
		var cols []string
		for _, ds := range days {
			barRatio := float64(ds.minutes) / float64(maxMins)
			barUnits := int(barRatio * float64(chartHeight))
			if ds.minutes > 0 && barUnits == 0 {
				barUnits = 1
			}

			var cell string
			if barUnits >= level {
				cell = lipgloss.NewStyle().Foreground(themeColor).Render("█")
			} else {
				cell = " "
			}
			cols = append(cols, lipgloss.PlaceHorizontal(colWidth, lipgloss.Center, cell))
		}
		chartLines = append(chartLines, lipgloss.JoinHorizontal(lipgloss.Top, cols...))
	}

	var labelCols []string
	for _, ds := range days {
		lbl := lipgloss.NewStyle().Foreground(lipgloss.Color("#888899")).Render(ds.dayLabel)
		labelCols = append(labelCols, lipgloss.PlaceHorizontal(colWidth, lipgloss.Center, lbl))
	}
	chartLines = append(chartLines, lipgloss.JoinHorizontal(lipgloss.Top, labelCols...))

	var valCols []string
	for _, ds := range days {
		valStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#666677")).Render(fmt.Sprintf("%dm", ds.minutes))
		valCols = append(valCols, lipgloss.PlaceHorizontal(colWidth, lipgloss.Center, valStr))
	}
	chartLines = append(chartLines, lipgloss.JoinHorizontal(lipgloss.Top, valCols...))

	chartTitle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC")).Bold(true).MarginBottom(1).Render("─ 7-Day Focus Activity ─")
	chartBlock := lipgloss.JoinVertical(lipgloss.Center, append([]string{chartTitle}, chartLines...)...)

	// 4. 24-Week Focus Contribution Matrix
	numWeeks := 24
	matrixWeeks := make([][]int, numWeeks)
	for w := 0; w < numWeeks; w++ {
		matrixWeeks[w] = make([]int, 7)
	}

	todayDate := time.Now()
	dayOfWeek := int(todayDate.Weekday()) - 1
	if dayOfWeek < 0 {
		dayOfWeek = 6
	}

	for w := numWeeks - 1; w >= 0; w-- {
		for d := 6; d >= 0; d-- {
			daysAgo := (numWeeks-1-w)*7 + (dayOfWeek - d)
			if daysAgo < 0 {
				continue
			}
			targetDate := todayDate.AddDate(0, 0, -daysAgo)
			dateStr := targetDate.Format("2006-01-02")
			mins := 0
			if rec, ok := store.DailyRecords[dateStr]; ok && rec != nil {
				mins = rec.Minutes
			}
			matrixWeeks[w][d] = mins
		}
	}

	// Month header string calculation
	monthHeaderRunes := make([]rune, numWeeks*2)
	for i := range monthHeaderRunes {
		monthHeaderRunes[i] = ' '
	}
	lastMonth := ""
	for w := 0; w < numWeeks; w++ {
		daysAgo := (numWeeks - 1 - w) * 7
		targetDate := todayDate.AddDate(0, 0, -daysAgo)
		mName := targetDate.Format("Jan")
		if mName != lastMonth {
			lastMonth = mName
			pos := w * 2
			if pos+3 <= len(monthHeaderRunes) {
				for idx, ch := range mName {
					monthHeaderRunes[pos+idx] = ch
				}
			}
		}
	}
	monthHeaderStr := "    " + lipgloss.NewStyle().Foreground(lipgloss.Color("#888899")).Render(string(monthHeaderRunes))

	var matrixRowStrings []string
	matrixRowStrings = append(matrixRowStrings, monthHeaderStr)

	dayNames := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

	for d := 0; d < 7; d++ {
		var cells []string
		if d == 0 || d == 2 || d == 4 {
			cells = append(cells, lipgloss.NewStyle().Foreground(lipgloss.Color("#777788")).Render(dayNames[d]+" "))
		} else {
			cells = append(cells, "    ")
		}

		for w := 0; w < numWeeks; w++ {
			mins := matrixWeeks[w][d]
			var cell string
			if mins <= 0 {
				cell = lipgloss.NewStyle().Foreground(lipgloss.Color("#2A2B3D")).Render("■ ")
			} else if mins <= 25 {
				cell = lipgloss.NewStyle().Foreground(lipgloss.Color("#2E6F40")).Render("■ ")
			} else if mins <= 50 {
				cell = lipgloss.NewStyle().Foreground(lipgloss.Color("#38A169")).Render("■ ")
			} else {
				cell = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Render("■ ")
			}
			cells = append(cells, cell)
		}
		matrixRowStrings = append(matrixRowStrings, strings.Join(cells, ""))
	}

	legendStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#777788")).MarginTop(1).Render("Less ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#2A2B3D")).Render("■ ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#2E6F40")).Render("■ ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#38A169")).Render("■ ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Render("■ ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#777788")).Render("More")

	matrixRowStrings = append(matrixRowStrings, legendStr)

	matrixTitle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC")).Bold(true).MarginBottom(1).Render("24-Week Focus Contribution Matrix")
	matrixContainer := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#333344")).
		Padding(1, 2).
		MarginTop(1).
		MarginBottom(1).
		Align(lipgloss.Center)

	matrixBlock := matrixContainer.Render(lipgloss.JoinVertical(lipgloss.Center, append([]string{matrixTitle}, matrixRowStrings...)...))

	// 5. Category Breakdown
	var catLines []string
	if len(catMins) > 0 {
		type catEntry struct {
			name string
			mins int
		}
		var sortedCats []catEntry
		for name, m := range catMins {
			sortedCats = append(sortedCats, catEntry{name, m})
		}
		sort.Slice(sortedCats, func(i, j int) bool {
			return sortedCats[i].mins > sortedCats[j].mins
		})

		catTitle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC")).Bold(true).MarginTop(1).MarginBottom(1).Render("─ Category Breakdown ─")
		catLines = append(catLines, catTitle)

		maxBarLen := 24
		for _, c := range sortedCats {
			pct := 0
			if todayMins > 0 {
				pct = (c.mins * 100) / todayMins
			}
			filled := (pct * maxBarLen) / 100
			barStr := strings.Repeat("█", filled) + strings.Repeat("░", maxBarLen-filled)
			lineStr := fmt.Sprintf("%-10s  [%s]  %3dm (%d%%)", c.name, barStr, c.mins, pct)
			catLines = append(catLines, lipgloss.NewStyle().Foreground(lipgloss.Color("#A3BE8C")).Render(lineStr))
		}
	}
	var catBlock string
	if len(catLines) > 0 {
		catBlock = lipgloss.JoinVertical(lipgloss.Center, catLines...)
	}

	// Layout block assembly
	var blocks []string
	blocks = append(blocks, titleStyle.Render("Productivity Analytics"), cardsRow, goalBlock, chartBlock, matrixBlock)

	if catBlock != "" {
		blocks = append(blocks, catBlock)
	}

	content := lipgloss.JoinVertical(lipgloss.Center, blocks...)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}
