package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func press(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	panic("unknown key " + s)
}

func compose(t *testing.T) composeModel {
	t.Helper()
	draft, _, _ := composeStart("the quoted text", 3)
	return draft
}

// Finishing cannot be a chord: a terminal takes ctrl+s as flow control and the
// shell claims ctrl+t and ctrl+d, so the keys simply never arrive.
func TestEscapeLeavesTheTextAndSingleKeysDecide(t *testing.T) {
	m := compose(t)
	var model tea.Model = m
	for _, r := range "why this number?" {
		model, _ = model.Update(press(string(r)))
	}
	model, _ = model.Update(press("esc"))

	view := model.View()
	if !strings.Contains(view, "s save") || !strings.Contains(view, "q discard") {
		t.Errorf("the command line must say what the keys do:\n%s", view)
	}

	saved, _ := model.Update(press("s"))
	result := saved.(composeModel)
	if !result.saved {
		t.Error("s must save")
	}
	if got := strings.TrimSpace(result.body.Value()); got != "why this number?" {
		t.Errorf("text: got %q", got)
	}
}

// Nothing should be recorded when the reader changes their mind.
func TestDiscardKeepsNothing(t *testing.T) {
	var model tea.Model = compose(t)
	model, _ = model.Update(press("x"))
	model, _ = model.Update(press("esc"))
	model, _ = model.Update(press("q"))

	if model.(composeModel).saved {
		t.Error("q must discard")
	}
}

// Typing must resume without losing what was written.
func TestKeepTypingReturnsToTheText(t *testing.T) {
	var model tea.Model = compose(t)
	for _, r := range "half" {
		model, _ = model.Update(press(string(r)))
	}
	model, _ = model.Update(press("esc"))
	model, _ = model.Update(press("i"))
	for _, r := range " done" {
		model, _ = model.Update(press(string(r)))
	}

	if got := strings.TrimSpace(model.(composeModel).body.Value()); got != "half done" {
		t.Errorf("text after resuming: got %q, want %q", got, "half done")
	}
}

// Offering a replacement is what makes a comment a suggestion, so opening the
// field sets the type rather than leaving the two to disagree.
func TestReplacementFieldSetsTheType(t *testing.T) {
	var model tea.Model = compose(t)
	model, _ = model.Update(press("tab"))

	m := model.(composeModel)
	if commentTypes[m.typeAt] != "suggestion" {
		t.Errorf("type after opening the replacement: %q", commentTypes[m.typeAt])
	}
}
