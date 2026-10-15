package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/paulhmurray/tiller/internal/vault"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuild(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	notes := v.NotesPath()
	write(t, filepath.Join(notes, "a.org"), ":PROPERTIES:\n:ID: A\n:END:\n#+TITLE: A\n\nsee [[id:b][B]] and [[https://x.org][web]]\n")
	write(t, filepath.Join(notes, "b.org"), ":PROPERTIES:\n:ID: b\n:END:\n#+TITLE: B\n#+FILETAGS: :fleeting:\n\nno links\n")
	write(t, filepath.Join(notes, "c.org"), "#+TITLE: C\n\n[[file:zettelkasten/permanent/a.org::*Heading][A]]\n")
	write(t, filepath.Join(notes, "d.org"), "#+TITLE: D\n\n[[file:never-written.org]] and [[id:ghost]]\n")
	write(t, filepath.Join(notes, "e.org"), "#+TITLE: E\n\nlonely\n")
	write(t, filepath.Join(v.JournalPath(), "2026-10-13.org"), "#+TITLE: 2026-10-13 Tuesday\n\n* Thought\nabout [[id:A]]\n")

	ix, err := Build(v)
	if err != nil {
		t.Fatal(err)
	}
	if got := ix.Backlinks["a"]; len(got) != 2 || got[0] != "c" || got[1] != "day:2026-10-13" {
		t.Errorf("backlinks of A = %v", got)
	}
	if got := ix.Backlinks["b"]; len(got) != 1 || got[0] != "a" {
		t.Errorf("backlinks of B = %v", got)
	}
	if got := ix.Outgoing["a"]; len(got) != 1 || got[0] != "b" {
		t.Errorf("outgoing of A = %v (https must be ignored)", got)
	}
	if o := ix.Orphans(); len(o) != 2 || o[0].Title+o[1].Title != "ED" && o[0].Title+o[1].Title != "DE" {
		names := ""
		for _, n := range o {
			names += n.Title
		}
		t.Errorf("orphans = %q, want D and E", names)
	}
	if m := ix.Missing(); len(m) != 2 || m[0] != "id:ghost" || m[1] != "never-written" {
		t.Errorf("missing = %v", m)
	}
	if ix.Get("B").Life() != "fleeting" || ix.Get("a").Life() != "permanent" {
		t.Errorf("life buckets wrong")
	}
	if ix.LinkCount() != 3 {
		t.Errorf("link count = %d", ix.LinkCount())
	}
}

func TestSuggest(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	notes := v.NotesPath()
	write(t, filepath.Join(notes, "sprints.org"), "#+TITLE: Sprint planning\n#+FILETAGS: :fleeting:agile:\n")
	write(t, filepath.Join(notes, "retro.org"), "#+TITLE: Retrospectives\n#+FILETAGS: :agile:\n")
	write(t, filepath.Join(notes, "velocity.org"), "#+TITLE: Sprint velocity\n#+FILETAGS: :agile:metrics:\n")
	write(t, filepath.Join(notes, "linked.org"), "#+TITLE: Already linked sprint\n#+FILETAGS: :agile:\n[[file:sprints.org]]\n")
	write(t, filepath.Join(notes, "other.org"), "#+TITLE: Brownies\n#+FILETAGS: :fleeting:recipe:\n")
	ix, err := Build(v)
	if err != nil {
		t.Fatal(err)
	}
	got := ix.Suggest("sprints", 5)
	var titles []string
	for _, n := range got {
		titles = append(titles, n.Title)
	}
	// velocity: tag + word = 3; retro: tag = 2; linked excluded; brownies shares only the fleeting tag.
	if len(titles) != 2 || titles[0] != "Sprint velocity" || titles[1] != "Retrospectives" {
		t.Errorf("suggest = %v", titles)
	}
}

func TestBookLinks(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	write(t, filepath.Join(v.NotesPath(), "mara.org"), ":PROPERTIES:\n:ID: mara\n:END:\n#+TITLE: Mara\n")
	write(t, filepath.Join(v.NotesPath(), "hub.org"), "#+TITLE: Hub\n[[file:../book/hot-desk/outline.org][outline]]\n")
	write(t, filepath.Join(v.BooksPath(), "hot-desk", "outline.org"), "#+TITLE: Hot Desk\n\n* OUTLINE The desk\n  :PROPERTIES:\n  :FILE: ch01.org\n  :END:\n** Uses\n- [[id:mara][Mara]]\n")
	write(t, filepath.Join(v.BooksPath(), "hot-desk", "ch01.org"), "#+TITLE: The desk\n\nMara sat. [[file:../../notes/mara.org]]\n")
	ix, err := Build(v)
	if err != nil {
		t.Fatal(err)
	}
	bl := ix.Backlinks["mara"]
	if len(bl) != 2 || bl[0] != "book:book/hot-desk/outline.org" || bl[1] != "book:book/hot-desk/ch01.org" {
		t.Errorf("backlinks of mara = %v", bl)
	}
	if n := ix.Get("book:book/hot-desk/ch01.org"); n == nil || n.Kind != KindChapter || n.Title != "Hot Desk · The desk" || n.Words != 3 {
		t.Errorf("chapter node: %+v", n)
	}
	if out := ix.Outgoing["hub"]; len(out) != 1 || out[0] != "book:book/hot-desk/outline.org" {
		t.Errorf("hub → outline via relative path: %v, dangling %v", out, ix.Dangling)
	}
	if len(ix.Missing()) != 0 || len(ix.Orphans()) != 0 {
		t.Errorf("missing=%v orphans=%v", ix.Missing(), ix.Orphans())
	}
}
