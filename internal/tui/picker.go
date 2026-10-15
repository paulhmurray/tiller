package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/paulhmurray/tiller/internal/index"
	"github.com/paulhmurray/tiller/internal/vault"
)

type pickPurpose int

const (
	pickLinkOut pickPurpose = iota // current note gets a link to the chosen one
	pickLinkIn                     // chosen note gets a link to the current one
)

const pickRows = 8

type picker struct {
	purpose pickPurpose
	target  string // key of the note being linked
	cursor  int
	items   []*index.Node
}

func (m *Model) startPick(purpose pickPurpose) tea.Cmd {
	n := m.current()
	if n == nil {
		return nil
	}
	m.pick = picker{purpose: purpose, target: n.Key}
	m.mode = modePick
	m.input.Placeholder = "type to filter; suggestions shown first"
	m.input.Prompt = "/ "
	m.input.SetValue("")
	m.input.Focus()
	m.pickRefresh()
	return textinput.Blink
}

// pickRefresh fills the candidate list: with no query, suggestions then
// everything else; with a query, title/tag substring matches.
func (m *Model) pickRefresh() {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	me := m.pick.target
	seen := map[string]bool{me: true}
	m.pick.items = m.pick.items[:0]
	add := func(n *index.Node) {
		if !seen[n.Key] {
			seen[n.Key] = true
			m.pick.items = append(m.pick.items, n)
		}
	}
	if q == "" {
		for _, n := range m.ix.Suggest(me, 5) {
			add(n)
		}
	}
	for _, n := range m.ix.Notes() {
		if q == "" || strings.Contains(strings.ToLower(n.Title), q) || strings.Contains(strings.ToLower(strings.Join(n.Tags, " ")), q) {
			add(n)
		}
	}
	if m.pick.cursor >= len(m.pick.items) {
		m.pick.cursor = max(0, len(m.pick.items)-1)
	}
}

func (m Model) updatePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case kEsc, "ctrl+c":
		m.mode = modeBrowse
		m.input.Blur()
		m.input.Prompt = "> "
		return m, nil
	case kNext, "down":
		if m.pick.cursor < len(m.pick.items)-1 {
			m.pick.cursor++
		}
		return m, nil
	case kPrev, "up":
		if m.pick.cursor > 0 {
			m.pick.cursor--
		}
		return m, nil
	case kEnter:
		m.mode = modeBrowse
		m.input.Blur()
		m.input.Prompt = "> "
		if m.pick.cursor < len(m.pick.items) {
			m.linkTo(m.pick.items[m.pick.cursor], m.pick.purpose)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.pick.cursor = 0
	m.pickRefresh()
	return m, cmd
}

// linkTo writes the link into whichever file is the source, then reloads.
func (m *Model) linkTo(other *index.Node, purpose pickPurpose) {
	me := m.ix.Get(m.pick.target)
	if me == nil {
		me = m.current()
	}
	if me == nil || other == nil {
		return
	}
	src, dst := me, other
	if purpose == pickLinkIn {
		src, dst = other, me
	}
	ok, err := vault.AppendLink(src.Path, dst.ID, dst.Title)
	switch {
	case err != nil:
		m.status = err.Error()
	case !ok:
		m.status = src.Title + " already links to " + dst.Title
	default:
		m.status = "linked: " + src.Title + " → " + dst.Title + "   (written under * Related)"
	}
	m.reload()
}

func (m Model) viewPick(sb *strings.Builder) {
	what := "link this note to…"
	if m.pick.purpose == pickLinkIn {
		what = "add a link to this note inside…"
	}
	sb.WriteString("\n" + sHeader.Render(what) + "\n  " + m.input.View() + "\n")
	cols := m.columns()
	start := 0
	if m.pick.cursor >= pickRows {
		start = m.pick.cursor - pickRows + 1
	}
	for i := start; i < len(m.pick.items) && i < start+pickRows; i++ {
		n := m.pick.items[i]
		it := m.noteItem("", n)
		if i < 5 && strings.TrimSpace(m.input.Value()) == "" && len(m.ix.Suggest(m.pick.target, 5)) > i {
			it.age = "suggested"
		}
		sb.WriteString(cols.row(it, i == m.pick.cursor) + "\n")
	}
	if len(m.pick.items) == 0 {
		sb.WriteString(sDim.Render("  no matches") + "\n")
	}
}
