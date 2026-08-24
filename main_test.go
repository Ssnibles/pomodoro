package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "pomodoro-test-*")
	if err == nil {
		os.Setenv("POMODORO_CONFIG_DIR", tmpDir)
		defer os.RemoveAll(tmpDir)
	}
	os.Exit(m.Run())
}

func TestInitialModel(t *testing.T) {
	m := initialModel()
	if m.workDuration < 1 {
		t.Errorf("Expected work duration >= 1, got %d", m.workDuration)
	}
	if m.mode != workMode {
		t.Errorf("Expected initial mode workMode, got %v", m.mode)
	}
}

func TestPresetSwitching(t *testing.T) {
	m := initialModel()
	m.applyPreset(1)
	if m.workDuration != 50 || m.shortBreakDuration != 10 || m.longBreakDuration != 20 {
		t.Errorf("ApplyPreset(1) failed: work=%d, short=%d, long=%d", m.workDuration, m.shortBreakDuration, m.longBreakDuration)
	}
	if m.presetIndex != 1 {
		t.Errorf("Expected presetIndex=1, got %d", m.presetIndex)
	}
}

func TestNextModeTransitions(t *testing.T) {
	m := initialModel()
	m.workDuration = 25
	m.shortBreakDuration = 5
	m.longBreakDuration = 15

	m = m.nextMode()
	if m.completedCycles != 1 {
		t.Errorf("Expected completedCycles=1, got %d", m.completedCycles)
	}
	if m.mode != shortBreakMode {
		t.Errorf("Expected shortBreakMode, got %v", m.mode)
	}
	if m.remaining != 5*time.Minute {
		t.Errorf("Expected remaining 5m, got %v", m.remaining)
	}

	m = m.nextMode()
	if m.mode != workMode {
		t.Errorf("Expected workMode, got %v", m.mode)
	}
}

func TestTimerKeybindingsAndInterruption(t *testing.T) {
	m := initialModel()
	m.presetIndex = 0

	mUpdated, _ := m.updateTimer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m2 := mUpdated.(model)
	if !m2.running {
		t.Errorf("Expected timer to be running after pressing space")
	}

	mInterrupted, _ := m2.updateTimer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m3 := mInterrupted.(model)
	if m3.currentInterruptions != 1 {
		t.Errorf("Expected currentInterruptions=1, got %d", m3.currentInterruptions)
	}
}

func TestParseTaskInputEqualsPrioritySyntax(t *testing.T) {
	input := "=1 Make the thing +dev due:2029-12-31 @home 4p"
	task := parseTaskInput(input)

	if task.Priority != 1 {
		t.Errorf("Expected Priority=1 from '=1', got %d", task.Priority)
	}
	if task.Title != "Make the thing" {
		t.Errorf("Expected Title 'Make the thing', got '%s'", task.Title)
	}
	if task.Category != "dev" {
		t.Errorf("Expected Category 'dev', got '%s'", task.Category)
	}
	if task.Context != "home" {
		t.Errorf("Expected Context 'home', got '%s'", task.Context)
	}
	if task.DueDate != "2029-12-31" {
		t.Errorf("Expected DueDate '2029-12-31', got '%s'", task.DueDate)
	}
	if task.Target != 4 {
		t.Errorf("Expected Target=4, got %d", task.Target)
	}
}

func TestTaskInputCursorNavigation(t *testing.T) {
	tm := initialTaskModel()
	tm.addingTask = true
	tm.newTaskInput = "helo"
	tm.inputCursorPos = 4

	// Move left twice -> position 2
	tm, _ = tm.update(tea.KeyMsg{Type: tea.KeyLeft})
	tm, _ = tm.update(tea.KeyMsg{Type: tea.KeyLeft})
	if tm.inputCursorPos != 2 {
		t.Errorf("Expected inputCursorPos=2, got %d", tm.inputCursorPos)
	}

	// Insert 'l' -> "hello"
	tm, _ = tm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if tm.newTaskInput != "hello" {
		t.Errorf("Expected newTaskInput='hello', got '%s'", tm.newTaskInput)
	}
	if tm.inputCursorPos != 3 {
		t.Errorf("Expected inputCursorPos=3 after insert, got %d", tm.inputCursorPos)
	}
}

func TestTaskSingleStepCreationAndAutoSort(t *testing.T) {
	tm := initialTaskModel()
	tm.store.Tasks = []Task{
		{ID: "1", Title: "Low Priority Task", Category: "general", Priority: 3},
	}
	tm.selectedIndex = 0

	tm.addingTask = true
	tm.newTaskInput = "=1 High Priority Task +dev 3p"
	tmFinal, _ := tm.update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(tmFinal.store.Tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(tmFinal.store.Tasks))
	}

	if tmFinal.store.Tasks[0].Title != "High Priority Task" || tmFinal.store.Tasks[0].Priority != 1 {
		t.Errorf("Expected P1 'High Priority Task' at index 0, got '%s' (P%d)", tmFinal.store.Tasks[0].Title, tmFinal.store.Tasks[0].Priority)
	}
	if tmFinal.store.Tasks[0].Category != "dev" || tmFinal.store.Tasks[0].Target != 3 {
		t.Errorf("Expected Category 'dev' & Target 3, got '%s' & %d", tmFinal.store.Tasks[0].Category, tmFinal.store.Tasks[0].Target)
	}
}

func TestExportStateAndHistoryLogging(t *testing.T) {
	m := initialModel()
	m.workDuration = 25
	exportStateToFile(m)

	state, err := loadExportState()
	if err != nil {
		t.Fatalf("Failed to load export state: %v", err)
	}
	if state.Mode != "FOCUS" {
		t.Errorf("Expected mode 'FOCUS', got %s", state.Mode)
	}

	recordFocusSession(25, "dev", "Implement feature", 1, "Testing log")
	store, err := loadHistory()
	if err != nil {
		t.Fatalf("Failed to load history: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	rec := store.DailyRecords[today]
	if rec == nil {
		t.Fatalf("Expected daily record for today")
	}
}

func TestWriteJSONAtomic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "atomic-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	targetFile := filepath.Join(tmpDir, "test.json")
	data := map[string]string{"status": "ok"}

	if err := writeJSONAtomic(targetFile, data); err != nil {
		t.Fatalf("writeJSONAtomic failed: %v", err)
	}

	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Fatalf("Target file was not created by atomic write")
	}
}
