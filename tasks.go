// Package main provides task management models, inline token parser, priority sorting, live preview rendering, and interactive task view components.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Task represents an individual task item with Pomodoro progress tracking, category tagging, context tags, due dates, and numeric priority levels.
type Task struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Category  string `json:"category"` // e.g. "dev" from +dev
	Context   string `json:"context"`  // e.g. "work" from @work
	DueDate   string `json:"due_date"` // e.g. "2026-08-25" from due:2026-08-25
	Priority  int    `json:"priority"` // 1 = Highest, 2 = Medium-High, 3 = Medium-Low, 4 = Lowest
	Completed bool   `json:"completed"`
	Pomodoros int    `json:"pomodoros"`
	Target    int    `json:"target"`
}

// TaskStore manages the collection of tasks and the currently active task reference.
type TaskStore struct {
	Tasks        []Task `json:"tasks"`
	ActiveTaskID string `json:"active_task_id"`
}

// tasksPath returns the absolute file path to the tasks storage JSON file.
func tasksPath() string {
	return filepath.Join(configDir(), "tasks.json")
}

// parseTaskInput parses a single-line task specification into a structured Task.
// Examples:
//   "=1 Refactor database query +dev @work due:2026-08-25 4p"
//   "Fix authentication bug +dev t:3"
func parseTaskInput(raw string) Task {
	raw = strings.TrimSpace(raw)
	t := Task{
		Priority: 2,
		Category: "general",
		Target:   2,
	}

	fields := strings.Fields(raw)
	var titleParts []string

	for _, token := range fields {
		lowerToken := strings.ToLower(token)

		// 1. Priority token: =1, =2, =3, =4 (or p1, p2, p:1, =p1)
		if (strings.HasPrefix(lowerToken, "=") || strings.HasPrefix(lowerToken, "p")) && len(lowerToken) >= 2 {
			trimmed := strings.TrimPrefix(lowerToken, "=")
			trimmed = strings.TrimPrefix(trimmed, "p")
			trimmed = strings.TrimPrefix(trimmed, ":")
			if p, err := strconv.Atoi(trimmed); err == nil && p >= 1 && p <= 9 {
				t.Priority = p
				continue
			}
		}

		// 2. Category token: +category
		if strings.HasPrefix(token, "+") && len(token) > 1 {
			t.Category = strings.ToLower(token[1:])
			continue
		}

		// 3. Context token: @context
		if strings.HasPrefix(token, "@") && len(token) > 1 {
			t.Context = strings.ToLower(token[1:])
			continue
		}

		// 4. Due date token: due:YYYY-MM-DD
		if strings.HasPrefix(lowerToken, "due:") && len(token) > 4 {
			t.DueDate = token[4:]
			continue
		}

		// 5. Target pomodoro token: t:N or pomo:N or Np or Nt
		if strings.HasPrefix(lowerToken, "t:") && len(token) > 2 {
			if target, err := strconv.Atoi(token[2:]); err == nil && target >= 1 {
				t.Target = target
				continue
			}
		}
		if strings.HasPrefix(lowerToken, "pomo:") && len(token) > 5 {
			if target, err := strconv.Atoi(token[5:]); err == nil && target >= 1 {
				t.Target = target
				continue
			}
		}
		if (strings.HasSuffix(lowerToken, "p") || strings.HasSuffix(lowerToken, "t")) && len(lowerToken) >= 2 {
			numPart := lowerToken[:len(lowerToken)-1]
			if target, err := strconv.Atoi(numPart); err == nil && target >= 1 {
				t.Target = target
				continue
			}
		}

		titleParts = append(titleParts, token)
	}

	if len(titleParts) > 0 {
		t.Title = strings.Join(titleParts, " ")
	}

	return t
}

// loadTasks reads and deserialises tasks from disk.
func loadTasks() (TaskStore, error) {
	var store TaskStore
	data, err := os.ReadFile(tasksPath())
	if err != nil {
		return store, err
	}
	err = json.Unmarshal(data, &store)
	return store, err
}

// saveTasks serialises and atomically writes tasks to disk.
func saveTasks(store TaskStore) error {
	return writeJSONAtomic(tasksPath(), store)
}

// ActiveTask retrieves the currently active task, if set.
func (ts *TaskStore) ActiveTask() *Task {
	if ts.ActiveTaskID == "" {
		return nil
	}
	for i := range ts.Tasks {
		if ts.Tasks[i].ID == ts.ActiveTaskID {
			return &ts.Tasks[i]
		}
	}
	return nil
}

// IncrementActivePomodoro increments the completed pomodoro count for the currently active task.
func (ts *TaskStore) IncrementActivePomodoro() {
	task := ts.ActiveTask()
	if task == nil {
		return
	}
	task.Pomodoros++
	_ = saveTasks(*ts)
}

// taskModel manages state for task list navigation, creation modal, and task actions.
type taskModel struct {
	store          TaskStore
	selectedIndex  int
	addingTask     bool
	newTaskInput   string
	inputCursorPos int
}

// initialTaskModel constructs and initialises the taskModel with persisted task data.
func initialTaskModel() taskModel {
	store, _ := loadTasks()
	tm := taskModel{
		store:          store,
		selectedIndex:  0,
		addingTask:     false,
		newTaskInput:   "",
		inputCursorPos: 0,
	}
	tm.sortTasks()
	return tm
}

// sortTasks sorts the tasks list: uncompleted first (sorted by priority =1, =2, =3...), then completed.
func (tm *taskModel) sortTasks() {
	activeID := tm.store.ActiveTaskID
	sort.SliceStable(tm.store.Tasks, func(i, j int) bool {
		ti, tj := tm.store.Tasks[i], tm.store.Tasks[j]
		if ti.Completed != tj.Completed {
			return !ti.Completed // Uncompleted tasks first
		}
		p1, p2 := ti.Priority, tj.Priority
		if p1 <= 0 {
			p1 = 2
		}
		if p2 <= 0 {
			p2 = 2
		}
		if p1 != p2 {
			return p1 < p2 // Lower priority number = higher priority
		}
		return i < j
	})

	// Restore selectedIndex for active task or bounds
	if activeID != "" {
		for i, t := range tm.store.Tasks {
			if t.ID == activeID && tm.selectedIndex >= len(tm.store.Tasks) {
				tm.selectedIndex = i
				break
			}
		}
	}
	if tm.selectedIndex >= len(tm.store.Tasks) {
		tm.selectedIndex = max(0, len(tm.store.Tasks)-1)
	}
}

func (tm taskModel) update(msg tea.Msg) (taskModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return tm, nil
	}

	if tm.addingTask {
		switch keyMsg.Type {
		case tea.KeyEsc:
			tm.addingTask = false
			tm.newTaskInput = ""
			tm.inputCursorPos = 0
			return tm, nil

		case tea.KeyEnter:
			input := strings.TrimSpace(tm.newTaskInput)
			if input != "" {
				parsed := parseTaskInput(input)
				parsed.ID = fmt.Sprintf("%d", time.Now().UnixNano())
				if parsed.Title == "" {
					parsed.Title = "Untitled Task"
				}
				tm.store.Tasks = append(tm.store.Tasks, parsed)
				if tm.store.ActiveTaskID == "" {
					tm.store.ActiveTaskID = parsed.ID
				}
				_ = saveTasks(tm.store)
				tm.sortTasks()
			}
			tm.addingTask = false
			tm.newTaskInput = ""
			tm.inputCursorPos = 0
			return tm, nil

		case tea.KeyLeft:
			if tm.inputCursorPos > 0 {
				tm.inputCursorPos--
			}
			return tm, nil

		case tea.KeyRight:
			runes := []rune(tm.newTaskInput)
			if tm.inputCursorPos < len(runes) {
				tm.inputCursorPos++
			}
			return tm, nil

		case tea.KeyHome:
			tm.inputCursorPos = 0
			return tm, nil

		case tea.KeyEnd:
			runes := []rune(tm.newTaskInput)
			tm.inputCursorPos = len(runes)
			return tm, nil

		case tea.KeyBackspace:
			runes := []rune(tm.newTaskInput)
			if tm.inputCursorPos > 0 && len(runes) > 0 {
				before := runes[:tm.inputCursorPos-1]
				after := runes[tm.inputCursorPos:]
				tm.newTaskInput = string(append(before, after...))
				tm.inputCursorPos--
			}
			return tm, nil

		case tea.KeyDelete:
			runes := []rune(tm.newTaskInput)
			if tm.inputCursorPos < len(runes) {
				before := runes[:tm.inputCursorPos]
				after := runes[tm.inputCursorPos+1:]
				tm.newTaskInput = string(append(before, after...))
			}
			return tm, nil

		case tea.KeyRunes:
			runes := []rune(tm.newTaskInput)
			inserted := keyMsg.Runes
			before := runes[:tm.inputCursorPos]
			after := runes[tm.inputCursorPos:]
			newRunes := append(before, append(inserted, after...)...)
			tm.newTaskInput = string(newRunes)
			tm.inputCursorPos += len(inserted)
			return tm, nil

		case tea.KeySpace:
			runes := []rune(tm.newTaskInput)
			before := runes[:tm.inputCursorPos]
			after := runes[tm.inputCursorPos:]
			newRunes := append(before, append([]rune{' '}, after...)...)
			tm.newTaskInput = string(newRunes)
			tm.inputCursorPos++
			return tm, nil
		}
		return tm, nil
	}

	// Normal list navigation mode
	switch keyMsg.String() {
	case "a", "n":
		tm.addingTask = true
		tm.newTaskInput = ""
		tm.inputCursorPos = 0
		return tm, nil

	case "up", "k":
		if tm.selectedIndex > 0 {
			tm.selectedIndex--
		}

	case "down", "j":
		if tm.selectedIndex < len(tm.store.Tasks)-1 {
			tm.selectedIndex++
		}

	case "Shift+up", "K":
		if tm.selectedIndex > 0 && len(tm.store.Tasks) > 1 {
			i := tm.selectedIndex
			tm.store.Tasks[i], tm.store.Tasks[i-1] = tm.store.Tasks[i-1], tm.store.Tasks[i]
			tm.selectedIndex--
			_ = saveTasks(tm.store)
		}

	case "Shift+down", "J":
		if tm.selectedIndex < len(tm.store.Tasks)-1 && len(tm.store.Tasks) > 1 {
			i := tm.selectedIndex
			tm.store.Tasks[i], tm.store.Tasks[i+1] = tm.store.Tasks[i+1], tm.store.Tasks[i]
			tm.selectedIndex++
			_ = saveTasks(tm.store)
		}

	case "space":
		if len(tm.store.Tasks) > 0 && tm.selectedIndex < len(tm.store.Tasks) {
			targetID := tm.store.Tasks[tm.selectedIndex].ID
			if tm.store.ActiveTaskID == targetID {
				tm.store.ActiveTaskID = ""
			} else {
				tm.store.ActiveTaskID = targetID
			}
			_ = saveTasks(tm.store)
		}

	case "x":
		if len(tm.store.Tasks) > 0 && tm.selectedIndex < len(tm.store.Tasks) {
			tm.store.Tasks[tm.selectedIndex].Completed = !tm.store.Tasks[tm.selectedIndex].Completed
			_ = saveTasks(tm.store)
			tm.sortTasks()
		}

	case "p":
		if len(tm.store.Tasks) > 0 && tm.selectedIndex < len(tm.store.Tasks) {
			currP := tm.store.Tasks[tm.selectedIndex].Priority
			if currP <= 0 {
				currP = 2
			}
			newP := (currP % 4) + 1
			tm.store.Tasks[tm.selectedIndex].Priority = newP
			_ = saveTasks(tm.store)
			tm.sortTasks()
		}

	case "d":
		if len(tm.store.Tasks) > 0 && tm.selectedIndex < len(tm.store.Tasks) {
			deletedID := tm.store.Tasks[tm.selectedIndex].ID
			if tm.store.ActiveTaskID == deletedID {
				tm.store.ActiveTaskID = ""
			}
			tm.store.Tasks = append(tm.store.Tasks[:tm.selectedIndex], tm.store.Tasks[tm.selectedIndex+1:]...)
			if tm.selectedIndex >= len(tm.store.Tasks) {
				tm.selectedIndex = max(0, len(tm.store.Tasks)-1)
			}
			_ = saveTasks(tm.store)
			tm.sortTasks()
		}
	}

	return tm, nil
}

// renderPriorityBadge renders numeric priority badges (=1, =2, =3, =4).
func renderPriorityBadge(priority int, isSelected bool, bg lipgloss.Color) string {
	var priStr string
	switch priority {
	case 1:
		if isSelected {
			priStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Background(bg).Bold(true).Render("[=1] ")
		} else {
			priStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Bold(true).Render("[=1] ")
		}
	case 2:
		if isSelected {
			priStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Background(bg).Render("[=2] ")
		} else {
			priStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Render("[=2] ")
		}
	case 3:
		if isSelected {
			priStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1")).Background(bg).Render("[=3] ")
		} else {
			priStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1")).Render("[=3] ")
		}
	default:
		if isSelected {
			priStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#666677")).Background(bg).Render("[=4] ")
		} else {
			priStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#666677")).Render("[=4] ")
		}
	}
	return priStr
}

func (tm taskModel) view(width, height int, activeColor lipgloss.Color) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		MarginBottom(1).
		Align(lipgloss.Center)

	if tm.addingTask {
		modalWidth := 74
		inputBoxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(activeColor).
			Padding(1, 3).
			Width(modalWidth).
			Align(lipgloss.Left)

		prompt := lipgloss.NewStyle().
			Bold(true).
			Foreground(activeColor).
			MarginBottom(1).
			Align(lipgloss.Left).
			Render("Add Task")

		// Cursor rendering logic
		runes := []rune(tm.newTaskInput)
		if tm.inputCursorPos > len(runes) {
			tm.inputCursorPos = len(runes)
		}

		var inputDisplay string
		cursorStyle := lipgloss.NewStyle().Reverse(true)
		if tm.inputCursorPos == len(runes) {
			inputDisplay = string(runes) + cursorStyle.Render(" ")
		} else {
			before := string(runes[:tm.inputCursorPos])
			at := cursorStyle.Render(string(runes[tm.inputCursorPos : tm.inputCursorPos+1]))
			after := string(runes[tm.inputCursorPos+1:])
			inputDisplay = before + at + after
		}

		inputField := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#252638")).
			Padding(0, 1).
			Width(modalWidth - 8).
			MarginBottom(1).
			Render(inputDisplay)

		// Dynamic live preview
		isPlaceholder := strings.TrimSpace(tm.newTaskInput) == ""
		var previewTask Task
		if isPlaceholder {
			previewTask = Task{
				Title:    "Enter task title...",
				Priority: 2,
				Category: "general",
				Target:   2,
			}
		} else {
			previewTask = parseTaskInput(tm.newTaskInput)
		}

		checkStr := lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")).Render("[ ] ")
		priStr := renderPriorityBadge(previewTask.Priority, false, "")

		var titleStr, catStr, ctxStr, dueStr, pomoStr string
		if isPlaceholder {
			mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666677"))
			titleStr = mutedStyle.Render(previewTask.Title)
			catStr = mutedStyle.Render(" [+" + previewTask.Category + "]")
			pomoStr = mutedStyle.Render(fmt.Sprintf(" (🍅 0/%d)", previewTask.Target))
		} else {
			titleStr = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")).Render(previewTask.Title)
			if previewTask.Category != "" {
				catStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1")).Render(" [+" + previewTask.Category + "]")
			}
			if previewTask.Context != "" {
				ctxStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#C678DD")).Render(" [@" + previewTask.Context + "]")
			}
			if previewTask.DueDate != "" {
				dueStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ECDC4")).Render(" [📅 " + previewTask.DueDate + "]")
			}
			pomoStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Render(fmt.Sprintf(" (🍅 %d/%d)", previewTask.Pomodoros, previewTask.Target))
		}

		previewRow := checkStr + priStr + titleStr + catStr + ctxStr + dueStr + pomoStr

		previewHeader := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#777788")).
			MarginBottom(0).
			Render("Preview:")

		previewBlock := lipgloss.JoinVertical(
			lipgloss.Left,
			previewHeader,
			previewRow,
		)

		syntaxGuide := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666677")).
			MarginTop(1).
			Render("Format: =1..=4  •  +category  •  @context  •  due:YYYY-MM-DD  •  4p")

		content := inputBoxStyle.Render(lipgloss.JoinVertical(
			lipgloss.Left,
			prompt,
			inputField,
			previewBlock,
			syntaxGuide,
		))
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
	}

	totalTasks := len(tm.store.Tasks)
	completedCount := 0
	for _, t := range tm.store.Tasks {
		if t.Completed {
			completedCount++
		}
	}

	headerText := fmt.Sprintf("Task Manager   •   %d Total   •   %d Done", totalTasks, completedCount)
	headerLine := titleStyle.Render(headerText)

	if totalTasks == 0 {
		emptyText := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#777788")).
			MarginTop(2).
			Render("No tasks created yet.\nPress 'a' or 'n' to add a task!")
		content := lipgloss.JoinVertical(lipgloss.Center, titleStyle.Render("Task Manager"), emptyText)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
	}

	var listRows []string
	maxRowWidth := 74

	for i, t := range tm.store.Tasks {
		isSelected := (i == tm.selectedIndex)
		var bg lipgloss.Color
		if isSelected {
			bg = lipgloss.Color("#252638")
		}

		var cursorStr string
		if isSelected {
			cursorStr = lipgloss.NewStyle().Foreground(activeColor).Background(bg).Bold(true).Render("❯ ")
		} else {
			cursorStr = "  "
		}

		// Active task prefix indicator: fixed 2 columns (⚡ or 2 spaces)
		var activePrefix string
		if t.ID == tm.store.ActiveTaskID {
			if isSelected {
				activePrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Background(bg).Bold(true).Render("⚡ ")
			} else {
				activePrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Bold(true).Render("⚡ ")
			}
		} else {
			if isSelected {
				activePrefix = lipgloss.NewStyle().Background(bg).Render("  ")
			} else {
				activePrefix = "  "
			}
		}

		var check string
		if t.Completed {
			if isSelected {
				check = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Background(bg).Render("[✓] ")
			} else {
				check = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Render("[✓] ")
			}
		} else {
			if isSelected {
				check = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")).Background(bg).Render("[ ] ")
			} else {
				check = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")).Render("[ ] ")
			}
		}

		priStr := renderPriorityBadge(t.Priority, isSelected, bg)

		var titleStr string
		if t.Completed {
			if isSelected {
				titleStr = lipgloss.NewStyle().Strikethrough(true).Foreground(lipgloss.Color("#666666")).Background(bg).Render(t.Title)
			} else {
				titleStr = lipgloss.NewStyle().Strikethrough(true).Foreground(lipgloss.Color("#666666")).Render(t.Title)
			}
		} else if isSelected {
			titleStr = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FAFAFA")).Background(bg).Render(t.Title)
		} else {
			titleStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC")).Render(t.Title)
		}

		catLabel := ""
		if t.Category != "" {
			if isSelected {
				catLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1")).Background(bg).Render(fmt.Sprintf(" [+%s]", t.Category))
			} else {
				catLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1")).Render(fmt.Sprintf(" [+%s]", t.Category))
			}
		}

		ctxLabel := ""
		if t.Context != "" {
			if isSelected {
				ctxLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#C678DD")).Background(bg).Render(fmt.Sprintf(" [@%s]", t.Context))
			} else {
				ctxLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#C678DD")).Render(fmt.Sprintf(" [@%s]", t.Context))
			}
		}

		dueLabel := ""
		if t.DueDate != "" {
			if isSelected {
				dueLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ECDC4")).Background(bg).Render(fmt.Sprintf(" [📅 %s]", t.DueDate))
			} else {
				dueLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ECDC4")).Render(fmt.Sprintf(" [📅 %s]", t.DueDate))
			}
		}

		var pomoStr string
		if isSelected {
			pomoStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Background(bg).Render(fmt.Sprintf(" (🍅 %d/%d)", t.Pomodoros, t.Target))
		} else {
			pomoStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Render(fmt.Sprintf(" (🍅 %d/%d)", t.Pomodoros, t.Target))
		}

		var line string
		if isSelected {
			rawText := fmt.Sprintf("❯ %s%s[P%d] %s", func() string {
				if t.ID == tm.store.ActiveTaskID {
					return "⚡ "
				}
				return "  "
			}(), func() string {
				if t.Completed {
					return "[✓] "
				}
				return "[ ] "
			}(), func() int {
				if t.Priority <= 0 {
					return 2
				}
				return t.Priority
			}(), t.Title)

			if t.Category != "" {
				rawText += fmt.Sprintf(" [+%s]", t.Category)
			}
			if t.Context != "" {
				rawText += fmt.Sprintf(" [@%s]", t.Context)
			}
			if t.DueDate != "" {
				rawText += fmt.Sprintf(" [📅 %s]", t.DueDate)
			}
			rawText += fmt.Sprintf(" (🍅 %d/%d)", t.Pomodoros, t.Target)

			runeCount := len([]rune(rawText))
			padCount := max(0, maxRowWidth-runeCount)
			padStr := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", padCount))
			line = cursorStr + activePrefix + check + priStr + titleStr + catLabel + ctxLabel + dueLabel + pomoStr + padStr
		} else {
			line = cursorStr + activePrefix + check + priStr + titleStr + catLabel + ctxLabel + dueLabel + pomoStr
		}

		listRows = append(listRows, line)
	}

	listContainer := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#333344")).
		Padding(1, 2).
		MarginBottom(1).
		Width(74)

	content := lipgloss.JoinVertical(lipgloss.Center, headerLine, listContainer.Render(strings.Join(listRows, "\n")))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}
