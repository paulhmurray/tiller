package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paulhmurray/tiller/internal/index"
	"github.com/paulhmurray/tiller/internal/vault"
)

// process walks the fleeting inbox oldest first. The queue is a snapshot
// of keys; reload() drops entries that stop being fleeting.
type process struct {
	queue []string
	pos   int
	done  int
}

var ahrens = []string{
	"What does this say, in your own words?",
	"Which existing note does it connect to, and why?",
	"Is it one idea? If not, split it.",
}

func (m *Model) startProcess() {
	var keys []string
	notes := m.fleetingOldestFirst()
	for _, n := range notes {
		keys = append(keys, n.Key)
	}
	if len(keys) == 0 {
		m.status = "inbox clear: nothing fleeting to process"
		return
	}
	m.push()
	m.screen = screenProcess
	m.proc = process{queue: keys}
	m.mode = modeBrowse
	m.note = nil
	m.procSync()
}

func (m *Model) fleetingOldestFirst() []*index.Node {
	var notes []*index.Node
	for _, n := range m.ix.Notes() {
		if n.HasTag(vault.FleetingTag) {
			notes = append(notes, n)
		}
	}
	sortOldest(notes)
	return notes
}

// procSync points m.note at the queue position, skipping notes that have
// gone or been promoted, and leaves process mode when the queue is spent.
func (m *Model) procSync() {
	for m.proc.pos < len(m.proc.queue) {
		n := m.ix.Get(m.proc.queue[m.proc.pos])
		if n != nil && n.HasTag(vault.FleetingTag) {
			m.setNote(n)
			m.items = m.items[:0]
			m.cursor = 0
			return
		}
		m.proc.pos++
	}
	m.note = nil
	m.back()
	m.status = fmt.Sprintf("inbox processed: %d handled", m.proc.done)
	if left := len(m.fleetingOldestFirst()); left > 0 {
		m.status += fmt.Sprintf(", %d skipped for now", left)
	}
}

func (m *Model) procAdvance() {
	m.proc.pos++
	m.procSync()
}

func (m Model) updateProcess(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	k := msg.String()
	if len(k) == 1 && k[0] >= '1' && k[0] <= '5' {
		sug := m.ix.Suggest(m.note.Key, 5)
		i := int(k[0] - '1')
		if i < len(sug) {
			m.pick.target = m.note.Key
			m.linkTo(sug[i], pickLinkOut)
		}
		return m, nil
	}
	switch k {
	case kQuit, "ctrl+c":
		return m, tea.Quit
	case kBack, kEsc:
		m.back()
		m.status = fmt.Sprintf("stopped: %d handled", m.proc.done)
	case kEdit:
		return m, m.edit(m.note.Path, false)
	case kLinkOut:
		return m, m.startPick(pickLinkOut)
	case kLinkIn:
		return m, m.startPick(pickLinkIn)
	case kTags:
		return m, m.startTags()
	case kPromote:
		// promote() reloads, and reload on this screen advances the walk.
		if m.ix.Degree(m.note.Key) > 0 {
			m.proc.done++
		}
		m.promote()
	case kDelete:
		m.mode = modeConfirmDelete
	case kSkip:
		m.procAdvance()
	case kHelp:
		m.showKeys = !m.showKeys
		m.savePrefs()
	}
	return m, nil
}

func (m Model) viewProcess(sb *strings.Builder) {
	sb.WriteString(sDim.Render(fmt.Sprintf("processing fleeting notes, %d of %d", m.proc.pos+1, len(m.proc.queue))) + "\n\n")
	m.viewNoteHeader(sb)
	sb.WriteString("\n" + sHeader.Render("ask yourself") + "\n")
	for _, q := range ahrens {
		sb.WriteString(sDim.Render("  · "+q) + "\n")
	}
	sug := m.ix.Suggest(m.note.Key, 5)
	sb.WriteString(sHeader.Render("might connect to") + "\n")
	if len(sug) == 0 {
		sb.WriteString(sDim.Render("  nothing obvious by tag or title; l searches everything") + "\n")
	}
	cols := m.columns()
	for i, n := range sug {
		it := m.noteItem("", n)
		row := cols.row(it, false)
		sb.WriteString(sCursor.Render(fmt.Sprintf("%d", i+1)) + row[1:] + "\n")
	}
	if d := m.ix.Degree(m.note.Key); d > 0 {
		sb.WriteString("\n" + sDim.Render(fmt.Sprintf("  linked %s: p promotes it", plural(d, "time", "times"))) + "\n")
	} else {
		sb.WriteString("\n" + sWarn.Render("  not linked yet") + sDim.Render(": pick a number, or l to search, before promoting") + "\n")
	}
}
