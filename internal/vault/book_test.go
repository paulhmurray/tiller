package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBook(t *testing.T) {
	v := Vault{Root: t.TempDir()}
	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.Local)
	b, err := v.NewBook("Hot Desk", 70000, now)
	if err != nil || b.Slug != "hot-desk" || b.Target != 70000 || len(b.Stages) != 4 || b.Stages[0] != "OUTLINE" {
		t.Fatalf("new book: %v %+v", err, b)
	}
	c, err := v.AddChapter(b, "The desk")
	if err != nil || c.State != "OUTLINE" || filepath.Base(c.File) != "ch01.org" || !c.Exists {
		t.Fatalf("add chapter: %v %+v", err, c)
	}
	b, _ = v.Book("hot-desk")
	if _, err := v.AddChapter(b, "The meeting"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(b.Dir, "ch01.org"), []byte("#+TITLE: The desk\n#+BOOK: Hot Desk\n\nShe found the desk already taken, again.\n"), 0o644)
	b, _ = v.Book("hot-desk")
	if len(b.Chapters) != 2 || b.Chapters[0].Words != 7 || b.Words() != 7 || b.Chapters[1].Title != "The meeting" {
		t.Fatalf("parsed: %+v", b.Chapters)
	}
	// Cycle stage edits only the heading line.
	before, _ := os.ReadFile(b.Outline)
	next, err := v.CycleStage(b, b.Chapters[0])
	if err != nil || next != "DRAFT" {
		t.Fatal(err, next)
	}
	after, _ := os.ReadFile(b.Outline)
	if strings.Replace(string(before), "* OUTLINE The desk", "* DRAFT The desk", 1) != string(after) {
		t.Errorf("cycle changed more than the heading:\n%s", after)
	}
	b, _ = v.Book("hot-desk")
	for i := 0; i < 3; i++ {
		v.CycleStage(b, b.Chapters[0])
		b, _ = v.Book("hot-desk")
	}
	if b.Chapters[0].State != "OUTLINE" {
		t.Errorf("cycle should wrap, got %s", b.Chapters[0].State)
	}
	// Positional fallback for a hand-written heading, and :FILE: wins when present.
	os.WriteFile(b.Outline, []byte("#+TITLE: Hot Desk\n\n* Premise\nplanning text, not a chapter\n* Story map\n* DRAFT Hand written\n* OUTLINE Explicit\n  :PROPERTIES:\n  :FILE: ch01.org\n  :END:\n"), 0o644)
	b, _ = v.Book("hot-desk")
	if len(b.Chapters) != 2 || filepath.Base(b.Chapters[0].File) != "ch01.org" || filepath.Base(b.Chapters[1].File) != "ch01.org" || b.Chapters[0].State != "DRAFT" {
		t.Errorf("file resolution: %+v", b.Chapters)
	}
	// Today's words: snapshot on first look, delta after, reset on a new day.
	cache := filepath.Join(v.Root, "cache")
	if n := WordsToday(b, cache, now); n != 0 {
		t.Errorf("first look should be 0, got %d", n)
	}
	os.WriteFile(filepath.Join(b.Dir, "ch01.org"), []byte("#+TITLE: x\n\none two three four five six seven eight nine ten\n"), 0o644)
	b, _ = v.Book("hot-desk")
	if n := WordsToday(b, cache, now); n != 2*10-2*7 {
		t.Errorf("delta = %d (two chapters share ch01 here)", n)
	}
	if n := WordsToday(b, cache, now.AddDate(0, 0, 1)); n != 0 {
		t.Errorf("new day should reset, got %d", n)
	}
}
