package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/paulhmurray/tiller/internal/vault"
)

func key(m Model, k string) Model {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	mm, _ := m.Update(msg)
	return mm.(Model)
}

func TestWorkflow(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	if _, err := v.Jot(time.Now(), "first thought"); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(v.NotesPath(), 0o755)
	alpha := filepath.Join(v.NotesPath(), "alpha.org")
	original := ":PROPERTIES:\n:ID: alpha\n:END:\n#+TITLE: Alpha\n#+FILETAGS: :fleeting:go:\n#+CREATED: [2026-01-01 Thu 09:00]\n\nA fleeting thought about [[file:gone.org]].\n"
	os.WriteFile(alpha, []byte(original), 0o644)
	os.WriteFile(filepath.Join(v.NotesPath(), "beta.org"), []byte(":PROPERTIES:\n:ID: beta\n:END:\n#+TITLE: Beta\n\nhello\n"), 0o644)

	var clipped string
	m := New(v, "true")
	m.width, m.height = 100, 40
	m.trashDir = filepath.Join(root, "trash")
	m.clip = func(s string) error { clipped = s; return nil }

	home := m.View()
	for _, want := range []string{"| |_(_) | | ___ _ __", "1 fleeting note waiting", "2 orphans", "1 referenced but unwritten", "fleeting inbox"} {
		if !strings.Contains(home, want) {
			t.Errorf("home missing %q:\n%s", want, home)
		}
	}
	if os.Getenv("TILLER_SHOW") != "" {
		t.Log("\n" + home)
	}

	// f → fleeting list holds Alpha, marked orphan.
	m = key(m, "f")
	if m.screen != screenNotes || m.filter != filterFleeting || len(m.items) != 1 || m.items[0].label != "Alpha" || !strings.Contains(m.View(), "orphan") {
		t.Fatalf("fleeting list wrong:\n%s", m.View())
	}
	// Open it; promote is refused while orphaned.
	m = key(m, "enter")
	if m.screen != screenNote || !strings.Contains(m.View(), "A fleeting thought") {
		t.Fatalf("note view missing preview:\n%s", m.View())
	}
	m = key(m, "p")
	if !strings.Contains(m.status, "orphan") {
		t.Fatalf("promote should refuse an orphan, status=%q", m.status)
	}
	// y copies a link.
	m = key(m, "y")
	if clipped != "[[id:alpha][Alpha]]" {
		t.Errorf("clip = %q", clipped)
	}
	// Link Beta → Alpha on disk, reload, promote succeeds and only FILETAGS changed.
	os.WriteFile(filepath.Join(v.NotesPath(), "beta.org"), []byte(":PROPERTIES:\n:ID: beta\n:END:\n#+TITLE: Beta\n\nhello "+clipped+"\n"), 0o644)
	m = key(m, "r")
	m = key(m, "p")
	if !strings.Contains(m.status, "promoted") {
		t.Fatalf("promote failed: %q", m.status)
	}
	got, _ := os.ReadFile(alpha)
	want := strings.Replace(original, "#+FILETAGS: :fleeting:go:", "#+FILETAGS: :go:", 1)
	if string(got) != want {
		t.Errorf("promote changed more than the tag line:\n%s", got)
	}
	// h returns to the fleeting list, now empty.
	m = key(m, "h")
	if m.screen != screenNotes || m.filter != filterFleeting || len(m.items) != 0 {
		t.Fatalf("back should land on the empty fleeting list, got screen=%d filter=%d items=%d", m.screen, m.filter, len(m.items))
	}
	// tab → orphans: none now (both linked). tab → missing: gone.
	m = key(m, "tab")
	if m.filter != filterOrphans || len(m.items) != 0 {
		t.Fatalf("orphans should be empty: %v", m.items)
	}
	m = key(m, "tab")
	if m.filter != filterMissing || len(m.items) != 1 || m.items[0].label != "gone" {
		t.Fatalf("missing list wrong: %v", m.items)
	}
	// Search on the all-notes list.
	m = key(m, "tab")
	m = key(m, "/")
	m = key(m, "bet")
	m = key(m, "enter")
	if len(m.items) != 1 || m.items[0].label != "Beta" {
		t.Fatalf("search wrong: %v", m.items)
	}
	// Delete Beta: D then y moves it to the trash and pops back to the list.
	m = key(m, "enter")
	m = key(m, "D")
	if m.mode != modeConfirmDelete {
		t.Fatal("expected confirm mode")
	}
	m = key(m, "y")
	if _, err := os.Stat(filepath.Join(v.NotesPath(), "beta.org")); !os.IsNotExist(err) {
		t.Error("beta.org should be gone")
	}
	trash, _ := filepath.Glob(filepath.Join(root, "trash", "beta-*.org"))
	if len(trash) != 1 {
		t.Errorf("expected one trashed file, got %v", trash)
	}
	if m.screen != screenNotes {
		t.Errorf("after delete expected notes list, got screen %d", m.screen)
	}
}

func TestInteractiveRefinement(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	os.MkdirAll(v.NotesPath(), 0o755)
	write := func(name, content string) {
		os.WriteFile(filepath.Join(v.NotesPath(), name), []byte(content), 0o644)
	}
	write("alpha.org", ":PROPERTIES:\n:ID: alpha\n:END:\n#+TITLE: Alpha sprint idea\n#+FILETAGS: :fleeting:agile:\n#+CREATED: [2026-01-01 Thu 09:00]\n\nraw thought\n")
	write("beta.org", ":PROPERTIES:\n:ID: beta\n:END:\n#+TITLE: Beta\n#+FILETAGS: :fleeting:\n#+CREATED: [2026-02-01 Sun 09:00]\n\nanother\n")
	write("sprints.org", ":PROPERTIES:\n:ID: sprints\n:END:\n#+TITLE: Sprint planning\n#+FILETAGS: :agile:\n\npermanent\n")
	m := New(v, "true")
	m.width, m.height = 110, 60
	m.prefDir = filepath.Join(root, "prefs")
	m.showKeys = true

	// Panel is on by default and describes the link picker; ? hides it and persists.
	if out := m.View(); !strings.Contains(out, "fleeting inbox") || !strings.Contains(out, "process the inbox") {
		t.Fatalf("panel missing on home:\n%s", out)
	}
	m = key(m, "?")
	if strings.Contains(m.View(), "hide / show these keys") {
		t.Error("? should hide the panel")
	}
	if _, err := os.Stat(filepath.Join(root, "prefs", "keys-hidden")); err != nil {
		t.Error("preference not saved")
	}
	m = key(m, "?")

	// Note view: l opens the picker with a suggestion first; enter links Alpha → Sprint planning on disk.
	m = key(m, "a")
	for i := range m.items {
		if m.items[i].label == "Alpha sprint idea" {
			m.cursor = i
		}
	}
	m = key(m, "enter")
	if m.screen != screenNote || !strings.Contains(m.View(), "link this note to") {
		t.Fatalf("note view/panel wrong:\n%s", m.View())
	}
	m = key(m, "l")
	if m.mode != modePick || len(m.pick.items) == 0 || m.pick.items[0].Title != "Sprint planning" {
		t.Fatalf("picker should suggest Sprint planning first: %+v", m.pick.items)
	}
	m = key(m, "enter")
	b, _ := os.ReadFile(filepath.Join(v.NotesPath(), "alpha.org"))
	if !strings.HasSuffix(string(b), "raw thought\n\n* Related\n- [[id:sprints][Sprint planning]]\n") {
		t.Fatalf("link not written:\n%s", b)
	}
	if m.ix.Degree("alpha") != 1 {
		t.Fatalf("degree = %d", m.ix.Degree("alpha"))
	}
	// L: put a link to Alpha inside Sprint planning via typed search.
	m = key(m, "L")
	m = key(m, "plan")
	m = key(m, "enter")
	b, _ = os.ReadFile(filepath.Join(v.NotesPath(), "sprints.org"))
	if !strings.Contains(string(b), "- [[id:alpha][Alpha sprint idea]]") {
		t.Fatalf("L did not write into the other note:\n%s", b)
	}
	// t edits tags in place.
	m = key(m, "t")
	m.input.SetValue(":fleeting:agile:planning:")
	m = key(m, "enter")
	if !m.note.HasTag("planning") {
		t.Fatalf("tag edit failed: %v", m.note.Tags)
	}
	// N creates a note already linked back here.
	m = key(m, "N")
	if m.mode != modeNewKind {
		t.Fatal("N should ask for a kind")
	}
	m = key(m, "l")
	m = key(m, "Some Book")
	m = key(m, "enter")
	b, _ = os.ReadFile(filepath.Join(v.NotesPath(), "some-book.org"))
	if !strings.Contains(string(b), "#+FILETAGS: :literature:") || !strings.Contains(string(b), "- [[id:alpha][Alpha sprint idea]]") {
		t.Fatalf("N template wrong:\n%s", b)
	}
	m = key(m, "r")

	// Process mode walks oldest first: Alpha (Jan) before Beta (Feb); s skips, p promotes, end returns.
	m = key(m, "h")
	m = key(m, "h")
	if m.screen != screenHome {
		t.Fatalf("expected home, got %d", m.screen)
	}
	m = key(m, "P")
	if m.screen != screenProcess || m.note.Title != "Alpha sprint idea" || !strings.Contains(m.View(), "ask yourself") {
		t.Fatalf("process should start on Alpha:\n%s", m.View())
	}
	m = key(m, "p")
	if m.screen != screenProcess || m.note.Title != "Beta" {
		t.Fatalf("after promote expected Beta, got screen=%d note=%v", m.screen, m.note)
	}
	m = key(m, "p")
	if !strings.Contains(m.status, "orphan") || m.note.Title != "Beta" {
		t.Fatalf("orphan promote should refuse and stay: %q", m.status)
	}
	m = key(m, "s")
	if m.screen != screenHome || !strings.Contains(m.status, "1 handled") || !strings.Contains(m.status, "1 skipped") {
		t.Fatalf("end of queue should return home with a summary, got screen=%d status=%q", m.screen, m.status)
	}
	b, _ = os.ReadFile(filepath.Join(v.NotesPath(), "alpha.org"))
	if strings.Contains(string(b), "fleeting") {
		t.Error("Alpha should be permanent now")
	}
}

// Promoting at the end of the queue must return to where P was pressed,
// not one screen further back.
func TestProcessReturnsToList(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	os.MkdirAll(v.NotesPath(), 0o755)
	os.WriteFile(filepath.Join(v.NotesPath(), "a.org"), []byte(":PROPERTIES:\n:ID: a\n:END:\n#+TITLE: A\n#+FILETAGS: :fleeting:\n\n[[id:b]]\n"), 0o644)
	os.WriteFile(filepath.Join(v.NotesPath(), "b.org"), []byte(":PROPERTIES:\n:ID: b\n:END:\n#+TITLE: B\n\nx\n"), 0o644)
	m := New(v, "true")
	m.width, m.height = 100, 50
	m.prefDir = filepath.Join(root, "prefs")
	m = key(m, "f")
	m = key(m, "P")
	m = key(m, "p")
	if m.screen != screenNotes || m.filter != filterFleeting || !strings.Contains(m.status, "1 handled") {
		t.Fatalf("expected fleeting list with summary, got screen=%d filter=%d status=%q", m.screen, m.filter, m.status)
	}
}

func TestJournalHeadline(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	m := New(v, "true")
	m.width, m.height = 100, 50
	m.prefDir = filepath.Join(root, "prefs")
	m = key(m, "J")
	if m.mode != modeJournal || !strings.Contains(m.View(), "today's journal") {
		t.Fatalf("J should prompt for a headline:\n%s", m.View())
	}
	m = key(m, "Geelong, tired")
	m = key(m, "enter")
	b, err := os.ReadFile(v.DayPath(time.Now()))
	if err != nil || !strings.HasSuffix(string(b), "\n* Geelong, tired\n") {
		t.Fatalf("headline not written: %v\n%s", err, b)
	}
	// Empty headline just opens the file without adding an entry.
	m = key(m, "J")
	m = key(m, "enter")
	b2, _ := os.ReadFile(v.DayPath(time.Now()))
	if string(b2) != string(b) {
		t.Errorf("empty headline changed the file:\n%s", b2)
	}
}

func TestGoalsFlow(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	v.Jot(time.Now(), "went for a run, legs heavy")
	m := New(v, "true")
	m.width, m.height = 110, 60
	m.prefDir = filepath.Join(root, "prefs")
	// G opens goals; n walks the four prompts.
	m = key(m, "G")
	if m.screen != screenGoals || !strings.Contains(m.View(), "no goals yet") {
		t.Fatalf("goals screen:\n%s", m.View())
	}
	m = key(m, "n")
	m = key(m, "Run a half marathon")
	m = key(m, "enter")
	m = key(m, "feel strong")
	m = key(m, "enter")
	m = key(m, "2027-04")
	m = key(m, "enter")
	m = key(m, "run, long run")
	m = key(m, "enter")
	if len(m.goals) != 1 || m.goals[0].Why != "feel strong" || len(m.goals[0].Keywords) != 2 {
		t.Fatalf("goal not created: %+v", m.goals)
	}
	if !strings.Contains(m.View(), "Run a half marathon") {
		t.Fatalf("goal row missing:\n%s", m.View())
	}
	// Open it: the journal mention from today shows; add and toggle a milestone; add a review.
	m = key(m, "enter")
	if m.screen != screenGoal || !strings.Contains(m.View(), "journal mentions") || !strings.Contains(m.View(), "1 mention") {
		t.Fatalf("goal view should show the run mention:\n%s", m.View())
	}
	m = key(m, "a")
	m = key(m, "Run 10k")
	m = key(m, "enter")
	if len(m.goal.Milestones) != 1 {
		t.Fatalf("milestone not added: %+v", m.goal)
	}
	m.cursor = 0
	m = key(m, "x")
	if d, _ := m.goal.Progress(); d != 1 {
		t.Fatalf("toggle failed: %+v", m.goal.Milestones)
	}
	m = key(m, "R")
	m = key(m, "Moved: first 10k. Next: 12k.")
	m = key(m, "enter")
	if len(m.goal.Reviews) != 1 || !strings.Contains(m.View(), "first 10k") {
		t.Fatalf("review not shown:\n%s", m.View())
	}
	// Back to list, then the weekly walk: one goal, a review line, lands back on the list.
	m = key(m, "h")
	if m.screen != screenGoals {
		t.Fatalf("expected goals list, got %d", m.screen)
	}
	m = key(m, "P")
	if m.screen != screenGoalWalk || m.mode != modeReview {
		t.Fatalf("walk should prompt a review, screen=%d mode=%d", m.screen, m.mode)
	}
	m = key(m, "Steady week.")
	m = key(m, "enter")
	if m.screen != screenGoals || !strings.Contains(m.status, "1 of 1") {
		t.Fatalf("walk end: screen=%d status=%q", m.screen, m.status)
	}
	if len(m.goals[0].Reviews) != 2 {
		t.Errorf("walk review not saved: %+v", m.goals[0].Reviews)
	}
	// Home shows the goals section and the goals stat line.
	m = key(m, "h")
	if out := m.View(); !strings.Contains(out, "1 active goal") || !strings.Contains(out, "milestones") {
		t.Fatalf("home goals section missing:\n%s", out)
	}
}

func TestNewGoalEscSaves(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	m := New(v, "true")
	m.width, m.height = 110, 60
	m.prefDir = filepath.Join(root, "prefs")
	m = key(m, "G")
	m = key(m, "n")
	m = key(m, "esc")
	if len(m.goals) != 0 || !strings.Contains(m.status, "nothing saved") {
		t.Fatalf("esc before a title should save nothing: %+v %q", m.goals, m.status)
	}
	m = key(m, "n")
	m = key(m, "Draft the book outline")
	m = key(m, "enter")
	m = key(m, "because it matters")
	m = key(m, "enter")
	m = key(m, "esc")
	if len(m.goals) != 1 || m.goals[0].Title != "Draft the book outline" || m.goals[0].Why != "because it matters" || !strings.Contains(m.status, "saved early") {
		t.Fatalf("esc after a title should save the draft: %+v %q", m.goals, m.status)
	}
}
