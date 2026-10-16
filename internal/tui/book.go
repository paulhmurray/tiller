package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/paulhmurray/tiller/internal/vault"
)

// Books: a list when there is more than one, a book screen with its
// chapters, stage cycling, a new-chapter prompt, and today's word count.

func (m *Model) loadBooks() {
	books, err := m.v.Books()
	if err != nil {
		m.err = err
	}
	m.books = books
	m.book = nil
	if m.bookSlug != "" {
		m.book = m.findBook(m.bookSlug)
	}
}

func (m *Model) findBook(slug string) *vault.Book {
	for i := range m.books {
		if m.books[i].Slug == slug {
			return &m.books[i]
		}
	}
	return nil
}

func (m *Model) cacheDir() string {
	if m.trashDir == "" {
		return ""
	}
	return strings.TrimSuffix(m.trashDir, "/trash")
}

func (m *Model) bookItem(section string, b *vault.Book) item {
	it := item{section: section, kind: rowBook, key: b.Slug, label: b.Title, path: b.Outline,
		degree: b.Words(), words: b.Target, wanted: len(b.Chapters), age: "untouched"}
	if t := b.LastEdited(); !t.IsZero() {
		it.age = ago(t, m.now)
	}
	it.horizon = fmt.Sprintf("+%d today", vault.WordsToday(*b, m.cacheDir(), m.now))
	return it
}

func (m *Model) buildBookItems() {
	switch m.screen {
	case screenBooks:
		for i := range m.books {
			m.items = append(m.items, m.bookItem("books", &m.books[i]))
		}
	case screenBook:
		for i, c := range m.book.Chapters {
			it := item{section: "chapters", kind: rowChapter, key: fmt.Sprintf("%d", i), label: c.Title, path: c.File, line: c.Line,
				tags: []string{c.Promise}, horizon: strings.ToLower(c.State), degree: c.Words, age: "not started"}
			if c.Exists && c.Words > 0 {
				it.age = ago(c.ModTime, m.now)
			}
			m.items = append(m.items, it)
		}
		// Notes tagged for this book, newest edit first; t shows them all.
		shown := 0
		for _, n := range m.ix.Notes() {
			if !n.HasTag(m.book.Tag) || n.HasTag("hub") {
				continue
			}
			if shown == bookNoteRows {
				break
			}
			m.items = append(m.items, m.noteItem("notes about this book", n))
			shown++
		}
	}
}

const bookNoteRows = 8

// openBookNotes is the all-notes list filtered to the book's tag.
func (m *Model) openBookNotes() {
	tag := m.book.Tag
	m.openNotes(filterAll)
	m.query = tag
	m.cursor = 0
	m.buildItems()
}

// openBooks goes to the list, or straight to the only book.
func (m *Model) openBooks() {
	if len(m.books) == 1 {
		m.openBook(m.books[0].Slug)
		return
	}
	m.push()
	m.screen, m.cursor, m.note, m.goal = screenBooks, 0, nil, nil
	m.buildItems()
}

func (m *Model) openBook(slug string) {
	b := m.findBook(slug)
	if b == nil {
		return
	}
	m.push()
	m.screen, m.cursor, m.book, m.bookSlug = screenBook, 0, b, slug
	m.buildItems()
}

func (m *Model) chapterAt(cur *item) *vault.Chapter {
	if cur == nil || cur.kind != rowChapter || m.book == nil {
		return nil
	}
	var i int
	fmt.Sscanf(cur.key, "%d", &i)
	if i < 0 || i >= len(m.book.Chapters) {
		return nil
	}
	return &m.book.Chapters[i]
}

// --- prompts ---

func (m *Model) startChapter() tea.Cmd {
	m.mode = modeChapter
	m.input.Placeholder = "chapter title; the file is made for you"
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

func (m *Model) startNewBook() tea.Cmd {
	m.draft = goalDraft{}
	m.mode = modeBookTitle
	m.input.Placeholder = "the book's title"
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

func (m Model) updateBookInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case kEsc, "ctrl+c":
		m.mode = modeBrowse
		m.input.Blur()
		return m, nil
	case kEnter:
		val := strings.TrimSpace(m.input.Value())
		switch m.mode {
		case modeChapter:
			m.mode = modeBrowse
			m.input.Blur()
			if val == "" {
				return m, nil
			}
			c, err := m.v.AddChapter(*m.book, val)
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.reload()
			m.cursor = max(0, len(m.items)-1)
			m.status = "added " + c.Title + "; enter opens it, o opens its outline heading"
			return m, m.edit(c.File, true)
		case modeBookTitle:
			if val == "" {
				return m, nil
			}
			m.draft.title = val
			m.mode = modeBookTarget
			m.input.Placeholder = "word target, e.g. 70000 (enter for 70000)"
			m.input.SetValue("")
			return m, nil
		case modeBookTarget:
			m.mode = modeBrowse
			m.input.Blur()
			target := 70000
			if val != "" {
				if _, err := fmt.Sscanf(strings.ReplaceAll(val, ",", ""), "%d", &target); err != nil {
					m.status = "target must be a number"
					return m, nil
				}
			}
			b, err := m.v.NewBook(m.draft.title, target, time.Now())
			if err != nil {
				m.status = err.Error()
				return m, nil
			}
			m.reload()
			m.openBook(b.Slug)
			m.status = b.Title + ": a adds the first chapter"
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// --- views ---

func (m Model) viewBooksHeader(b *strings.Builder) {
	b.WriteString(sDim.Render("each book is a folder under book/: an outline of chapters and one prose file per chapter") + "\n")
}

func (m Model) viewBookHeader(b *strings.Builder) {
	bk := m.book
	words := bk.Words()
	prog := sValue.Render(fmt.Sprintf("%s words", group(words)))
	if bk.Target > 0 {
		prog += sDim.Render(fmt.Sprintf(" of %s  ", group(bk.Target))) + bar(min(words, bk.Target), bk.Target, 24)
	}
	b.WriteString(sLabel.Render("progress") + prog + "\n")
	today := vault.WordsToday(*bk, m.cacheDir(), m.now)
	tl := sDim.Render("nothing yet; counted from the first look each day")
	if today > 0 {
		tl = sCursor.Render(fmt.Sprintf("+%s words", group(today)))
	} else if today < 0 {
		tl = sWarn.Render(fmt.Sprintf("%s words (cut)", group(today)))
	}
	b.WriteString(sLabel.Render("today") + tl + "\n")
	counts := bk.Counts()
	var parts []string
	for _, s := range bk.Stages {
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ToLower(s)))
		}
	}
	stages := sDim.Render("no chapters yet; a adds one")
	if len(parts) > 0 {
		stages = sValue.Render(strings.Join(parts, ", "))
	}
	b.WriteString(sLabel.Render("chapters") + stages + "\n")
	b.WriteString(sLabel.Render("outline") + sDim.Render(strings.TrimPrefix(bk.Outline, m.v.Root+"/")) + "\n")
}

func group(n int) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
