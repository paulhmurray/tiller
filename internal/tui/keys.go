package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Key names as Bubble Tea reports them. Every switch in the package uses
// these constants and every help table is built from them, so the panel
// can never describe a key the code doesn't handle.
const (
	kQuit         = "q"
	kDown         = "j"
	kUp           = "k"
	kGoals        = "G"
	kToggle       = "x"
	kAddMilestone = "a"
	kReview       = "R"
	kBack         = "h"
	kEsc          = "esc"
	kEnter        = "enter"
	kEdit         = "e"
	kToday        = "J"
	kNew          = "n"
	kNewFrom      = "N"
	kAll          = "a"
	kFleeting     = "f"
	kOrphans      = "o"
	kMissing      = "m"
	kProcess      = "P"
	kTab          = "tab"
	kSearch       = "/"
	kLinkOut      = "l"
	kLinkIn       = "L"
	kTags         = "t"
	kPromote      = "p"
	kYank         = "y"
	kDelete       = "D"
	kSkip         = "s"
	kGraph        = "v"
	kReload       = "r"
	kHelp         = "?"
	kNext         = "ctrl+n"
	kPrev         = "ctrl+p"
	kKindFlt      = "f"
	kKindLit      = "l"
	kYes          = "y"
)

type binding struct {
	key, desc string
	primary   bool // shown on the one-line help
}

var (
	keysNav = []binding{
		{kDown + "/" + kUp, "move", true},
	}
	keysCommon = []binding{
		{kToday, "journal: headline, then write", true}, {kNew, "new note", true}, {kGraph, "graph in browser", true},
		{kReload, "reload", false}, {kHelp, "hide / show these keys", false}, {kQuit, "quit", true},
	}
	keysHome = concat(keysNav, []binding{
		{kEnter, "open note / journal day", true}, {kEdit, "edit in nvim", true},
		{kAll, "all notes", true}, {kFleeting, "fleeting inbox", true}, {kOrphans, "orphans", true}, {kMissing, "missing notes", true},
		{kProcess, "process the inbox", true}, {kGoals, "goals", true},
	}, keysCommon)
	keysGoals = concat(keysNav, []binding{
		{kEnter, "open goal", true}, {kEdit, "edit in nvim", true}, {kNew, "new goal", true}, {kProcess, "weekly review walk", true},
		{kBack, "back", true},
	}, without(keysCommon, kNew))
	keysGoal = concat(keysNav, []binding{
		{kEnter, "toggle step / open mention", true}, {kToggle, "toggle step done", false}, {kAddMilestone, "add a milestone", true},
		{kReview, "write a review line", true}, {kEdit, "edit goal in nvim", true}, {kBack, "back", true},
	}, keysCommon)
	keysNotes = concat(keysNav, []binding{
		{kEnter, "open / create missing", true}, {kEdit, "edit in nvim", true}, {kTab, "next filter", true},
		{kSearch, "search title or tag", true}, {kProcess, "process the inbox", false}, {kBack, "back", true},
	}, keysCommon)
	keysNote = concat(keysNav, []binding{
		{kEnter, "follow link / create missing", true}, {kEdit, "edit in nvim", true},
		{kLinkOut, "link this note to…", true}, {kLinkIn, "link … to this note", true}, {kNewFrom, "new note linked from here", true},
		{kTags, "edit tags", true}, {kPromote, "promote to permanent", true}, {kYank, "copy link to clipboard", false},
		{kDelete, "delete (to trash)", true}, {kBack, "back", true},
	}, keysCommon)
	keysProcess = []binding{
		{"1-5", "link to a suggestion", true}, {kEdit, "rewrite in nvim", true}, {kLinkOut, "link this note to…", true}, {kLinkIn, "link … to this note", true},
		{kTags, "edit tags", true}, {kPromote, "promote and next", true}, {kDelete, "delete and next", true}, {kSkip, "skip for now", true},
		{kBack, "stop processing", true}, {kHelp, "hide / show these keys", false}, {kQuit, "quit", false},
	}
	keysPicker = []binding{
		{"type", "filter titles and tags", true}, {kNext + "/" + kPrev + " ↑/↓", "move", true}, {kEnter, "choose", true}, {kEsc, "cancel", true},
	}
	keysNewKind = []binding{
		{kKindFlt, "fleeting: a passing thought, processed later", true}, {kKindLit, "literature: notes on something you read", true}, {kEsc, "cancel", true},
	}
	keysInput = []binding{
		{kEnter, "confirm", true}, {kEsc, "cancel", true},
	}
	keysConfirm = []binding{
		{kYes, "yes, move it to the trash", true}, {"any other key", "keep it", true},
	}
)

func without(bs []binding, key string) []binding {
	var out []binding
	for _, b := range bs {
		if b.key != key {
			out = append(out, b)
		}
	}
	return out
}

func concat(groups ...[]binding) []binding {
	var out []binding
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// bindings for the current mode, else screen.
func (m Model) bindings() []binding {
	switch m.mode {
	case modePick:
		return keysPicker
	case modeNewKind:
		return keysNewKind
	case modeNewNote, modeSearch, modeTags, modeJournal, modeGoalTitle, modeGoalWhy, modeGoalHorizon, modeGoalKeywords, modeMilestone, modeReview:
		return keysInput
	case modeConfirmDelete:
		return keysConfirm
	}
	switch m.screen {
	case screenNotes:
		return keysNotes
	case screenNote:
		return keysNote
	case screenProcess:
		return keysProcess
	case screenGoals:
		return keysGoals
	case screenGoal, screenGoalWalk:
		return keysGoal
	}
	return keysHome
}

func (m Model) helpLine() string {
	var parts []string
	for _, b := range m.bindings() {
		if b.primary {
			parts = append(parts, sCursor.Render(b.key)+" "+sDim.Render(b.desc))
		}
	}
	return strings.Join(parts, "   ")
}

// keyPanel is the which-key style table: every binding, in columns.
func (m Model) keyPanel() string {
	bs := m.bindings()
	keyW, descW := 0, 0
	for _, b := range bs {
		keyW = max(keyW, len([]rune(b.key)))
		descW = max(descW, len([]rune(b.desc)))
	}
	cellW := keyW + 2 + descW
	width := m.width
	if width <= 0 {
		width = 100
	}
	cols := max(1, min(3, (width-4)/(cellW+3)))
	rows := (len(bs) + cols - 1) / cols
	var sb strings.Builder
	sb.WriteString(sDim.Render(strings.Repeat("─", min(width-4, cols*(cellW+3)))) + "\n")
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := c*rows + r
			if i >= len(bs) {
				continue
			}
			b := bs[i]
			sb.WriteString(sCursor.Render(pad(b.key, keyW)) + "  " + sDim.Render(pad(b.desc, descW)) + "   ")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// panelRows is how many lines keyPanel will take.
func (m Model) panelRows() int {
	return strings.Count(m.keyPanel(), "\n") + 1
}

// --- preference: panel shown or hidden, per device ---

func defaultPrefDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "tiller")
	}
	return ""
}

func (m *Model) loadPrefs() {
	m.showKeys = true
	if m.prefDir == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(m.prefDir, "keys-hidden")); err == nil {
		m.showKeys = false
	}
}

func (m *Model) savePrefs() {
	if m.prefDir == "" {
		return
	}
	p := filepath.Join(m.prefDir, "keys-hidden")
	if m.showKeys {
		os.Remove(p)
		return
	}
	os.MkdirAll(m.prefDir, 0o755)
	os.WriteFile(p, []byte("delete this file to show the key panel again\n"), 0o644)
}

var _ = lipgloss.Width
