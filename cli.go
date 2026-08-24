// Package main provides non-interactive CLI subcommands for status export, JSON streaming, and Markdown activity reporting.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// handleCLISubcommands checks os.Args for non-interactive subcommands.
// Returns true if a subcommand was executed (program should exit), or false to launch the TUI.
func handleCLISubcommands() bool {
	if len(os.Args) < 2 {
		return false
	}

	arg := os.Args[1]
	switch arg {
	case "status":
		return handleStatusCmd()
	case "export":
		return handleExportCmd()
	case "help", "-h", "--help":
		printUsage()
		return true
	default:
		return false
	}
}

// handleStatusCmd outputs live timer status in text or JSON format.
func handleStatusCmd() bool {
	jsonOutput := false
	for _, a := range os.Args[2:] {
		if a == "--json" || a == "-j" {
			jsonOutput = true
		}
	}

	state, err := loadExportState()
	if err != nil {
		if jsonOutput {
			fmt.Println("{}")
		} else {
			fmt.Println("🍅 Idle")
		}
		return true
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(state, "", "  ")
		fmt.Println(string(data))
		return true
	}

	// Plain text format for statuslines (e.g. zline.nvim / tmux / waybar)
	if !state.Running && state.Remaining == "" {
		fmt.Println("🍅 Paused")
		return true
	}

	statusIcon := "⏸"
	if state.Running {
		statusIcon = "▶"
	}

	taskInfo := ""
	if state.ActiveTask != "" {
		taskInfo = fmt.Sprintf(" (%s)", state.ActiveTask)
	}

	fmt.Printf("🍅 %s %s [%s]%s\n", statusIcon, state.Remaining, state.Mode, taskInfo)
	return true
}

// handleExportCmd generates a Markdown daily focus summary and journal report to stdout.
func handleExportCmd() bool {
	store, _ := loadHistory()
	taskStore, _ := loadTasks()

	today := time.Now().Format("2006-01-02")
	rec := store.DailyRecords[today]

	fmt.Printf("# Pomodoro Focus Report - %s\n\n", today)

	if rec == nil || rec.Sessions == 0 {
		fmt.Println("No focus sessions recorded today.")
		return true
	}

	hours := rec.Minutes / 60
	mins := rec.Minutes % 60
	fmt.Printf("- **Total Focus Time**: %dh %dm (%d minutes)\n", hours, mins, rec.Minutes)
	fmt.Printf("- **Completed Sessions**: %d pomodoros\n", rec.Sessions)
	fmt.Printf("- **Recorded Interruptions**: %d\n", rec.Interruptions)

	if len(rec.CategoryMins) > 0 {
		fmt.Println("\n### Category Breakdown")
		for cat, m := range rec.CategoryMins {
			fmt.Printf("- **%s**: %dm\n", cat, m)
		}
	}

	if len(rec.Logs) > 0 {
		fmt.Println("\n### Session History")
		for _, log := range rec.Logs {
			timeStr := log.Timestamp.Format("15:04")
			taskStr := log.TaskTitle
			if taskStr == "" {
				taskStr = "General Focus"
			}
			catStr := ""
			if log.Category != "" {
				catStr = fmt.Sprintf(" [%s]", log.Category)
			}
			noteStr := ""
			if log.Note != "" {
				noteStr = fmt.Sprintf(" - *%s*", log.Note)
			}
			fmt.Printf("- **%s** %s%s (%dm)%s\n", timeStr, taskStr, catStr, log.DurationMins, noteStr)
		}
	}

	if len(taskStore.Tasks) > 0 {
		fmt.Println("\n### Active Tasks")
		for _, t := range taskStore.Tasks {
			statusStr := "[ ]"
			if t.Completed {
				statusStr = "[✓]"
			}
			catStr := ""
			if t.Category != "" {
				catStr = fmt.Sprintf(" [%s]", t.Category)
			}
			fmt.Printf("- %s %s%s (🍅 %d/%d)\n", statusStr, t.Title, catStr, t.Pomodoros, t.Target)
		}
	}

	return true
}

// printUsage outputs CLI subcommand options.
func printUsage() {
	fmt.Println("Pomodoro CLI - Terminal Productivity Suite")
	fmt.Println("\nUsage:")
	fmt.Println("  pomodoro              Launch interactive TUI application")
	fmt.Println("  pomodoro status       Output live timer status string (for statuslines)")
	fmt.Println("  pomodoro status --json Output timer status in JSON format")
	fmt.Println("  pomodoro export       Output today's focus report & journal in Markdown")
	fmt.Println("  pomodoro help         Show this help message")
}
