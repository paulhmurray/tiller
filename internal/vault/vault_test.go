package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateAndAppendLink(t *testing.T) {
	v := Vault{Root: t.TempDir()}
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.Local)
	p, err := v.Create("Alpha thought", KindFleeting, nil, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := ":PROPERTIES:\n:ID: alpha-thought\n:END:\n#+TITLE: Alpha thought\n#+FILETAGS: :fleeting:\n#+CREATED: [2026-10-14 Wed 09:00]\n\n"
	if string(b) != want {
		t.Errorf("fleeting template:\n%q", b)
	}
	// First link creates the Related heading at the end.
	ok, err := AppendLink(p, "beta", "Beta")
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	b, _ = os.ReadFile(p)
	if string(b) != want+"* Related\n- [[id:beta][Beta]]\n" {
		t.Errorf("after first link:\n%q", b)
	}
	// Second link goes under the same heading; a repeat is a no-op.
	if ok, _ := AppendLink(p, "gamma", "Gamma"); !ok {
		t.Fatal("second link refused")
	}
	if ok, _ := AppendLink(p, "beta", "Beta again"); ok {
		t.Fatal("duplicate link accepted")
	}
	b, _ = os.ReadFile(p)
	if !strings.HasSuffix(string(b), "* Related\n- [[id:beta][Beta]]\n- [[id:gamma][Gamma]]\n") {
		t.Errorf("after second link:\n%q", b)
	}
	// Related in the middle of a file: insert stays inside that section.
	q := filepath.Join(v.NotesPath(), "mid.org")
	os.WriteFile(q, []byte("#+TITLE: Mid\n\n* Related\n- [[id:beta][Beta]]\n\n* Notes\ntext\n"), 0o644)
	AppendLink(q, "gamma", "Gamma")
	b, _ = os.ReadFile(q)
	if string(b) != "#+TITLE: Mid\n\n* Related\n- [[id:beta][Beta]]\n- [[id:gamma][Gamma]]\n\n* Notes\ntext\n" {
		t.Errorf("mid insert:\n%q", b)
	}
	// Literature template with an initial link.
	p, _ = v.Create("A Book", KindLiterature, []string{"go"}, []LinkTo{{"alpha-thought", "Alpha thought"}}, now)
	b, _ = os.ReadFile(p)
	s := string(b)
	for _, w := range []string{"#+FILETAGS: :literature:go:", "#+AUTHOR: ", "* Summary", "* Key takeaways", "* My take", "* Related\n- [[id:alpha-thought][Alpha thought]]\n"} {
		if !strings.Contains(s, w) {
			t.Errorf("literature template missing %q:\n%s", w, s)
		}
	}
}

func TestWriteLine(t *testing.T) {
	v := Vault{Root: t.TempDir()}
	now := time.Now()
	p, _ := v.Create("Plain", KindFleeting, nil, nil, now)
	if got := WriteLine(p); got != 7 {
		t.Errorf("plain fleeting: line %d", got)
	}
	p, _ = v.Create("Linked", KindFleeting, []string{"hotdesk"}, []LinkTo{{"hub", "Hub"}}, now)
	if got := WriteLine(p); got != 7 {
		b, _ := os.ReadFile(p)
		t.Errorf("linked fleeting: line %d\n%s", got, b)
	}
	p, _ = v.Create("Book", KindLiterature, nil, nil, now)
	if got := WriteLine(p); got != 9 {
		b, _ := os.ReadFile(p)
		t.Errorf("literature: line %d\n%s", got, b)
	}
}
