package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/paulhmurray/tiller/internal/org"
)

// A book lives in <root>/book/<slug>/: outline.org holds the chapters as
// level-1 headings that carry a stage keyword, each with a :FILE: property
// naming the prose file beside it. Level-1 headings without a keyword
// (Premise, Story map, Characters) are planning space and are left alone. The outline carries its own TODO keywords so chapter status reads as
// writing stages in nvim's agenda too:
//
//	#+TITLE: Hot Desk
//	#+TARGET: 70000
//	#+TODO: OUTLINE DRAFT REVISE | DONE
//
//	* OUTLINE Chapter one: the desk
//	  :PROPERTIES:
//	  :FILE: ch01.org
//	  :END:
//	  One line on what the chapter promises.
//	** Uses
//	- [[id:mara][Mara]]

const (
	BooksDir    = "book"
	OutlineFile = "outline.org"
)

var defaultStages = []string{"OUTLINE", "DRAFT", "REVISE", "DONE"}

type Chapter struct {
	Title   string
	State   string
	File    string // absolute path of the prose file
	Promise string
	Words   int
	ModTime time.Time
	Line    int // heading line in outline.org
	Exists  bool
}

type Book struct {
	Slug     string
	Dir      string
	Outline  string
	Title    string
	Target   int
	Tag      string   // the zettelkasten tag for notes about this book
	Stages   []string // active keywords then done ones, in cycle order
	Chapters []Chapter
}

func (b Book) Words() int {
	n := 0
	for _, c := range b.Chapters {
		n += c.Words
	}
	return n
}

func (b Book) Counts() map[string]int {
	out := map[string]int{}
	for _, c := range b.Chapters {
		out[c.State]++
	}
	return out
}

func (b Book) LastEdited() time.Time {
	var t time.Time
	for _, c := range b.Chapters {
		if c.ModTime.After(t) {
			t = c.ModTime
		}
	}
	return t
}

// HubID is the id of the book's map-of-content note, notes/hub-<slug>.org.
func (b Book) HubID() string { return "hub-" + b.Slug }

func (v Vault) BooksPath() string { return filepath.Join(v.Root, BooksDir) }

// Books lists every book folder that has an outline, by title.
func (v Vault) Books() ([]Book, error) {
	dirs, err := filepath.Glob(filepath.Join(v.BooksPath(), "*", OutlineFile))
	if err != nil {
		return nil, err
	}
	var books []Book
	for _, outline := range dirs {
		b, err := ParseBook(outline)
		if err != nil {
			return nil, err
		}
		books = append(books, b)
	}
	sort.Slice(books, func(i, j int) bool { return books[i].Title < books[j].Title })
	return books, nil
}

func (v Vault) Book(slug string) (Book, error) {
	return ParseBook(filepath.Join(v.BooksPath(), slug, OutlineFile))
}

func ParseBook(outline string) (Book, error) {
	doc, err := org.ParseFile(outline)
	if err != nil {
		return Book{}, err
	}
	dir := filepath.Dir(outline)
	b := Book{Slug: filepath.Base(dir), Dir: dir, Outline: outline, Title: doc.Title(), Stages: defaultStages}
	if b.Title == "" {
		b.Title = strings.ReplaceAll(b.Slug, "-", " ")
	}
	b.Tag = strings.ReplaceAll(b.Slug, "-", "")
	if t, ok := doc.Keyword("TAG"); ok && strings.TrimSpace(t) != "" {
		b.Tag = strings.Trim(strings.TrimSpace(t), ":")
	}
	if t, ok := doc.Keyword("TARGET"); ok {
		fmt.Sscanf(strings.ReplaceAll(t, ",", ""), "%d", &b.Target)
	}
	if kw, ok := doc.Keyword("TODO"); ok {
		var stages []string
		for _, w := range strings.Fields(kw) {
			if w != "|" {
				stages = append(stages, w)
			}
		}
		if len(stages) > 0 {
			b.Stages = stages
		}
	}
	stageRe := regexp.MustCompile(`^(` + strings.Join(b.Stages, "|") + `)\s+(.*)$`)
	n := 0
	for _, h := range doc.Headlines {
		if h.Level != 1 {
			continue
		}
		m := stageRe.FindStringSubmatch(h.Title)
		if m == nil && h.Props["FILE"] == "" {
			continue // a planning heading (premise, story map, characters), not a chapter
		}
		n++
		c := Chapter{Title: h.Title, Line: h.Line}
		if m != nil {
			c.State, c.Title = m[1], strings.TrimSpace(m[2])
		}
		file := h.Props["FILE"]
		if file == "" {
			file = fmt.Sprintf("ch%02d.org", n) // positional fallback for hand-written headings
		}
		c.File = filepath.Join(dir, file)
		for _, l := range doc.Lines[h.ContentStart:h.End] {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "*") || strings.HasPrefix(t, ":") {
				if strings.HasPrefix(t, "*") {
					break
				}
				continue
			}
			c.Promise = t
			break
		}
		if cd, err := org.ParseFile(c.File); err == nil {
			c.Exists = true
			c.Words = org.Words(cd.Lines[cd.BodyStart:])
			if st, err := os.Stat(c.File); err == nil {
				c.ModTime = st.ModTime()
			}
		}
		b.Chapters = append(b.Chapters, c)
	}
	return b, nil
}

// NewBook scaffolds book/<slug>/outline.org and returns the book.
func (v Vault) NewBook(title string, target int, now time.Time) (Book, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Book{}, fmt.Errorf("empty title")
	}
	slug := org.Slugify(title)
	dir := filepath.Join(v.BooksPath(), slug)
	outline := filepath.Join(dir, OutlineFile)
	if _, err := os.Stat(outline); err == nil {
		return Book{}, fmt.Errorf("book %q already exists", slug)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Book{}, err
	}
	content := fmt.Sprintf("#+TITLE: %s\n#+TARGET: %d\n#+TAG: %s\n#+TODO: OUTLINE DRAFT REVISE | DONE\n#+CREATED: %s\n#+STARTUP: content\n\n"+
		"# One level-1 heading per chapter. The heading's keyword is the chapter's\n"+
		"# stage; the first line under it is the promise: what the chapter does for\n"+
		"# the reader. Links under \"Uses\" point at the notes it draws on.\n",
		title, target, strings.ReplaceAll(slug, "-", ""), org.Stamp(now))
	if err := os.WriteFile(outline, []byte(content), 0o644); err != nil {
		return Book{}, err
	}
	return ParseBook(outline)
}

// AddChapter appends a heading to the outline and creates the prose file.
func (v Vault) AddChapter(b Book, title string) (Chapter, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Chapter{}, fmt.Errorf("empty title")
	}
	n := len(b.Chapters) + 1
	file := fmt.Sprintf("ch%02d.org", n)
	for {
		if _, err := os.Stat(filepath.Join(b.Dir, file)); os.IsNotExist(err) {
			break
		}
		n++
		file = fmt.Sprintf("ch%02d.org", n)
	}
	doc, err := org.ParseFile(b.Outline)
	if err != nil {
		return Chapter{}, err
	}
	lines := doc.Lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	lines = append(lines, "",
		"* "+b.Stages[0]+" "+title,
		"  :PROPERTIES:",
		"  :FILE: "+file,
		"  :END:",
		"  ",
		"** Uses")
	if err := os.WriteFile(b.Outline, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return Chapter{}, err
	}
	path := filepath.Join(b.Dir, file)
	if err := v.EnsureChapterFile(b, Chapter{Title: title, File: path}); err != nil {
		return Chapter{}, err
	}
	nb, err := ParseBook(b.Outline)
	if err != nil {
		return Chapter{}, err
	}
	return nb.Chapters[len(nb.Chapters)-1], nil
}

// EnsureChapterFile creates the prose file with a short header if missing.
func (v Vault) EnsureChapterFile(b Book, c Chapter) error {
	if _, err := os.Stat(c.File); err == nil {
		return nil
	}
	content := fmt.Sprintf("#+TITLE: %s\n#+BOOK: %s\n\n", c.Title, b.Title)
	return os.WriteFile(c.File, []byte(content), 0o644)
}

// CycleStage moves the chapter's heading to the next stage keyword, in
// place, wrapping from the last back to the first.
func (v Vault) CycleStage(b Book, c Chapter) (string, error) {
	doc, err := org.ParseFile(b.Outline)
	if err != nil {
		return "", err
	}
	lines := doc.Lines
	if c.Line < 0 || c.Line >= len(lines) || !strings.HasPrefix(lines[c.Line], "* ") {
		return "", fmt.Errorf("chapter heading moved; reload")
	}
	next := b.Stages[0]
	for i, s := range b.Stages {
		if s == c.State {
			next = b.Stages[(i+1)%len(b.Stages)]
		}
	}
	lines[c.Line] = "* " + next + " " + c.Title
	return next, os.WriteFile(b.Outline, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// --- today's words: a per-device snapshot of this morning's counts ---

type wordSnapshot struct {
	Date   string         `json:"date"`
	Counts map[string]int `json:"counts"` // chapter file -> words
	Total  int            `json:"total"`
}

// WordsToday returns words written since the first look at the book today.
// The snapshot is taken when the date changes, so prose written in nvim
// before tiller first opens on a day is not counted.
func WordsToday(b Book, cacheDir string, now time.Time) int {
	if cacheDir == "" {
		return 0
	}
	path := filepath.Join(cacheDir, "words", b.Slug+".json")
	today := now.Format("2006-01-02")
	var snap wordSnapshot
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &snap)
	}
	if snap.Date != today {
		snap = wordSnapshot{Date: today, Counts: map[string]int{}}
		for _, c := range b.Chapters {
			snap.Counts[filepath.Base(c.File)] = c.Words
		}
		snap.Total = b.Words()
		if data, err := json.Marshal(snap); err == nil {
			os.MkdirAll(filepath.Dir(path), 0o700)
			os.WriteFile(path, data, 0o600)
		}
		return 0
	}
	return b.Words() - snap.Total
}
