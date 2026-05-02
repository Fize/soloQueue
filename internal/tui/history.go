package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ─── History persistence ────────────────────────────────────────────────────

const maxHistory = 20

func historyFile() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".soloqueue", "history")
}

func loadHistory() []string {
	path := historyFile()
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var history []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\n\r")
		if line != "" {
			history = append(history, line)
		}
	}
	return history
}

func appendHistory(entry string) {
	if entry == "" {
		return
	}
	dir := filepath.Dir(historyFile())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	var history []string
	f, err := os.OpenFile(historyFile(), os.O_RDONLY, 0644)
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimRight(scanner.Text(), "\n\r")
			if line != "" {
				history = append(history, line)
			}
		}
		f.Close()
	}
	if len(history) > 0 && history[len(history)-1] == entry {
		return
	}
	history = append(history, entry)
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
	file, err := os.OpenFile(historyFile(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, h := range history {
		fmt.Fprintln(writer, h)
	}
	writer.Flush()
}

// ─── History navigation ─────────────────────────────────────────────────────

func (m *model) addHistory(line string) {
	if line == "" || (len(m.history) > 0 && m.history[len(m.history)-1] == line) {
		return
	}
	m.history = append(m.history, line)
	m.historyIdx = 0
	m.historyDraft = ""
	appendHistory(line)
}

func (m *model) navHistory(dir int) {
	if len(m.history) == 0 || m.isGenerating || m.confirmState != nil {
		return
	}
	if m.historyIdx == 0 && dir < 0 {
		m.historyDraft = m.textArea.Value()
	}
	newIdx := m.historyIdx - dir
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx > len(m.history) {
		newIdx = len(m.history)
	}
	if newIdx == m.historyIdx {
		return
	}
	m.historyIdx = newIdx
	if m.historyIdx == 0 {
		m.textArea.SetValue(m.historyDraft)
	} else {
		m.textArea.SetValue(m.history[len(m.history)-m.historyIdx])
	}
	m.textArea.CursorEnd()
}
