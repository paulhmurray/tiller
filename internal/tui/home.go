// Package tui is the tiller front end: a home screen, a notes browser with
// filters, and a note view with links, backlinks and the maintenance
// actions (promote, yank a link, delete). Editing happens in $EDITOR.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/paulhmurray/tiller/internal/graph"
	"github.com/paulhmurray/tiller/internal/index"
	"github.com/paulhmurray/tiller/internal/vault"
)

const (
	recentDays   = 7
	recentNotes  = 10
	previewLines = 8
)

const banner = ` _   _ _ _
| |_(_) | | ___ _ __
| __| | | |/ _ \ '__|
| |_| | | |  __/ |
 \__|_|_|_|\___|_|`

var (
	accent = lipgloss.AdaptiveColor{Light: "#5c1f2e", Dark: "#d98fa0"}
	dim    = lipgloss.AdaptiveColor{Light: "#8a877f", Dark: "#7a776f"}
	ink    = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#e8e6e1"}
	amber  = lipgloss.AdaptiveColor{Light: "#a15c00", Dark: "#f0b35b"}

	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	sHeader  = lipgloss.NewStyle().Bold(true).Foreground(ink).MarginTop(1)
	sDim     = lipgloss.NewStyle().Foreground(dim)
	sLabel   = lipgloss.NewStyle().Foreground(dim).Width(10)
	sValue   = lipgloss.NewStyle().Foreground(ink)
	sCursor  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	sTag     = lipgloss.NewStyle().Foreground(accent)
	sHelp    = lipgloss.NewStyle().Foreground(dim).MarginTop(1)
	sWarn    = lipgloss.NewStyle().Foreground(amber)
	sPreview = lipgloss.NewStyle().Foreground(ink).PaddingLeft(2)
	sQuote   = lipgloss.NewStyle().Foreground(ink).Italic(true)
	sPadding = lipgloss.NewStyle().Padding(1, 2)
)

type screen int

const (
	screenHome screen = iota
	screenNotes
	screenNote
	screenProcess
	screenGoals
	screenGoal
	screenGoalWalk
	screenBooks
	screenBook
	screenGuide
)

type filter int

const (
	filterAll filter = iota
	filterFleeting
	filterOrphans
	filterMissing
)

func (f filter) String() string {
	return [...]string{"all notes", "fleeting", "orphans", "missing"}[f]
}

type mode int

const (
	modeBrowse mode = iota
	modeNewKind
	modeNewNote
	modeSearch
	modeTags
	modeConfirmDelete
	modePick
	modeJournal
	modeGoalTitle
	modeGoalWhy
	modeGoalHorizon
	modeGoalKeywords
	modeMilestone
	modeReview
	modeChapter
	modeBookTitle
	modeBookTarget
)

type rowKind int

const (
	rowNote rowKind = iota
	rowDay
	rowMissing
	rowGoal
	rowMilestone
	rowReview
	rowBook
	rowChapter
)

// item is one cursor row on any screen.
type item struct {
	section string // header printed when it changes
	kind    rowKind
	key     string // index key of the target node; "" for missing targets
	path    string // file to edit for this row
	label   string // title, date or missing slug
	tags    []string
	degree  int // resolved links; 0 means orphan for notes
	words   int
	age     string
	missing string // missing-target label when kind == rowMissing; "quiet" flag for goals
	wanted  int    // referrers, for missing rows
	isDay   bool
	line    int    // goals.org line for goal rows, milestones and reviews
	done    bool   // milestone state
	horizon string // goal rows
}

// frame is a place to return to.
type frame struct {
	screen screen
	note   string
	goal   string
	book   string
	filter filter
	cursor int
	query  string
}

type Model struct {
	v        vault.Vault
	editor   string
	trashDir string
	// clip receives text for the clipboard; swapped in tests.
	clip func(string) error
	now  time.Time

	days   []vault.Day
	ix     *index.Index
	today  *vault.Day
	onThis []vault.Day

	screen  screen
	mode    mode
	filter  filter
	query   string
	note    *index.Node
	preview []string
	stack   []frame
	items   []item
	cursor  int
	input   textinput.Model
	status  string
	err     error
	width   int
	height  int

	pick     picker
	proc     process
	newKind  vault.Kind
	linkFrom string // key of the note a new note should link back to
	showKeys bool   // which-key panel visible
	prefDir  string

	noteRows, dayRows int // home list sizes after fitting to the window height

	goals  []vault.Goal
	goal   *vault.Goal
	goalID string
	draft  goalDraft
	walk   goalWalk

	books    []vault.Book
	book     *vault.Book
	bookSlug string
	tipSeed  int
}

func New(v vault.Vault, editor string) Model {
	ti := textinput.New()
	ti.CharLimit = 120
	ti.Width = 50
	m := Model{v: v, editor: editor, input: ti, clip: wlCopy, prefDir: defaultPrefDir(), noteRows: recentNotes, dayRows: recentDays}
	if dir, err := os.UserCacheDir(); err == nil {
		m.trashDir = filepath.Join(dir, "tiller", "trash")
	}
	m.loadPrefs()
	m.reload()
	return m
}

// current is the note in focus on the note and process screens.
func (m *Model) current() *index.Node {
	if m.screen == screenNote || m.screen == screenProcess {
		return m.note
	}
	return nil
}

func sortOldest(notes []*index.Node) {
	sort.SliceStable(notes, func(i, j int) bool { return stamp(notes[i]).Before(stamp(notes[j])) })
}

func wlCopy(text string) error {
	bin, err := exec.LookPath("wl-copy")
	if err != nil {
		return fmt.Errorf("wl-copy not found")
	}
	c := exec.Command(bin)
	c.Stdin = strings.NewReader(text)
	return c.Run()
}

func (m *Model) reload() {
	m.now = time.Now()
	m.err = nil
	days, err := m.v.Days()
	if err != nil {
		m.err = err
	}
	m.days = days
	ix, err := index.Build(m.v)
	if err != nil {
		m.err = err
		ix = &index.Index{Nodes: map[string]*index.Node{}}
	}
	m.ix = ix
	m.loadGoals()
	m.loadBooks()
	m.onThis = vault.OnThisDay(days, m.now)
	m.today = nil
	todayKey := m.now.Format("2006-01-02")
	for i := range days {
		if days[i].Date.Format("2006-01-02") == todayKey {
			m.today = &days[i]
		}
	}
	switch m.screen {
	case screenNote:
		if n := m.ix.Get(m.note.Key); n != nil {
			m.setNote(n)
		} else {
			m.back()
			return
		}
	case screenProcess:
		m.procSync()
		return
	case screenGoal, screenGoalWalk:
		if m.goal == nil {
			m.back()
			return
		}
	case screenBook:
		if m.book == nil {
			m.back()
			return
		}
	}
	m.buildItems()
}

func (m *Model) setNote(n *index.Node) {
	m.note = n
	m.preview = vault.Preview(n.Path, previewLines)
}

func (m *Model) noteItem(section string, n *index.Node) item {
	if n.Kind == index.KindDay {
		return m.dayItem(section, n.Date, n.Path, n.Title, n.Words)
	}
	return item{section: section, kind: rowNote, key: n.Key, path: n.Path, label: n.Title,
		tags: n.Tags, degree: m.ix.Degree(n.Key), words: n.Words, age: ago(n.ModTime, m.now)}
}

func (m *Model) dayItem(section string, date time.Time, path, first string, words int) item {
	return item{section: section, kind: rowDay, isDay: true, key: "day:" + date.Format("2006-01-02"), path: path,
		label: date.Format("Mon 02 Jan"), tags: []string{first}, words: words}
}

func (m *Model) buildItems() {
	m.items = m.items[:0]
	switch m.screen {
	case screenHome:
		for i := range m.books {
			m.items = append(m.items, m.bookItem("books", &m.books[i]))
		}
		for i, g := range m.activeGoals() {
			if i >= 5 {
				break
			}
			m.items = append(m.items, m.goalItem("goals", g))
		}
		m.noteRows, m.dayRows = recentNotes, recentDays
		m.appendHomeLists()
		m.fitHome()
	case screenGoals, screenGoal, screenGoalWalk:
		m.buildGoalItems()
	case screenBooks, screenBook:
		m.buildBookItems()
	case screenNotes:
		q := strings.ToLower(strings.TrimSpace(m.query))
		match := func(n *index.Node) bool {
			if q == "" {
				return true
			}
			if strings.Contains(strings.ToLower(n.Title), q) {
				return true
			}
			for _, t := range n.Tags {
				if strings.Contains(strings.ToLower(t), q) {
					return true
				}
			}
			return false
		}
		section := m.filter.String()
		if m.filter == filterMissing {
			for _, miss := range m.ix.Missing() {
				label := strings.TrimPrefix(miss, "id:")
				if q != "" && !strings.Contains(strings.ToLower(label), q) {
					continue
				}
				m.items = append(m.items, item{section: section, kind: rowMissing, missing: miss, label: label, wanted: len(m.ix.Dangling[miss])})
			}
			break
		}
		var notes []*index.Node
		switch m.filter {
		case filterAll:
			notes = m.ix.Notes()
		case filterFleeting:
			for _, n := range m.ix.Notes() {
				if n.HasTag(vault.FleetingTag) {
					notes = append(notes, n)
				}
			}
			sortOldest(notes) // processing order
		case filterOrphans:
			notes = m.ix.Orphans()
		}
		for _, n := range notes {
			if match(n) {
				m.items = append(m.items, m.noteItem(section, n))
			}
		}
	case screenNote:
		n := m.note
		for _, k := range m.ix.Outgoing[n.Key] {
			m.items = append(m.items, m.noteItem("links to", m.ix.Get(k)))
		}
		for _, k := range m.ix.Backlinks[n.Key] {
			m.items = append(m.items, m.noteItem("linked from", m.ix.Get(k)))
		}
		for _, miss := range m.ix.MissingFrom(n.Key) {
			m.items = append(m.items, item{section: "missing", kind: rowMissing, missing: miss, label: strings.TrimPrefix(miss, "id:"), wanted: len(m.ix.Dangling[miss])})
		}
	}
	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}
}

func (m *Model) appendHomeLists() {
	for i := len(m.days) - 1; i >= 0 && i >= len(m.days)-m.dayRows; i-- {
		d := m.days[i]
		first := ""
		if len(d.Entries) > 0 {
			first = d.Entries[0].Title
		}
		m.items = append(m.items, m.dayItem("recent journal", d.Date, d.Path, first, d.Words))
	}
	for i, n := range m.ix.Notes() {
		if i >= m.noteRows {
			break
		}
		m.items = append(m.items, m.noteItem("recent notes", n))
	}
}

// fitHome shortens the journal and notes lists until the whole home screen,
// key panel included, fits the window. Each list keeps at least three rows;
// if that is still too tall View falls back to the wrapped one-line help.
func (m *Model) fitHome() {
	if !m.showKeys || m.height <= 0 {
		return
	}
	for {
		over := m.homeRows() + m.panelRows() - m.height
		if over <= 0 {
			return
		}
		switch {
		case m.noteRows > 3:
			m.noteRows = max(3, m.noteRows-over)
		case m.dayRows > 3:
			m.dayRows = max(3, m.dayRows-over)
		default:
			return
		}
		m.items = m.items[:0]
		for i := range m.books {
			m.items = append(m.items, m.bookItem("books", &m.books[i]))
		}
		for _, g := range m.activeGoals() {
			if len(m.items) >= 5+len(m.books) {
				break
			}
			m.items = append(m.items, m.goalItem("goals", g))
		}
		m.appendHomeLists()
	}
}

// homeRows is the height of everything above the key panel, padding included.
func (m Model) homeRows() int {
	return strings.Count(m.renderTop(), "\n") + strings.Count(m.renderRows(), "\n") + 2 + 1 + 2 // tip line
}

func stamp(n *index.Node) time.Time {
	if !n.Created.IsZero() {
		return n.Created
	}
	return n.ModTime
}

func (m Model) Init() tea.Cmd { return nil }

type editDone struct{ err error }

func (m Model) edit(path string, atEnd bool) tea.Cmd {
	args := []string{}
	if atEnd {
		args = append(args, "+$")
	}
	args = append(args, path)
	c := exec.Command(m.editor, args...)
	c.Dir = m.v.Root
	return tea.ExecProcess(c, func(err error) tea.Msg { return editDone{err} })
}

// editAt opens the file with the cursor on a 1-based line.
func (m Model) editAt(path string, line int) tea.Cmd {
	c := exec.Command(m.editor, fmt.Sprintf("+%d", line), path)
	c.Dir = m.v.Root
	return tea.ExecProcess(c, func(err error) tea.Msg { return editDone{err} })
}

// push records the current place before navigating away.
func (m *Model) push() {
	f := frame{screen: m.screen, filter: m.filter, cursor: m.cursor, query: m.query}
	if m.note != nil {
		f.note = m.note.Key
	}
	if m.goal != nil {
		f.goal = m.goal.ID
	}
	if m.book != nil {
		f.book = m.book.Slug
	}
	m.stack = append(m.stack, f)
	m.tipSeed++
}

func (m *Model) back() {
	for len(m.stack) > 0 {
		f := m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
		if f.screen == screenProcess || f.screen == screenGoalWalk {
			continue // never return into a half-finished walk
		}
		if f.screen == screenGoal {
			g := m.findGoal(f.goal)
			if g == nil {
				continue
			}
			m.goal, m.goalID = g, g.ID
		} else {
			m.goal, m.goalID = nil, ""
		}
		if f.screen == screenBook {
			b := m.findBook(f.book)
			if b == nil {
				continue
			}
			m.book, m.bookSlug = b, b.Slug
		} else {
			m.book, m.bookSlug = nil, ""
		}
		m.tipSeed++
		if f.screen == screenNote {
			n := m.ix.Get(f.note)
			if n == nil {
				continue
			}
			m.setNote(n)
		} else {
			m.note = nil
		}
		m.screen, m.filter, m.query, m.cursor = f.screen, f.filter, f.query, f.cursor
		m.buildItems()
		return
	}
	m.screen, m.note, m.goal, m.goalID, m.book, m.bookSlug, m.cursor, m.query = screenHome, nil, nil, "", nil, "", 0, ""
	m.tipSeed++
	m.buildItems()
}

func (m *Model) openNote(key string) {
	n := m.ix.Get(key)
	if n == nil || n.Kind != index.KindNote {
		return
	}
	m.push()
	m.screen, m.cursor = screenNote, 0
	m.setNote(n)
	m.buildItems()
}

func (m *Model) openNotes(f filter) {
	m.push()
	m.screen, m.filter, m.query, m.cursor, m.note = screenNotes, f, "", 0, nil
	m.buildItems()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.screen == screenHome {
			m.buildItems()
		}
		return m, nil
	case editDone:
		m.reload()
		m.status = ""
		if msg.err != nil {
			m.status = "editor: " + msg.err.Error()
		}
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case modeNewKind:
			return m.updateNewKind(msg)
		case modeNewNote:
			return m.updateNewNote(msg)
		case modeSearch:
			return m.updateSearch(msg)
		case modeTags:
			return m.updateTags(msg)
		case modeJournal:
			return m.updateJournal(msg)
		case modeGoalTitle, modeGoalWhy, modeGoalHorizon, modeGoalKeywords:
			return m.updateNewGoal(msg)
		case modeMilestone, modeReview:
			return m.updateGoalInput(msg)
		case modeChapter, modeBookTitle, modeBookTarget:
			return m.updateBookInput(msg)
		case modeConfirmDelete:
			return m.updateConfirmDelete(msg)
		case modePick:
			return m.updatePick(msg)
		}
		if m.screen == screenProcess {
			return m.updateProcess(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m Model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cur *item
	if m.cursor < len(m.items) {
		cur = &m.items[m.cursor]
	}
	m.status = ""
	switch msg.String() {
	case kQuit, "ctrl+c":
		return m, tea.Quit
	case kDown, "down":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case kUp, "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case kGoals:
		if m.screen == screenHome {
			m.openGoals()
		}
	case kBooks:
		if m.screen == screenHome {
			if len(m.books) == 0 {
				m.push()
				m.screen, m.cursor = screenBooks, 0
				m.buildItems()
			} else {
				m.openBooks()
			}
		}
	case kGuide:
		if m.screen != screenGuide {
			m.push()
			m.screen, m.cursor = screenGuide, 0
			m.items = m.items[:0]
		}
	case kToggle:
		if cur != nil && cur.kind == rowMilestone {
			if err := m.v.ToggleMilestone(cur.line); err != nil {
				m.status = err.Error()
			} else {
				m.reload()
			}
		}
	case kReview:
		if m.screen == screenGoal {
			return m, m.startReview()
		}
	case kBack, kEsc, "backspace":
		if m.screen != screenHome {
			m.back()
		}
	case kEnter:
		if cur == nil {
			return m, nil
		}
		switch {
		case cur.kind == rowBook:
			m.openBook(cur.key)
		case cur.kind == rowChapter:
			if c := m.chapterAt(cur); c != nil {
				if err := m.v.EnsureChapterFile(*m.book, *c); err != nil {
					m.status = err.Error()
					return m, nil
				}
				return m, m.edit(c.File, true)
			}
		case cur.kind == rowGoal:
			m.openGoal(cur.key)
		case cur.kind == rowMilestone:
			if err := m.v.ToggleMilestone(cur.line); err == nil {
				m.reload()
			}
		case cur.kind == rowReview:
			return m, m.editAt(cur.path, cur.line+1)
		case cur.missing != "":
			title := strings.ReplaceAll(strings.TrimPrefix(cur.missing, "id:"), "-", " ")
			p, err := m.v.NewNote(title, nil, time.Now())
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			return m, m.edit(p, true)
		case cur.isDay:
			return m, m.edit(cur.path, false)
		default:
			m.openNote(cur.key)
		}
	case kEdit:
		switch {
		case m.screen == screenNote:
			return m, m.edit(m.note.Path, false)
		case m.screen == screenGoal:
			return m, m.editAt(m.v.GoalsPath(), m.goal.Line+1)
		case cur != nil && cur.kind == rowGoal:
			return m, m.editAt(cur.path, cur.line+1)
		case cur != nil && cur.kind == rowBook:
			return m, m.edit(cur.path, false)
		case m.screen == screenBook:
			return m, m.edit(m.book.Outline, false)
		case cur != nil && cur.path != "":
			return m, m.edit(cur.path, false)
		}
	case kAll: // also kAddMilestone, kAddChapter
		switch m.screen {
		case screenHome:
			m.openNotes(filterAll)
		case screenGoal:
			return m, m.startMilestone()
		case screenBook:
			return m, m.startChapter()
		}
	case kFleeting:
		if m.screen == screenHome {
			m.openNotes(filterFleeting)
		}
	case kOrphans: // also kOutline
		switch m.screen {
		case screenHome:
			m.openNotes(filterOrphans)
		case screenBook:
			if c := m.chapterAt(cur); c != nil {
				return m, m.editAt(m.book.Outline, c.Line+1)
			}
			return m, m.edit(m.book.Outline, false)
		}
	case kSkip: // kStage on the book screen
		if m.screen == screenBook {
			if c := m.chapterAt(cur); c != nil {
				next, err := m.v.CycleStage(*m.book, *c)
				if err != nil {
					m.status = err.Error()
				} else {
					m.reload()
					m.status = c.Title + " → " + strings.ToLower(next)
				}
			}
		}
	case kMissing:
		if m.screen == screenHome {
			m.openNotes(filterMissing)
		}
	case kProcess:
		switch m.screen {
		case screenHome, screenNotes:
			m.startProcess()
		case screenGoals:
			return m, m.startWalk()
		}
	case kTab:
		if m.screen == screenNotes {
			m.filter = (m.filter + 1) % 4
			m.cursor = 0
			m.buildItems()
		}
	case kSearch:
		if m.screen == screenNotes {
			m.mode = modeSearch
			m.input.Placeholder = "title or tag"
			m.input.SetValue(m.query)
			m.input.Focus()
			return m, textinput.Blink
		}
	case kLinkOut:
		if m.screen == screenNote {
			return m, m.startPick(pickLinkOut)
		}
	case kLinkIn:
		if m.screen == screenNote {
			return m, m.startPick(pickLinkIn)
		}
	case kTags:
		if m.screen == screenNote {
			return m, m.startTags()
		}
	case kPromote:
		if m.screen == screenNote {
			m.promote()
		}
	case kYank:
		if m.screen == screenNote {
			link := vault.RenderLink(m.note.ID, m.note.Title)
			if err := m.clip(link); err != nil {
				m.status = "clipboard: " + err.Error()
			} else {
				m.status = "copied " + link + `   paste in nvim with "+p`
			}
		}
	case kDelete:
		if m.screen == screenNote {
			m.mode = modeConfirmDelete
		}
	case kToday:
		return m, m.startJournal()
	case kNew:
		if m.screen == screenGoals {
			return m, m.startNewGoal()
		}
		if m.screen == screenBooks {
			return m, m.startNewBook()
		}
		m.linkFrom = ""
		return m, m.startNew()
	case kNewFrom:
		if m.screen == screenNote {
			m.linkFrom = m.note.Key
			return m, m.startNew()
		}
	case kGraph:
		path, err := graph.Generate(m.ix, "")
		if err == nil {
			err = graph.Open(path)
		}
		if err != nil {
			m.status = err.Error()
		} else {
			m.status = "graph opened in browser"
		}
	case kReload:
		m.reload()
		m.status = "reloaded"
	case kHelp:
		m.showKeys = !m.showKeys
		m.savePrefs()
	}
	return m, nil
}

// noteContext is what a new note inherits from where n was pressed: the
// book's tag and a link to its hub from a book screen, or a link back to
// the note on screen when N was used.
func (m *Model) noteContext() (tags []string, links []vault.LinkTo) {
	if from := m.ix.Get(m.linkFrom); from != nil {
		links = append(links, vault.LinkTo{ID: from.ID, Title: from.Title})
	}
	if m.book != nil {
		tags = append(tags, m.book.Tag)
		if hub := m.ix.Get(m.book.HubID()); hub != nil && m.linkFrom == "" {
			links = append(links, vault.LinkTo{ID: hub.ID, Title: hub.Title})
		}
	}
	return tags, links
}

// --- new note: kind, then title ---

func (m *Model) startNew() tea.Cmd {
	m.mode = modeNewKind
	return nil
}

func (m Model) updateNewKind(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case kKindFlt:
		m.newKind = vault.KindFleeting
	case kKindLit:
		m.newKind = vault.KindLiterature
	default:
		m.mode = modeBrowse
		return m, nil
	}
	m.mode = modeNewNote
	m.input.Placeholder = "title"
	m.input.SetValue("")
	m.input.Focus()
	return m, textinput.Blink
}

// --- journal: a headline first, then nvim ---

// journalHints rotate so the prompt reads as an invitation, not a field.
var journalHints = []string{
	"a place, a feeling, or what happened",
	"where are you, and how is it?",
	"one word for today would do",
	"what's on your mind?",
	"who were you with?",
}

func (m *Model) startJournal() tea.Cmd {
	m.mode = modeJournal
	m.input.Placeholder = journalHints[m.now.YearDay()%len(journalHints)]
	m.input.Prompt = "* "
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

func (m Model) updateJournal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case kEsc, "ctrl+c":
		m.mode = modeBrowse
		m.input.Blur()
		m.input.Prompt = "> "
		return m, nil
	case kEnter:
		m.mode = modeBrowse
		m.input.Blur()
		m.input.Prompt = "> "
		head := strings.TrimSpace(m.input.Value())
		var p string
		var err error
		if head == "" {
			p, _, err = m.v.EnsureDay(m.now)
		} else {
			p, err = m.v.Jot(m.now, head)
		}
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		return m, m.edit(p, true)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// --- tags ---

func (m *Model) startTags() tea.Cmd {
	m.mode = modeTags
	m.input.Placeholder = ":tag:another:"
	m.input.SetValue(":" + strings.Join(m.note.Tags, ":") + ":")
	m.input.Focus()
	m.input.CursorEnd()
	return textinput.Blink
}

func (m Model) updateTags(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case kEsc, "ctrl+c":
		m.mode = modeBrowse
		m.input.Blur()
		return m, nil
	case kEnter:
		m.mode = modeBrowse
		m.input.Blur()
		tags := splitTags(m.input.Value())
		if err := vault.SetTags(m.note.Path, tags); err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.reload()
		m.status = "tags saved"
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func splitTags(s string) []string {
	var out []string
	for _, t := range strings.FieldsFunc(s, func(r rune) bool { return r == ':' || r == ' ' || r == ',' }) {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// promote drops the fleeting tag, but only from a note that is linked:
// an unlinked permanent note is just a better-dressed orphan.
func (m *Model) promote() {
	n := m.note
	if !n.HasTag(vault.FleetingTag) {
		m.status = "already permanent"
		return
	}
	if m.ix.Degree(n.Key) == 0 {
		m.status = "orphan: link it to something first (y copies a link to this note), or edit the tag in nvim"
		return
	}
	if err := vault.Promote(n.Path); err != nil {
		m.status = err.Error()
		return
	}
	m.reload()
	if m.screen == screenProcess || m.status == "" {
		m.status = "promoted: " + n.Title + " is now permanent"
	}
}

func (m Model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = modeBrowse
	if msg.String() != "y" {
		m.status = "kept"
		return m, nil
	}
	n := m.note
	dst, err := vault.Trash(n.Path, m.trashDir, time.Now())
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	if m.screen == screenProcess {
		m.proc.done++
	}
	m.reload() // note is gone; reload pops back or advances
	m.status = "deleted " + n.Title + "  (in " + dst + ")"
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeBrowse
		m.input.Blur()
		m.query = ""
		m.cursor = 0
		m.buildItems()
		return m, nil
	case "enter":
		m.mode = modeBrowse
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.query = m.input.Value()
	m.cursor = 0
	m.buildItems()
	return m, cmd
}

func (m Model) updateNewNote(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeBrowse
		m.input.Blur()
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.input.Value())
		m.mode = modeBrowse
		m.input.Blur()
		if title == "" {
			return m, nil
		}
		tags, links := m.noteContext()
		p, err := m.v.Create(title, m.newKind, tags, links, time.Now())
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		// Land on the new note's view, so t, l and friends apply when nvim
		// closes, and h goes back to where n was pressed.
		m.reload()
		m.openNote(strings.ToLower(strings.TrimSuffix(filepath.Base(p), ".org")))
		return m, m.edit(p, true)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// renderTop is the banner or title line plus the screen's header block.
func (m Model) renderTop() string {
	var b strings.Builder
	date := sDim.Render(m.now.Format("Monday 2 January 2006"))
	useBanner := m.screen == screenHome && m.width >= 60 && m.height >= 28
	if useBanner {
		// Quote sits to the right of the logo, vertically centred on it;
		// the date stays on the top row at the right edge.
		lines := strings.Split(banner, "\n")
		widest := 0
		for _, l := range lines {
			widest = max(widest, len(l))
		}
		text, who := QuoteFor(m.now)
		gutter := 4
		qw := m.width - 4 - widest - gutter - lipgloss.Width(date) - 2
		qlines := wrapBalanced(text, max(24, min(70, qw)))
		qlines = append(qlines, "— "+who)
		// The logo's top row is just the ascenders, so the visual centre
		// sits a row lower than the arithmetic one: drop the block by one.
		top := (len(lines)-len(qlines))/2 + 1
		if top+len(qlines) > len(lines) {
			top = len(lines) - len(qlines)
		}
		if top < 0 {
			top = 0
		}
		rows := max(len(lines), top+len(qlines))
		for i := 0; i < rows; i++ {
			left := ""
			if i < len(lines) {
				left = lines[i]
			}
			b.WriteString(sTitle.Render(pad(left, widest)) + strings.Repeat(" ", gutter))
			right := ""
			if j := i - top; j >= 0 && j < len(qlines) {
				if j == len(qlines)-1 {
					right = sDim.Render(qlines[j])
				} else {
					right = sQuote.Render(qlines[j])
				}
			}
			if i == 0 {
				fill := max(1, m.width-4-widest-gutter-lipgloss.Width(right)-lipgloss.Width(date))
				b.WriteString(right + strings.Repeat(" ", fill) + date)
			} else {
				b.WriteString(right)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	} else {
		title := sTitle.Render("tiller")
		switch m.screen {
		case screenNote:
			title += sDim.Render("  ›  ") + sValue.Render(m.note.Title)
		case screenNotes:
			title += sDim.Render("  ›  ") + sValue.Render(m.filter.String())
		case screenProcess:
			title += sDim.Render("  ›  ") + sValue.Render("process")
		case screenGoals:
			title += sDim.Render("  ›  ") + sValue.Render("goals")
		case screenGoal, screenGoalWalk:
			title += sDim.Render("  ›  ") + sValue.Render(m.goal.Title)
		case screenBooks:
			title += sDim.Render("  ›  ") + sValue.Render("books")
		case screenBook:
			title += sDim.Render("  ›  ") + sValue.Render(m.book.Title)
		case screenGuide:
			title += sDim.Render("  ›  ") + sValue.Render("how tiller fits together")
		}
		gap := max(1, m.width-4-lipgloss.Width(title)-lipgloss.Width(date))
		b.WriteString(title + strings.Repeat(" ", gap) + date + "\n\n")
	}

	switch m.screen {
	case screenHome:
		m.viewHomeStats(&b)
	case screenNote:
		m.viewNoteHeader(&b)
	case screenNotes:
		m.viewNotesHeader(&b)
	case screenProcess:
		m.viewProcess(&b)
	case screenGoals:
		m.viewGoalsHeader(&b)
	case screenGoal, screenGoalWalk:
		m.viewGoalHeader(&b)
	case screenBooks:
		m.viewBooksHeader(&b)
	case screenBook:
		m.viewBookHeader(&b)
	case screenGuide:
		m.viewGuide(&b)
	}
	return b.String()
}

// renderRows is the cursor list with its section and column headers.
func (m Model) renderRows() string {
	var b strings.Builder
	cols := m.columns()
	section := ""
	for i, it := range m.items {
		if it.section != section {
			section = it.section
			b.WriteString(sHeader.Render(section) + "\n")
			b.WriteString(cols.header(it.kind) + "\n")
		}
		b.WriteString(cols.row(it, i == m.cursor) + "\n")
	}
	return b.String()
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.renderTop())
	b.WriteString(m.renderRows())
	switch {
	case m.screen == screenNote && len(m.items) == 0:
		b.WriteString("\n" + sWarn.Render("no links in or out") + sDim.Render("  an orphan; y copies a link to paste into a related note") + "\n")
	case m.screen == screenNotes && len(m.items) == 0:
		b.WriteString("\n" + sDim.Render("  nothing here") + "\n")
	case m.screen == screenGoals && len(m.items) == 0:
		b.WriteString("\n" + sDim.Render("  no goals yet. n starts one: what you want, why, by when, and the words that mean you touched it") + "\n")
	case m.screen == screenBooks && len(m.items) == 0:
		b.WriteString("\n" + sDim.Render("  no books yet. n starts one: a title and a word target") + "\n")
	case m.screen == screenBook && len(m.items) == 0:
		b.WriteString("\n" + sDim.Render("  no chapters yet. a adds the first; its prose file is created beside the outline") + "\n")
	}

	switch m.mode {
	case modeNewKind:
		b.WriteString("\n" + sHeader.Render("new note: what kind?") + "\n")
		for _, k := range keysNewKind[:2] {
			b.WriteString("  " + sCursor.Render(k.key) + "  " + sValue.Render(k.desc) + "\n")
		}
	case modeNewNote:
		head := "new " + m.newKind.String() + " note"
		if from := m.ix.Get(m.linkFrom); from != nil {
			head += sDim.Render(", linked from " + from.Title)
		}
		if m.book != nil {
			head += sDim.Render(", tagged " + m.book.Tag + " and linked to the " + m.book.Title + " hub")
		}
		b.WriteString("\n" + sHeader.Render(head) + "\n  " + m.input.View() + "\n")
	case modeSearch:
		b.WriteString("\n  " + sDim.Render("/ ") + m.input.View() + "\n")
	case modeJournal:
		b.WriteString("\n" + sHeader.Render("today's journal") + sDim.Render("  headline for this entry; enter with nothing just opens the file") + "\n")
		if m.today != nil {
			for _, e := range m.today.Entries {
				b.WriteString(sDim.Render("  * "+e.Title) + "\n")
			}
		}
		b.WriteString("  " + m.input.View() + "\n")
	case modeGoalTitle, modeGoalWhy, modeGoalHorizon, modeGoalKeywords:
		step := map[mode]string{modeGoalTitle: "1 of 4: the goal", modeGoalWhy: "2 of 4: why", modeGoalHorizon: "3 of 4: by when", modeGoalKeywords: "4 of 4: journal keywords"}[m.mode]
		b.WriteString("\n" + sHeader.Render("new goal, "+step) + "\n")
		if m.draft.title != "" {
			b.WriteString(sDim.Render("  "+m.draft.title) + "\n")
		}
		b.WriteString("  " + m.input.View() + "\n")
	case modeMilestone:
		b.WriteString("\n" + sHeader.Render("new milestone") + "\n  " + m.input.View() + "\n")
	case modeChapter:
		b.WriteString("\n" + sHeader.Render("new chapter") + "\n  " + m.input.View() + "\n")
	case modeBookTitle, modeBookTarget:
		b.WriteString("\n" + sHeader.Render("new book") + "\n")
		if m.draft.title != "" {
			b.WriteString(sDim.Render("  "+m.draft.title) + "\n")
		}
		b.WriteString("  " + m.input.View() + "\n")
	case modeReview:
		b.WriteString("\n" + sHeader.Render("review") + sDim.Render("  one line, dated, kept under the goal; enter with nothing skips") + "\n  " + m.input.View() + "\n")
	case modeTags:
		b.WriteString("\n" + sHeader.Render("tags") + sDim.Render("  remove fleeting to promote; add literature, hub, or any topic") + "\n  " + m.input.View() + "\n")
	case modeConfirmDelete:
		b.WriteString("\n" + sWarn.Render("delete "+m.note.Title+"? ") + "\n")
	case modePick:
		m.viewPick(&b)
	}
	if m.err != nil {
		b.WriteString("\n" + sWarn.Render(m.err.Error()) + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + sDim.Render(m.status) + "\n")
	}
	if tip := m.tip(); tip != "" && m.mode == modeBrowse && m.showKeys {
		b.WriteString("\n" + sDim.Render("tip  ") + sValue.Render(tip) + "\n")
	}
	body := b.String()
	// The panel is the default; when the screen can't fit it, the help wraps
	// on key boundaries instead so nothing scrolls off the top.
	used := strings.Count(body, "\n") + 3
	if m.showKeys && (m.height <= 0 || used+m.panelRows() <= m.height) {
		return sPadding.Render(body + "\n" + m.keyPanel())
	}
	return sPadding.Render(body + sHelp.Render(strings.Join(m.helpLines(), "\n")))
}

func (m Model) viewHomeStats(b *strings.Builder) {
	todayLine := sWarn.Render("nothing written yet") + sDim.Render("   J to start")
	if m.today != nil {
		todayLine = sValue.Render(fmt.Sprintf("%s, %d words", plural(len(m.today.Entries), "entry", "entries"), m.today.Words))
	}
	notes := m.ix.Notes()
	fleeting := 0
	for _, n := range notes {
		if n.HasTag(vault.FleetingTag) {
			fleeting++
		}
	}
	b.WriteString(sLabel.Render("today") + todayLine + "\n")
	b.WriteString(sLabel.Render("streak") + sValue.Render(plural(vault.Streak(m.days, m.now), "day", "days")) + "\n")
	b.WriteString(sLabel.Render("journal") + sValue.Render(plural(len(m.days), "day", "days")) + "\n")
	fl := sDim.Render("inbox clear")
	if fleeting > 0 {
		fl = sValue.Render(plural(fleeting, "fleeting note", "fleeting notes")+" waiting") + sDim.Render("  f")
	}
	b.WriteString(sLabel.Render("notes") + sValue.Render(fmt.Sprintf("%d", len(notes))) + "   " + fl + "\n")
	orphans, missing := len(m.ix.Orphans()), len(m.ix.Missing())
	linkLine := sValue.Render(plural(m.ix.LinkCount(), "link", "links"))
	if orphans > 0 {
		linkLine += "   " + sWarn.Render(plural(orphans, "orphan", "orphans")) + sDim.Render("  o")
	}
	if missing > 0 {
		linkLine += "   " + sWarn.Render(fmt.Sprintf("%d referenced but unwritten", missing)) + sDim.Render("  m")
	}
	b.WriteString(sLabel.Render("links") + linkLine + "\n")
	if len(m.books) > 0 {
		var parts []string
		for i := range m.books {
			bk := &m.books[i]
			p := fmt.Sprintf("%s %s", bk.Title, group(bk.Words()))
			if t := vault.WordsToday(*bk, m.cacheDir(), m.now); t != 0 {
				p += fmt.Sprintf(" (%+d today)", t)
			}
			parts = append(parts, p)
		}
		b.WriteString(sLabel.Render("books") + sValue.Render(strings.Join(parts, "   ")) + sDim.Render("  B") + "\n")
	}
	if active := m.activeGoals(); len(active) > 0 {
		quiet := 0
		for _, g := range active {
			if t, ok := g.LastTouched(); !ok || m.now.Sub(t) > quietAfter {
				quiet++
			}
		}
		gl := sValue.Render(plural(len(active), "active goal", "active goals"))
		if quiet > 0 {
			gl += "   " + sWarn.Render(fmt.Sprintf("%d quiet in the journal", quiet))
		}
		b.WriteString(sLabel.Render("goals") + gl + sDim.Render("  G") + "\n")
	}
	if todo := fleeting + orphans + missing; todo > 0 && (m.now.Weekday() == time.Sunday || fleeting >= 10) {
		b.WriteString(sLabel.Render("review") + sWarn.Render("good day for it") + sDim.Render("   P walks the fleeting inbox one note at a time") + "\n")
	}

	b.WriteString(sHeader.Render("on this day") + "\n")
	if len(m.onThis) == 0 {
		b.WriteString(sDim.Render("  nothing from earlier years yet") + "\n")
	}
	for _, d := range m.onThis {
		first := ""
		if len(d.Entries) > 0 {
			first = d.Entries[0].Title
		}
		b.WriteString(fmt.Sprintf("  %s  %s\n", sValue.Render(d.Date.Format("2006")), first))
	}
}

func (m Model) viewNotesHeader(b *strings.Builder) {
	hint := map[filter]string{
		filterAll:      "every note, newest edit first",
		filterFleeting: "oldest first: rewrite in your own words, link it, then p to promote",
		filterOrphans:  "nothing links here and these link nowhere: y a link into a hub or related note, or D",
		filterMissing:  "linked to but never written: enter creates the note",
	}[m.filter]
	b.WriteString(sDim.Render(hint) + "\n")
	if m.query != "" && m.mode != modeSearch {
		b.WriteString(sDim.Render("filtered by ") + sValue.Render(m.query) + sDim.Render("   / to change, esc clears") + "\n")
	}
}

func (m Model) viewNoteHeader(b *strings.Builder) {
	n := m.note
	if len(n.Tags) > 0 {
		b.WriteString(sLabel.Render("tags") + sTag.Render(":"+strings.Join(n.Tags, ":")+":") + "\n")
	}
	b.WriteString(sLabel.Render("life") + sValue.Render(n.Life()) + "\n")
	created := sDim.Render("unknown")
	if !n.Created.IsZero() {
		created = sValue.Render(n.Created.Format("Mon 2 Jan 2006"))
	}
	b.WriteString(sLabel.Render("created") + created + "\n")
	b.WriteString(sLabel.Render("words") + sValue.Render(fmt.Sprintf("%d", n.Words)) + "\n")
	b.WriteString(sLabel.Render("file") + sDim.Render(strings.TrimPrefix(n.Path, m.v.Root+"/")) + "\n")
	if len(m.preview) > 0 {
		b.WriteString("\n")
		limit := max(20, m.width-8)
		for _, l := range m.preview {
			b.WriteString(sPreview.Render(clip(l, limit)) + "\n")
		}
		if n.Words > 0 && len(m.preview) == previewLines {
			b.WriteString(sDim.Render("  …  e to read the rest") + "\n")
		}
	}
}

func Run(v vault.Vault, editor string) error {
	p := tea.NewProgram(New(v, editor), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2006")
	}
}

// layout is the column plan for list rows, derived from the terminal width.
type layout struct {
	title, tags, links, age int
}

const colGap = "  "

func (m Model) columns() layout {
	w := m.width
	if w <= 0 {
		w = 100
	}
	avail := w - 4 - 2 // padding and cursor
	l := layout{links: 8, age: 10}
	l.tags = min(30, max(12, avail*3/10))
	l.title = min(52, avail-l.tags-l.links-l.age-3*len(colGap))
	if l.title < 18 {
		l.title = 18
		l.tags = max(8, avail-l.title-l.links-l.age-3*len(colGap))
	}
	return l
}

func (l layout) header(kind rowKind) string {
	h := func(s string, w int) string { return sDim.Render(pad(s, w)) }
	switch kind {
	case rowDay:
		return "  " + h("day", 12) + colGap + h("first entry", l.title+l.tags+l.links-12-len(colGap)) + colGap + h("words", l.age)
	case rowMissing:
		return "  " + h("missing note", l.title) + colGap + h("wanted by", l.tags+l.links+l.age+2*len(colGap))
	case rowGoal:
		return "  " + h("goal", l.title) + colGap + h("milestones", l.tags) + colGap + h("by", l.links) + colGap + h("journal", l.age)
	case rowBook:
		return "  " + h("book", l.title) + colGap + h("words / target", l.tags) + colGap + h("today", l.links+2) + colGap + h("edited", l.age)
	case rowChapter:
		return "  " + h("stage", 8) + " " + h("chapter", l.title-9) + colGap + h("promise", l.tags) + colGap + h("words", l.links) + colGap + h("edited", l.age)
	case rowMilestone:
		return "  " + h("step", l.title+4)
	case rowReview:
		return "  " + h("when", 12) + colGap + h("what moved / what's next", l.title)
	}
	return "  " + h("title", l.title) + colGap + h("tags", l.tags) + colGap + h("links", l.links) + colGap + h("edited", l.age)
}

func (l layout) row(it item, selected bool) string {
	cur, title := "  ", sValue
	if selected {
		cur, title = sCursor.Render("> "), sCursor
	}
	switch it.kind {
	case rowDay:
		first := ""
		if len(it.tags) > 0 {
			first = it.tags[0]
		}
		wide := l.title + l.tags + l.links - 12 - len(colGap)
		return cur + title.Render(pad(it.label, 12)) + colGap + sValue.Render(pad(first, wide)) + colGap + sDim.Render(pad(fmt.Sprintf("%dw", it.words), l.age))
	case rowMissing:
		return cur + title.Render(pad(it.label, l.title)) + colGap + sDim.Render(plural(it.wanted, "note", "notes")+"   enter creates it")
	case rowGoal:
		prog := sDim.Render(pad("no steps", l.tags))
		if it.words > 0 {
			prog = sValue.Render(pad(fmt.Sprintf("%d/%d", it.degree, it.words), 6)) + bar(it.degree, it.words, l.tags-7) + " "
		}
		touched := sDim.Render(pad(it.age, l.age))
		if it.missing == "quiet" {
			touched = sWarn.Render(pad(it.age, l.age))
		}
		return cur + title.Render(pad(it.label, l.title)) + colGap + prog + colGap + sDim.Render(pad(it.horizon, l.links)) + colGap + touched
	case rowBook:
		prog := sValue.Render(pad(group(it.degree), 8))
		if it.words > 0 {
			prog += bar(min(it.degree, it.words), it.words, l.tags-9) + " "
		} else {
			prog = sValue.Render(pad(group(it.degree)+" words", l.tags))
		}
		return cur + title.Render(pad(it.label, l.title)) + colGap + prog + colGap + sDim.Render(pad(it.horizon, l.links+2)) + colGap + sDim.Render(pad(it.age, l.age))
	case rowChapter:
		stage := sDim.Render(pad(it.horizon, 8))
		if it.horizon == "draft" || it.horizon == "revise" {
			stage = sCursor.Render(pad(it.horizon, 8))
		}
		promise := ""
		if len(it.tags) > 0 {
			promise = it.tags[0]
		}
		return cur + stage + " " + title.Render(pad(it.label, l.title-9)) + colGap + sDim.Render(pad(promise, l.tags)) + colGap + sValue.Render(pad(group(it.degree)+"w", l.links)) + colGap + sDim.Render(pad(it.age, l.age))
	case rowMilestone:
		box := sDim.Render("[ ]")
		if it.done {
			box = sCursor.Render("[x]")
		}
		return cur + box + " " + title.Render(clip(it.label, l.title+l.tags))
	case rowReview:
		text := ""
		if len(it.tags) > 0 {
			text = it.tags[0]
		}
		return cur + title.Render(pad(it.label, 12)) + colGap + sValue.Render(clip(text, l.title+l.tags+l.links-12))
	}
	tags := ""
	if len(it.tags) > 0 {
		tags = ":" + strings.Join(it.tags, ":") + ":"
	}
	links := sWarn.Render(pad("orphan", l.links))
	if it.degree > 0 {
		links = sDim.Render(pad(plural(it.degree, "link", "links"), l.links))
	}
	return cur + title.Render(pad(it.label, l.title)) + colGap + sTag.Render(pad(tags, l.tags)) + colGap + links + colGap + sDim.Render(pad(it.age, l.age))
}

// pad clips s to w runes (with an ellipsis) or pads it to exactly w.
func pad(s string, w int) string {
	s = clip(s, w)
	n := len([]rune(s))
	if n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// clip truncates s to at most w runes, ending in an ellipsis when cut.
func clip(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}
