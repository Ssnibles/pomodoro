package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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
	m.applyPreset(1) // Focus (50/10)
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

	// Cycle 1 Work -> Short Break
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

	// Short Break -> Work
	m = m.nextMode()
	if m.mode != workMode {
		t.Errorf("Expected workMode, got %v", m.mode)
	}

	// Cycle 2, 3, 4
	m = m.nextMode() // short break 2
	m = m.nextMode() // work 3
	m = m.nextMode() // short break 3
	m = m.nextMode() // work 4
	m = m.nextMode() // long break 4
	if m.completedCycles != 4 {
		t.Errorf("Expected completedCycles=4, got %d", m.completedCycles)
	}
	if m.mode != longBreakMode {
		t.Errorf("Expected longBreakMode on 4th cycle, got %v", m.mode)
	}
	if m.remaining != 15*time.Minute {
		t.Errorf("Expected remaining 15m, got %v", m.remaining)
	}
}

func TestTimerKeybindings(t *testing.T) {
	m := initialModel()
	m.presetIndex = 0

	// Toggle start/pause
	mUpdated, _ := m.updateTimer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m2 := mUpdated.(model)
	if !m2.running {
		t.Errorf("Expected timer to be running after pressing space")
	}

	// Toggle preset with 'p'
	mPresetUpdated, _ := m2.updateTimer(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m3 := mPresetUpdated.(model)
	if m3.presetIndex != 1 {
		t.Errorf("Expected preset index to increment to 1 after pressing 'p', got %d", m3.presetIndex)
	}
}
