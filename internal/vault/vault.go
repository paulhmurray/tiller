// Package vault knows the on-disk layout of a tiller root:
//
//	<root>/journal/YYYY-MM-DD.org   one file per day
//	<root>/notes/<slug>.org         flat zettelkasten, lifecycle via tags
//
// Everything is plain org. The vault never writes outside root.
package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/paulhmurray/tiller/internal/org"
)

const (
	JournalDir = "journal"
	NotesDir   = "notes"
	// FleetingTag marks a note that has not yet been processed into a
	// permanent note. Removing the tag is the act of processing it.
	FleetingTag = "fleeting"
)

type Vault struct{ Root string }

func (v Vault) JournalPath() string { return filepath.Join(v.Root, JournalDir) }
func (v Vault) NotesPath() string   { return filepath.Join(v.Root, NotesDir) }

func (v Vault) DayPath(t time.Time) string {
	return filepath.Join(v.JournalPath(), t.Format("2006-01-02")+".org")
}

// DayTitle is the #+TITLE used for a daily file.
func DayTitle(t time.Time) string { return t.Format("2006-01-02 Monday") }

type Entry struct {
	Title string
	Body  []string
}

type Day struct {
	Date    time.Time
	Path    string
	Entries []Entry
	Words   int
	Links   []string // raw link targets found anywhere in the file
}

// Days returns every daily file, oldest first.
func (v Vault) Days() ([]Day, error) {
	paths, err := filepath.Glob(filepath.Join(v.JournalPath(), "????-??-??.org"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var days []Day
	for _, p := range paths {
		d, err := ParseDay(p)
		if err != nil {
			return nil, err
		}
		days = append(days, d)
	}
	return days, nil
}

func ParseDay(path string) (Day, error) {
	stem := strings.TrimSuffix(filepath.Base(path), ".org")
	date, err := time.ParseInLocation("2006-01-02", stem, time.Local)
	if err != nil {
		return Day{}, fmt.Errorf("%s: not a daily file", path)
	}
	doc, err := org.ParseFile(path)
	if err != nil {
		return Day{}, err
	}
	day := Day{Date: date, Path: path}
	for _, l := range doc.Links() {
		day.Links = append(day.Links, l.Target)
	}
	for _, h := range doc.Headlines {
		if h.Level != 1 {
			continue
		}
		e := Entry{Title: h.Title, Body: trimBlank(doc.Lines[h.ContentStart:h.End])}
		day.Entries = append(day.Entries, e)
		day.Words += org.Words(e.Body) + len(strings.Fields(e.Title))
	}
	return day, nil
}

// EnsureDay creates today's file with its title line if missing.
func (v Vault) EnsureDay(t time.Time) (string, bool, error) {
	p := v.DayPath(t)
	if _, err := os.Stat(p); err == nil {
		return p, false, nil
	}
	if err := os.MkdirAll(v.JournalPath(), 0o755); err != nil {
		return "", false, err
	}
	content := "#+TITLE: " + DayTitle(t) + "\n\n"
	return p, true, os.WriteFile(p, []byte(content), 0o644)
}

// Jot appends a one-line entry to the day's file.
func (v Vault) Jot(t time.Time, text string) (string, error) {
	p, _, err := v.EnsureDay(t)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if len(b) > 0 && b[len(b)-1] != '\n' {
		sb.WriteByte('\n')
	}
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n\n") {
		sb.WriteByte('\n')
	}
	sb.WriteString("* " + strings.TrimSpace(text) + "\n")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.WriteString(sb.String())
	return p, err
}

// Streak counts consecutive days with a journal file, ending today or
// yesterday (so an unwritten today does not break it).
func Streak(days []Day, today time.Time) int {
	have := map[string]bool{}
	for _, d := range days {
		have[d.Date.Format("2006-01-02")] = true
	}
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	if !have[day.Format("2006-01-02")] {
		day = day.AddDate(0, 0, -1)
	}
	n := 0
	for have[day.Format("2006-01-02")] {
		n++
		day = day.AddDate(0, 0, -1)
	}
	return n
}

// OnThisDay returns entries from the same month and day in earlier years.
func OnThisDay(days []Day, today time.Time) []Day {
	var out []Day
	for _, d := range days {
		if d.Date.Month() == today.Month() && d.Date.Day() == today.Day() && d.Date.Year() < today.Year() {
			out = append(out, d)
		}
	}
	return out
}

type Note struct {
	Path    string
	Slug    string
	ID      string
	Title   string
	Tags    []string
	Created time.Time
	ModTime time.Time
	Words   int
	Links   []string // link targets, raw (id:..., file:..., https://...)
}

func (n Note) HasTag(tag string) bool {
	for _, t := range n.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Notes returns every note, newest modified first.
func (v Vault) Notes() ([]Note, error) {
	paths, err := filepath.Glob(filepath.Join(v.NotesPath(), "*.org"))
	if err != nil {
		return nil, err
	}
	var notes []Note
	for _, p := range paths {
		n, err := ParseNote(p)
		if err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].ModTime.After(notes[j].ModTime) })
	return notes, nil
}

func ParseNote(path string) (Note, error) {
	doc, err := org.ParseFile(path)
	if err != nil {
		return Note{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return Note{}, err
	}
	n := Note{
		Path:    path,
		Slug:    strings.TrimSuffix(filepath.Base(path), ".org"),
		ID:      doc.FileID,
		Title:   doc.Title(),
		Tags:    doc.FileTags(),
		ModTime: st.ModTime(),
		Words:   org.Words(doc.Lines[doc.BodyStart:]),
	}
	if n.Title == "" {
		n.Title = strings.ReplaceAll(n.Slug, "-", " ")
	}
	if c, ok := doc.Keyword("CREATED"); ok {
		if t, ok := org.ParseStamp(c); ok {
			n.Created = t
		}
	}
	for _, l := range doc.Links() {
		n.Links = append(n.Links, l.Target)
	}
	return n, nil
}

// Kind is the lifecycle a note is born into.
type Kind int

const (
	KindFleeting Kind = iota
	KindLiterature
)

func (k Kind) String() string {
	if k == KindLiterature {
		return "literature"
	}
	return "fleeting"
}

// LinkTo is an initial link written under a new note's Related heading.
type LinkTo struct{ ID, Title string }

func RenderLink(id, title string) string { return "[[id:" + id + "][" + title + "]]" }

// NewNote creates a fleeting note with no links. See Create.
func (v Vault) NewNote(title string, tags []string, now time.Time) (string, error) {
	return v.Create(title, KindFleeting, tags, nil, now)
}

// Create writes notes/<slug>.org from the kind's template and returns its
// path. Fleeting notes are a bare header so capture stays fast; literature
// notes get a skeleton. Links land under a Related heading.
func (v Vault) Create(title string, kind Kind, tags []string, links []LinkTo, now time.Time) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("empty title")
	}
	slug := org.Slugify(title)
	if slug == "" {
		slug = now.Format("20060102150405")
	}
	if err := os.MkdirAll(v.NotesPath(), 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(v.NotesPath(), slug+".org")
	for i := 2; ; i++ {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			break
		}
		p = filepath.Join(v.NotesPath(), fmt.Sprintf("%s-%d.org", slug, i))
	}
	id := strings.TrimSuffix(filepath.Base(p), ".org")
	lead := kind.String()
	seen := map[string]bool{lead: true}
	all := []string{lead}
	for _, t := range tags {
		if !seen[t] {
			seen[t] = true
			all = append(all, t)
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, ":PROPERTIES:\n:ID: %s\n:END:\n#+TITLE: %s\n#+FILETAGS: %s\n#+CREATED: %s\n", id, title, org.JoinTags(all), org.Stamp(now))
	if kind == KindLiterature {
		sb.WriteString("#+AUTHOR: \n#+SOURCE: \n\n* Summary\n\n* Key takeaways\n\n* My take\n\n")
	} else {
		sb.WriteString("\n")
	}
	if len(links) > 0 || kind == KindLiterature {
		if kind != KindLiterature {
			sb.WriteString("\n")
		}
		sb.WriteString("* Related\n")
		for _, l := range links {
			sb.WriteString("- " + RenderLink(l.ID, l.Title) + "\n")
		}
	}
	return p, os.WriteFile(p, []byte(sb.String()), 0o644)
}

// AppendLink adds "- [[id:id][title]]" under the note's level-1 Related
// heading, creating the heading at the end if needed. It returns false
// without touching the file when a link to that id already exists.
func AppendLink(path, id, title string) (bool, error) {
	doc, err := org.ParseFile(path)
	if err != nil {
		return false, err
	}
	for _, l := range doc.Links() {
		if strings.EqualFold(strings.TrimSpace(l.Target), "id:"+id) {
			return false, nil
		}
	}
	lines := doc.Lines
	entry := "- " + RenderLink(id, title)
	at := -1
	for _, h := range doc.Headlines {
		if h.Level == 1 && strings.EqualFold(strings.TrimSpace(h.Title), "related") {
			at = h.End
			for at > h.Line+1 && strings.TrimSpace(lines[at-1]) == "" {
				at--
			}
			break
		}
	}
	if at >= 0 {
		lines = append(lines[:at], append([]string{entry}, lines[at:]...)...)
	} else {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "* Related", entry)
	}
	return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// SetTags rewrites a note's #+FILETAGS line in place, preserving every
// other byte. An empty tag list removes the line.
func SetTags(path string, tags []string) error {
	doc, err := org.ParseFile(path)
	if err != nil {
		return err
	}
	lines := doc.Lines
	line := -1
	for _, k := range doc.Keywords {
		if k.Key == "FILETAGS" {
			line = k.Line
			break
		}
	}
	switch {
	case line >= 0 && len(tags) == 0:
		lines = append(lines[:line], lines[line+1:]...)
	case line >= 0:
		prefix, _, _ := strings.Cut(lines[line], ":")
		lines[line] = prefix + ": " + org.JoinTags(tags)
	case len(tags) > 0:
		at := doc.BodyStart
		for _, k := range doc.Keywords {
			if k.Key == "TITLE" {
				at = k.Line + 1
			}
		}
		lines = append(lines[:at], append([]string{"#+FILETAGS: " + org.JoinTags(tags)}, lines[at:]...)...)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// Promote removes the fleeting tag. It refuses when the note has no tag to
// remove, so callers can treat the error as "nothing to do".
func Promote(path string) error {
	n, err := ParseNote(path)
	if err != nil {
		return err
	}
	if !n.HasTag(FleetingTag) {
		return errors.New("not a fleeting note")
	}
	var keep []string
	for _, t := range n.Tags {
		if t != FleetingTag {
			keep = append(keep, t)
		}
	}
	return SetTags(path, keep)
}

// Trash moves a note out of the vault into trashDir and returns the new
// path. Syncthing then sees a plain deletion.
func Trash(path, trashDir string, now time.Time) (string, error) {
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return "", err
	}
	stem := strings.TrimSuffix(filepath.Base(path), ".org")
	dst := filepath.Join(trashDir, stem+"-"+now.Format("20060102-150405")+".org")
	if err := os.Rename(path, dst); err != nil {
		// Cross-device: copy then remove.
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return "", err
		}
		if werr := os.WriteFile(dst, b, 0o600); werr != nil {
			return "", werr
		}
		if rerr := os.Remove(path); rerr != nil {
			return "", rerr
		}
	}
	return dst, nil
}

// Preview returns up to n non-blank body lines of a note.
func Preview(path string, n int) []string {
	doc, err := org.ParseFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range doc.Lines[doc.BodyStart:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, l)
		if len(out) == n {
			break
		}
	}
	return out
}
